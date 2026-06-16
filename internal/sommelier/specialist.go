package sommelier

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	TierDeterministic = "tier-0-deterministic"
	TierSpecialist    = "tier-1-specialist"
	TierGeneralist    = "tier-2-local-generalist"
	TierSupervisor    = "tier-3-supervisor"
)

// SpecialistProfile describes a candidate local model that the specialist
// router can select: its supported languages and capabilities, current
// health/qualification state, resource footprint, and the score used to
// rank it against other profiles.
type SpecialistProfile struct {
	Alias              string
	Languages          []string
	Capabilities       []string
	Healthy            bool
	Qualified          bool
	ContextTokens      int
	MemoryBytes        uint64
	Warm               bool
	PredictedLatencyMS int64
	Score              float64
	Generalist         bool
	Backend            string
}

// SpecialistRequest describes the requirements a task places on a local
// model: the language it concerns, the capabilities it needs, its context
// size, the memory available to satisfy it, and an optional deterministic
// tool that can short-circuit model selection entirely.
type SpecialistRequest struct {
	Language          string
	Capabilities      []string
	ContextTokens     int
	AvailableMemory   uint64
	DeterministicTool string
}

// SpecialistDecision is the outcome of routing a SpecialistRequest: which
// tier handled it, which model and backend were selected (if any), why, and
// the score/latency/warm-state of that selection.
type SpecialistDecision struct {
	Tier      string
	Model     string
	Backend   string
	Reason    string
	Score     float64
	Cold      bool
	LatencyMS int64
}

// SpecialistRouter selects a local model for a request from a fixed set of
// candidate profiles, falling back to the generalist tier and ultimately to
// the supervisor tier when no local model qualifies.
type SpecialistRouter struct {
	Profiles []SpecialistProfile
}

func (router SpecialistRouter) DecideSpecialist(request SpecialistRequest) SpecialistDecision {
	if request.ContextTokens < 0 {
		request.ContextTokens = 0
	}
	if request.Capabilities == nil {
		request.Capabilities = []string{}
	}
	if request.DeterministicTool != "" {
		return SpecialistDecision{
			Tier: TierDeterministic, Reason: "deterministic tool satisfies the request",
			Model: request.DeterministicTool,
		}
	}
	var specialists, generalists []SpecialistProfile
	for _, profile := range router.Profiles {
		if !profile.Qualified || !profile.Healthy || profile.ContextTokens < request.ContextTokens ||
			(request.AvailableMemory > 0 && profile.MemoryBytes > request.AvailableMemory) ||
			!containsAll(profile.Capabilities, request.Capabilities) {
			continue
		}
		if profile.Generalist {
			generalists = append(generalists, profile)
			continue
		}
		if containsFold(profile.Languages, request.Language) {
			specialists = append(specialists, profile)
		}
	}
	if profile, ok := bestProfile(specialists); ok {
		return specialistDecision(TierSpecialist, profile, "qualified language specialist selected")
	}
	if profile, ok := bestProfile(generalists); ok {
		return specialistDecision(TierGeneralist, profile, "no qualified specialist; local generalist selected")
	}
	return SpecialistDecision{Tier: TierSupervisor, Reason: "no healthy qualified local model fits the request"}
}

// Decide implements the Router interface by delegating to DecideSpecialist.
// This is a partial mapping: only Language is currently extracted from
// RouteRequest.Signals. Capabilities, ContextTokens, AvailableMemory, and
// DeterministicTool are left at their zero values because RouteRequest and
// Signals do not yet carry that information. Wiring those signals through is
// reserved for future work.
//
// AvailableMemory is always 0 in the constructed SpecialistRequest, which
// disables the memory-fit check in DecideSpecialist; it must be wired before
// this method is safe to use in production routing.
//
// This method is currently exercised only via SetLocalModelRouter in tests.
func (router SpecialistRouter) Decide(_ context.Context, request RouteRequest) (Decision, bool) {
	language := ""
	if request.Signals != nil {
		language = request.Signals.Language
	}
	decision := router.DecideSpecialist(SpecialistRequest{Language: language})
	if decision.Tier == TierSupervisor {
		return Decision{}, false
	}
	return Decision{
		Kitchen: decision.Model,
		Reason:  decision.Reason,
		Tier:    decision.Tier,
	}, true
}

func specialistDecision(tier string, profile SpecialistProfile, reason string) SpecialistDecision {
	return SpecialistDecision{
		Tier: tier, Model: profile.Alias, Backend: profile.Backend, Reason: reason,
		Score: profile.Score, Cold: !profile.Warm, LatencyMS: profile.PredictedLatencyMS,
	}
}

// bestProfile returns the highest-scoring profile from profiles, or false if
// profiles is empty. Warm profiles get a +10 bonus over their raw Score, and
// predicted latency is subtracted (in seconds) from the score so that
// lower-latency models are preferred among otherwise-equal candidates. Ties
// are broken deterministically by Alias in ascending order, so the same
// input always yields the same selection.
func bestProfile(profiles []SpecialistProfile) (SpecialistProfile, bool) {
	if len(profiles) == 0 {
		return SpecialistProfile{}, false
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		left := profiles[i].Score
		right := profiles[j].Score
		if profiles[i].Warm {
			left += 10
		}
		if profiles[j].Warm {
			right += 10
		}
		left -= float64(profiles[i].PredictedLatencyMS) / 1000
		right -= float64(profiles[j].PredictedLatencyMS) / 1000
		if left != right {
			return left > right
		}
		return profiles[i].Alias < profiles[j].Alias
	})
	return profiles[0], true
}

type ModelState string

const (
	ModelStandby     ModelState = "standby"
	ModelLoading     ModelState = "loading"
	ModelReady       ModelState = "ready"
	ModelDraining    ModelState = "draining"
	ModelFailed      ModelState = "failed"
	ModelQuarantined ModelState = "quarantined"
)

type ModelRuntime struct {
	Alias       string
	State       ModelState
	MemoryBytes uint64
	Pinned      bool
	LastUsed    time.Time
	Progress    int
	Error       string
	Backend     string
	wait        chan struct{}
}

type Loader interface {
	Load(context.Context, string, func(int)) error
	Unload(context.Context, string) error
}

type ModelManager struct {
	mu      sync.Mutex
	models  map[string]*ModelRuntime
	budget  uint64
	idleTTL time.Duration
	loader  Loader
	now     func() time.Time
}

func NewModelManager(budget uint64, idleTTL time.Duration, loader Loader) *ModelManager {
	return &ModelManager{
		models: make(map[string]*ModelRuntime), budget: budget, idleTTL: idleTTL,
		loader: loader, now: time.Now,
	}
}

// Register adds or replaces a model's runtime entry. It must be called
// serially during initialization, before any concurrent EnsureLoaded or
// Quarantine calls begin; Register does not coordinate with in-flight
// load/quarantine operations on the same alias.
func (manager *ModelManager) Register(model ModelRuntime) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	copy := model
	if copy.State == "" {
		copy.State = ModelStandby
	}
	manager.models[model.Alias] = &copy
}

func (manager *ModelManager) EnsureLoaded(ctx context.Context, alias string) (bool, time.Duration, error) {
	started := manager.now()
	for {
		manager.mu.Lock()
		model, ok := manager.models[alias]
		if !ok {
			manager.mu.Unlock()
			return false, 0, fmt.Errorf("unknown model %q", alias)
		}
		switch model.State {
		case ModelReady:
			model.LastUsed = manager.now()
			manager.mu.Unlock()
			return false, manager.now().Sub(started), nil
		case ModelQuarantined:
			err := fmt.Errorf("model %q is quarantined: %s", alias, model.Error)
			manager.mu.Unlock()
			return false, 0, err
		case ModelLoading:
			wait := model.wait
			manager.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return false, 0, ctx.Err()
			}
		default:
			if err := manager.makeRoomLocked(ctx, model.MemoryBytes, alias); err != nil {
				manager.mu.Unlock()
				return false, 0, err
			}
			model.State = ModelLoading
			model.Progress = 0
			model.Error = ""
			model.wait = make(chan struct{})
			wait := model.wait
			manager.mu.Unlock()

			err := manager.loader.Load(ctx, alias, func(progress int) {
				manager.mu.Lock()
				if current := manager.models[alias]; current != nil {
					current.Progress = progress
				}
				manager.mu.Unlock()
			})
			manager.mu.Lock()
			model = manager.models[alias]
			if model == nil {
				close(wait)
				manager.mu.Unlock()
				return true, manager.now().Sub(started), fmt.Errorf("model %q removed during load", alias)
			}
			if err != nil {
				if ctx.Err() != nil {
					model.State = ModelStandby
				} else {
					model.State = ModelFailed
				}
				model.Error = err.Error()
			} else if model.State == ModelStandby {
				// CancelLoad transitioned the model to ModelStandby while
				// the load was in flight; honour the cancellation rather
				// than overwriting it with ModelReady.
			} else {
				model.State = ModelReady
				model.Progress = 100
				model.LastUsed = manager.now()
			}
			close(wait)
			manager.mu.Unlock()
			return true, manager.now().Sub(started), err
		}
	}
}

// CancelLoad aborts a load for a model that is in ModelStandby or
// ModelLoading state. It transitions the model back to ModelStandby under the
// lock so concurrent EnsureLoaded callers observe the state change, then calls
// loader.Unload outside the lock to release any partial resources without
// holding the mutex across a blocking I/O call.
func (manager *ModelManager) CancelLoad(ctx context.Context, alias string) error {
	manager.mu.Lock()
	model, ok := manager.models[alias]
	if !ok {
		manager.mu.Unlock()
		return fmt.Errorf("unknown model %q", alias)
	}
	switch model.State {
	case ModelLoading, ModelStandby:
		model.State = ModelStandby
		manager.mu.Unlock()
		// Unload is called without the lock to avoid holding mu across I/O.
		// A failed Unload is not promoted to an error here because the
		// model is already reset to Standby and the partial resource release
		// is best-effort.
		_ = manager.loader.Unload(ctx, alias)
		return nil
	default:
		state := model.State
		manager.mu.Unlock()
		return fmt.Errorf("model %q is %s, not loading; use Quarantine to remove a ready model", alias, state)
	}
}

func (manager *ModelManager) Prewarm(ctx context.Context, alias string) (cold bool, latency time.Duration, err error) {
	return manager.EnsureLoaded(ctx, alias)
}

func (manager *ModelManager) Quarantine(alias, reason string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	model, ok := manager.models[alias]
	if !ok {
		// alias may never have been registered, or it may have been removed
		// by a concurrent operation between the caller's check and this
		// call; this is a rare benign race and is not distinguished further.
		return fmt.Errorf("unknown or removed model %q", alias)
	}
	model.State = ModelQuarantined
	model.Error = reason
	return nil
}

func (manager *ModelManager) EvictIdle(ctx context.Context) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	now := manager.now()
	for _, model := range manager.models {
		if model.State == ModelReady && !model.Pinned && now.Sub(model.LastUsed) >= manager.idleTTL {
			if err := manager.loader.Unload(ctx, model.Alias); err != nil {
				model.State = ModelFailed
				model.Error = err.Error()
				continue
			}
			model.State = ModelStandby
		}
	}
	return nil
}

func (manager *ModelManager) Snapshot() []ModelRuntime {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	result := make([]ModelRuntime, 0, len(manager.models))
	for _, model := range manager.models {
		copy := *model
		copy.wait = nil
		result = append(result, copy)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Alias < result[j].Alias })
	return result
}

func (manager *ModelManager) makeRoomLocked(ctx context.Context, required uint64, loading string) error {
	used := uint64(0)
	var candidates []*ModelRuntime
	for alias, model := range manager.models {
		if alias == loading {
			continue
		}
		switch model.State {
		case ModelReady:
			used += model.MemoryBytes
			if !model.Pinned {
				candidates = append(candidates, model)
			}
		case ModelLoading, ModelDraining:
			// These models still occupy warm-set memory but cannot be
			// evicted here: a loading model is being raced by another
			// EnsureLoaded call, and a draining model's unload is owned
			// by another in-flight makeRoomLocked invocation.
			used += model.MemoryBytes
		case ModelQuarantined:
			// A quarantined model remains resident in memory and must be
			// counted against the budget, but it must not be added to the
			// eviction candidates: quarantine is a safety state and is
			// intentionally sticky.
			used += model.MemoryBytes
		}
	}
	if used+required <= manager.budget {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].LastUsed.Before(candidates[j].LastUsed) })
	for _, candidate := range candidates {
		candidate.State = ModelDraining
		if err := manager.loader.Unload(ctx, candidate.Alias); err != nil {
			candidate.State = ModelFailed
			candidate.Error = err.Error()
			continue
		}
		candidate.State = ModelStandby
		used -= candidate.MemoryBytes
		if used+required <= manager.budget {
			return nil
		}
	}
	return fmt.Errorf("model %q cannot fit within warm-set memory budget", loading)
}

type LifecycleAdapter struct {
	ID                string
	NativeLifecycle   bool
	ProcessEviction   bool
	SerializedLoading bool
}

func ManagedLifecycleAdapters() []LifecycleAdapter {
	return []LifecycleAdapter{
		{ID: "rs-llmctl", NativeLifecycle: true, ProcessEviction: true, SerializedLoading: true},
		{ID: "llama-swap", NativeLifecycle: false, ProcessEviction: false, SerializedLoading: true},
	}
}

func containsAll(values, required []string) bool {
	for _, value := range required {
		if !containsFold(values, value) {
			return false
		}
	}
	return true
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}
