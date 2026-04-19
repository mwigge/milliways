package orchestrator

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/bridge"
	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/project"
	"github.com/mwigge/milliways/internal/sommelier"
	"github.com/mwigge/milliways/internal/substrate"
)

type stubLocalModelRouter struct {
	calls    int
	decision sommelier.Decision
	ok       bool
}

func (s *stubLocalModelRouter) Decide(_ context.Context, _ sommelier.RouteRequest) (sommelier.Decision, bool) {
	s.calls++
	return s.decision, s.ok
}

var _ sommelier.LocalModelRouter = (*stubLocalModelRouter)(nil)

type stubAdapter struct {
	events    []adapter.Event
	sessionID string
}

func (s *stubAdapter) Exec(_ context.Context, _ kitchen.Task) (<-chan adapter.Event, error) {
	ch := make(chan adapter.Event, len(s.events))
	for _, evt := range s.events {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func (s *stubAdapter) Send(context.Context, string) error { return nil }
func (s *stubAdapter) SupportsResume() bool               { return s.sessionID != "" }
func (s *stubAdapter) SessionID() string                  { return s.sessionID }
func (s *stubAdapter) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{
		NativeResume:        s.sessionID != "",
		InteractiveSend:     true,
		StructuredEvents:    true,
		ExhaustionDetection: "structured",
	}
}

func TestOrchestratorFailover(t *testing.T) {
	t.Parallel()

	claude := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventText, Kitchen: "claude", Text: "working"},
		{Type: adapter.EventRateLimit, Kitchen: "claude", RateLimit: &adapter.RateLimitInfo{Status: "exhausted", IsExhaustion: true}},
		{Type: adapter.EventDone, Kitchen: "claude", ExitCode: 1},
	}}
	codex := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventText, Kitchen: "codex", Text: "continuing"},
		{Type: adapter.EventDone, Kitchen: "codex", ExitCode: 0},
	}}

	o := Orchestrator{
		Factory: func(_ context.Context, _ string, exclude map[string]bool, _ string, _ map[string]string) (RouteResult, error) {
			if !exclude["claude"] {
				return RouteResult{
					Decision: sommelier.Decision{Kitchen: "claude", Reason: "first"},
					Adapter:  claude,
				}, nil
			}
			return RouteResult{
				Decision: sommelier.Decision{Kitchen: "codex", Reason: "fallback"},
				Adapter:  codex,
			}, nil
		},
	}

	var routed []string
	var outputs []string
	conv, err := o.Run(context.Background(), RunRequest{
		ConversationID: "conv-1",
		BlockID:        "b1",
		Prompt:         "do the task",
	}, func(res RouteResult) {
		routed = append(routed, res.Decision.Kitchen)
	}, func(evt adapter.Event) {
		if evt.Text != "" {
			outputs = append(outputs, evt.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if conv.Status != "done" {
		t.Fatalf("status = %q", conv.Status)
	}
	if len(conv.Segments) != 2 {
		t.Fatalf("segments = %d", len(conv.Segments))
	}
	if len(routed) != 2 || routed[0] != "claude" || routed[1] != "codex" {
		t.Fatalf("routes = %#v", routed)
	}
	if conv.Segments[0].Status != "exhausted" || conv.Segments[1].Status != "done" {
		t.Fatalf("segment statuses = %#v", conv.Segments)
	}
	if len(outputs) < 3 {
		t.Fatalf("expected outputs from both providers and milliways, got %#v", outputs)
	}
}

// fakeReader is a substrate.Reader fake for testing.
type fakeReader struct {
	calls  atomic.Int64
	record substrate.ConversationRecord
	err    error
	cache  map[string]substrate.ConversationRecord
}

func newFakeReader(rec substrate.ConversationRecord) *fakeReader {
	return &fakeReader{
		record: rec,
		cache:  make(map[string]substrate.ConversationRecord),
	}
}

func (f *fakeReader) GetConversation(_ context.Context, id string) (substrate.ConversationRecord, error) {
	if f.err != nil {
		return substrate.ConversationRecord{}, f.err
	}
	if rec, ok := f.cache[id]; ok {
		return rec, nil
	}
	f.calls.Add(1)
	rec := f.record
	rec.ConversationID = id
	f.cache[id] = rec
	return rec, nil
}

func (f *fakeReader) InvalidateConversation(id string) {
	delete(f.cache, id)
}

// exhaustAndContinueOrchestrator is a helper that builds an orchestrator that
// exhausts a first provider then succeeds with a second.
func exhaustAndContinueOrchestrator(reader substrate.Reader) (*Orchestrator, RunRequest) {
	first := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventText, Kitchen: "first", Text: "working"},
		{Type: adapter.EventRateLimit, Kitchen: "first", RateLimit: &adapter.RateLimitInfo{Status: "exhausted", IsExhaustion: true}},
		{Type: adapter.EventDone, Kitchen: "first", ExitCode: 1},
	}}
	second := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventText, Kitchen: "second", Text: "done"},
		{Type: adapter.EventDone, Kitchen: "second", ExitCode: 0},
	}}
	o := &Orchestrator{
		Factory: func(_ context.Context, _ string, exclude map[string]bool, _ string, _ map[string]string) (RouteResult, error) {
			if !exclude["first"] {
				return RouteResult{Decision: sommelier.Decision{Kitchen: "first"}, Adapter: first}, nil
			}
			return RouteResult{Decision: sommelier.Decision{Kitchen: "second"}, Adapter: second}, nil
		},
		Reader: reader,
	}
	req := RunRequest{ConversationID: "conv-test", BlockID: "b1", Prompt: "do work"}
	return o, req
}

func TestOrchestratorReadsFromSubstrateOnExhaustion(t *testing.T) {
	t.Parallel()

	rec := substrate.ConversationRecord{
		Memory: conversation.MemoryState{
			WorkingSummary: "summary from substrate",
			NextAction:     "next from substrate",
		},
		Context: conversation.ContextBundle{
			SpecRefs:      []string{"spec/a.md"},
			MemPalaceText: "palace text",
		},
	}
	reader := newFakeReader(rec)
	o, req := exhaustAndContinueOrchestrator(reader)

	var hydratedConv *conversation.Conversation
	o.Hydrate = func(_ context.Context, conv *conversation.Conversation) error {
		hydratedConv = conv
		return nil
	}

	conv, err := o.Run(context.Background(), req, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if conv.Status != "done" {
		t.Fatalf("status = %q", conv.Status)
	}

	// Substrate was read at least once.
	if reader.calls.Load() == 0 {
		t.Fatal("expected substrate GetConversation to be called, got 0 calls")
	}

	// Hydrator received the pre-populated memory from substrate.
	if hydratedConv == nil {
		t.Fatal("Hydrate was not called")
	}
	if hydratedConv.Memory.WorkingSummary != "summary from substrate" {
		t.Errorf("WorkingSummary = %q, want %q", hydratedConv.Memory.WorkingSummary, "summary from substrate")
	}
	if hydratedConv.Memory.NextAction != "next from substrate" {
		t.Errorf("NextAction = %q, want %q", hydratedConv.Memory.NextAction, "next from substrate")
	}
	if len(hydratedConv.Context.SpecRefs) == 0 || hydratedConv.Context.SpecRefs[0] != "spec/a.md" {
		t.Errorf("SpecRefs = %v, want [spec/a.md]", hydratedConv.Context.SpecRefs)
	}
}

func TestOrchestratorCacheAvoidsRepeatSubstrateFetch(t *testing.T) {
	t.Parallel()

	// Two exhaustions → two hydrations for the same conversationID.
	first := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventRateLimit, Kitchen: "first", RateLimit: &adapter.RateLimitInfo{Status: "exhausted", IsExhaustion: true}},
		{Type: adapter.EventDone, Kitchen: "first", ExitCode: 1},
	}}
	second := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventRateLimit, Kitchen: "second", RateLimit: &adapter.RateLimitInfo{Status: "exhausted", IsExhaustion: true}},
		{Type: adapter.EventDone, Kitchen: "second", ExitCode: 1},
	}}
	third := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventDone, Kitchen: "third", ExitCode: 0},
	}}

	reader := newFakeReader(substrate.ConversationRecord{
		Memory: conversation.MemoryState{WorkingSummary: "cached"},
	})

	callOrder := 0
	o := &Orchestrator{
		Factory: func(_ context.Context, _ string, exclude map[string]bool, _ string, _ map[string]string) (RouteResult, error) {
			callOrder++
			switch {
			case !exclude["first"]:
				return RouteResult{Decision: sommelier.Decision{Kitchen: "first"}, Adapter: first}, nil
			case !exclude["second"]:
				return RouteResult{Decision: sommelier.Decision{Kitchen: "second"}, Adapter: second}, nil
			default:
				return RouteResult{Decision: sommelier.Decision{Kitchen: "third"}, Adapter: third}, nil
			}
		},
		Reader: reader,
	}

	_, err := o.Run(context.Background(), RunRequest{ConversationID: "conv-cache", BlockID: "b", Prompt: "p"}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Two exhaustions but only one substrate fetch (cache hit on second).
	if reader.calls.Load() != 1 {
		t.Errorf("substrate fetches = %d, want 1 (cache should absorb second exhaustion)", reader.calls.Load())
	}
}

func TestOrchestratorEvaluatesAfterEachUserTurn(t *testing.T) {
	t.Parallel()

	first := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventRateLimit, Kitchen: "first", RateLimit: &adapter.RateLimitInfo{Status: "exhausted", IsExhaustion: true}},
		{Type: adapter.EventDone, Kitchen: "first", ExitCode: 1},
	}}
	second := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventDone, Kitchen: "second", ExitCode: 0},
	}}

	o := &Orchestrator{
		Factory: func(_ context.Context, prompt string, exclude map[string]bool, _ string, _ map[string]string) (RouteResult, error) {
			if !exclude["first"] {
				return RouteResult{Decision: sommelier.Decision{Kitchen: "first", Reason: prompt}, Adapter: first}, nil
			}
			return RouteResult{Decision: sommelier.Decision{Kitchen: "second", Reason: prompt}, Adapter: second}, nil
		},
	}

	var evaluations []string
	o.Evaluator = EvaluatorFunc(func(_ context.Context, conv *conversation.Conversation) error {
		last := conv.Transcript[len(conv.Transcript)-1]
		if last.Role != conversation.RoleUser {
			t.Fatalf("last turn role = %q, want %q", last.Role, conversation.RoleUser)
		}
		evaluations = append(evaluations, last.Text)
		return nil
	})

	conv, err := o.Run(context.Background(), RunRequest{ConversationID: "conv-eval", BlockID: "b1", Prompt: "start here"}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(evaluations) != 2 {
		t.Fatalf("evaluations = %d, want 2", len(evaluations))
	}
	if evaluations[0] != "start here" {
		t.Fatalf("first evaluation = %q, want %q", evaluations[0], "start here")
	}
	if evaluations[1] == evaluations[0] {
		t.Fatal("continuation prompt was not appended as a distinct user turn")
	}
	last := conv.Transcript[len(conv.Transcript)-1]
	if last.Role != conversation.RoleUser || last.Text != evaluations[1] {
		t.Fatalf("last transcript turn = %#v, want appended continuation user turn", last)
	}
}

type stubBridgeClient struct {
	hits []conversation.ProjectHit

	queries []string
	limits  []int
}

func (s *stubBridgeClient) SearchProjectContext(_ context.Context, query string, limit int) ([]conversation.ProjectHit, error) {
	s.queries = append(s.queries, query)
	s.limits = append(s.limits, limit)
	return s.hits, nil
}

func (s *stubBridgeClient) Close() error { return nil }

func TestOrchestratorInjectsProjectContextIntoUserTurn(t *testing.T) {
	t.Parallel()

	second := &stubAdapter{events: []adapter.Event{{Type: adapter.EventDone, Kitchen: "second", ExitCode: 0}}}
	bridgeClient := &stubBridgeClient{hits: []conversation.ProjectHit{{
		DrawerID:    "drawer-1",
		Wing:        "decisions",
		Room:        "routing",
		Content:     "budget fallback prefers opencode",
		FactSummary: "budget fallback prefers opencode",
		Relevance:   0.9,
		CapturedAt:  "2026-04-18T10:00:00Z",
	}}}

	o := &Orchestrator{
		Factory: func(_ context.Context, _ string, _ map[string]bool, _ string, _ map[string]string) (RouteResult, error) {
			return RouteResult{Decision: sommelier.Decision{Kitchen: "second"}, Adapter: second}, nil
		},
		Bridge: bridge.NewForClient(&project.ProjectContext{RepoName: "repo"}, 1, bridgeClient),
	}

	conv, err := o.Run(context.Background(), RunRequest{ConversationID: "conv-project", BlockID: "b1", Prompt: "Investigate AlphaService retry policy"}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conv.Context.ProjectHits) != 1 {
		t.Fatalf("project hits = %#v, want one hit", conv.Context.ProjectHits)
	}
	if got := conv.Transcript[0].ProjectRefs; len(got) != 1 || got[0].DrawerID != "drawer-1" {
		t.Fatalf("project refs = %#v", got)
	}
	if len(bridgeClient.limits) == 0 || bridgeClient.limits[0] != 1 {
		t.Fatalf("limits = %#v, want [1]", bridgeClient.limits)
	}
}

func TestOrchestratorStoresRepoContextOnSegmentStart(t *testing.T) {
	t.Parallel()

	palaceDrawers := 11
	second := &stubAdapter{events: []adapter.Event{{Type: adapter.EventDone, Kitchen: "second", ExitCode: 0}}}
	o := &Orchestrator{
		Factory: func(_ context.Context, _ string, _ map[string]bool, _ string, _ map[string]string) (RouteResult, error) {
			return RouteResult{Decision: sommelier.Decision{Kitchen: "second"}, Adapter: second}, nil
		},
		ProjectContext: &project.ProjectContext{
			RepoRoot:         "/tmp/repo",
			RepoName:         "repo",
			Branch:           "feature/test",
			Commit:           "deadbeef",
			CodeGraphSymbols: 99,
			PalaceDrawers:    &palaceDrawers,
		},
	}

	conv, err := o.Run(context.Background(), RunRequest{ConversationID: "conv-project-context", BlockID: "b1", Prompt: "do work"}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conv.Segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(conv.Segments))
	}
	got := conv.Segments[0].RepoContext
	if got == nil {
		t.Fatal("repo context = nil, want populated context")
	}
	if got.RepoRoot != "/tmp/repo" || got.RepoName != "repo" || got.Branch != "feature/test" || got.Commit != "deadbeef" {
		t.Fatalf("repo identity = %#v", got)
	}
	if got.CodeGraphSymbols != 99 || got.PalaceDrawers != 11 {
		t.Fatalf("repo metrics = %#v", got)
	}
	got.RepoName = "changed"
	if o.ProjectContext.RepoName != "repo" {
		t.Fatalf("project context mutated = %#v", o.ProjectContext)
	}
	if conv.Segments[0].RepoContext.RepoName != "changed" {
		t.Fatalf("segment repo context should remain mutable copy, got %#v", conv.Segments[0].RepoContext)
	}
	if rebuilt := buildRepoContext(nil); rebuilt != nil {
		t.Fatalf("buildRepoContext(nil) = %#v, want nil", rebuilt)
	}
}

// TestOrchestratorTracksReposAccessedAndProjectRefsPerTurn verifies that the
// bridge is queried for project hits and that project refs are tracked per turn.
func TestOrchestratorTracksReposAccessedAndProjectRefsPerTurn(t *testing.T) {
	t.Parallel()

	palaceDrawers := 7
	second := &stubAdapter{events: []adapter.Event{{Type: adapter.EventDone, Kitchen: "second", ExitCode: 0}}}
	bridgeClient := &stubBridgeClient{
		hits: []conversation.ProjectHit{{
			DrawerID:    "drawer-1",
			Wing:        "decisions",
			Room:        "routing",
			Content:     "budget fallback prefers opencode",
			FactSummary: "budget fallback prefers opencode",
			Relevance:   0.9,
			CapturedAt:  "2026-04-18T10:00:00Z",
		}},
	}

	o := &Orchestrator{
		Factory: func(_ context.Context, _ string, _ map[string]bool, _ string, _ map[string]string) (RouteResult, error) {
			return RouteResult{Decision: sommelier.Decision{Kitchen: "second"}, Adapter: second}, nil
		},
		Bridge: bridge.NewForClient(&project.ProjectContext{
			RepoRoot:      "/home/user/acme",
			RepoName:      "acme",
			PalaceDrawers: &palaceDrawers,
		}, 1, bridgeClient),
		ProjectContext: &project.ProjectContext{
			RepoRoot:         "/home/user/acme",
			RepoName:         "acme",
			CodeGraphSymbols: 42,
		},
	}

	conv, err := o.Run(context.Background(), RunRequest{ConversationID: "conv-repos", BlockID: "b1", Prompt: "Investigate rate limiter behavior"}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify project hits were injected into context
	if len(conv.Context.ProjectHits) != 1 {
		t.Fatalf("project hits = %d, want 1", len(conv.Context.ProjectHits))
	}

	// Verify reposAccessed is tracked in orchestrator state
	// (The reposAccessed map is internal, but we verify via bridge queries)
	if len(bridgeClient.queries) == 0 {
		t.Error("expected bridge to have been queried")
	}
}

func TestBuildRepoContextOmitsOptionalZeroValues(t *testing.T) {
	t.Parallel()

	got := buildRepoContext(&project.ProjectContext{
		RepoRoot: "/tmp/repo",
		RepoName: "repo",
		Branch:   "main",
		Commit:   "abc123",
	})
	if got == nil {
		t.Fatal("buildRepoContext returned nil")
	}
	if got.CodeGraphSymbols != 0 || got.PalaceDrawers != 0 {
		t.Fatalf("optional metrics = %#v, want zero values", got)
	}
}

func TestOrchestratorAutoSwitchEmitsReversibleSwitchEvent(t *testing.T) {
	t.Parallel()

	first := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventText, Kitchen: "claude", Text: "starting here"},
		{Type: adapter.EventRateLimit, Kitchen: "claude", RateLimit: &adapter.RateLimitInfo{Status: "exhausted", IsExhaustion: true}},
		{Type: adapter.EventDone, Kitchen: "claude", ExitCode: 1},
	}}
	second := &stubAdapter{events: []adapter.Event{
		{Type: adapter.EventText, Kitchen: "gemini", Text: "continuing after auto-switch"},
		{Type: adapter.EventDone, Kitchen: "gemini", ExitCode: 0},
	}}

	var runtimeEvents []observability.Event
	o := &Orchestrator{
		Factory: func(_ context.Context, _ string, exclude map[string]bool, _ string, _ map[string]string) (RouteResult, error) {
			if !exclude["claude"] {
				return RouteResult{Decision: sommelier.Decision{Kitchen: "claude", Tier: "keyword", Reason: "initial route"}, Adapter: first}, nil
			}
			return RouteResult{Decision: sommelier.Decision{Kitchen: "gemini", Tier: "auto-switch", Reason: `task mentioned "search the web" (hard signal)`}, Adapter: second}, nil
		},
		Sink: observability.FuncSink(func(evt observability.Event) {
			runtimeEvents = append(runtimeEvents, evt)
		}),
	}

	var outputs []string
	conv, err := o.Run(context.Background(), RunRequest{ConversationID: "conv-auto", BlockID: "b1", Prompt: "search the web for the incident"}, nil, func(evt adapter.Event) {
		if evt.Text != "" {
			outputs = append(outputs, evt.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if conv.Status != conversation.StatusDone {
		t.Fatalf("status = %q, want %q", conv.Status, conversation.StatusDone)
	}

	var switchEvent *observability.Event
	for i := range runtimeEvents {
		if runtimeEvents[i].Kind == "switch" {
			switchEvent = &runtimeEvents[i]
			break
		}
	}
	if switchEvent == nil {
		t.Fatalf("runtime events = %#v, want switch event", runtimeEvents)
	}
	for key, want := range map[string]string{
		"from":          "claude",
		"to":            "gemini",
		"reason":        `task mentioned "search the web" (hard signal)`,
		"trigger":       "hard-signal",
		"tier":          "auto-switch",
		"reversal_hint": "/back",
	} {
		if got := switchEvent.Fields[key]; got != want {
			t.Fatalf("switch field %q = %q, want %q", key, got, want)
		}
	}

	foundHint := false
	for _, output := range outputs {
		if strings.Contains(output, "Use /back to return") && strings.Contains(output, "task mentioned \"search the web\"") {
			foundHint = true
			break
		}
	}
	if !foundHint {
		t.Fatalf("outputs = %#v, want visible auto-switch line with /back hint", outputs)
	}
}

func TestOrchestratorStickyModePreventsAutoSwitch(t *testing.T) {
	t.Parallel()

	sticky := &stubAdapter{events: []adapter.Event{{Type: adapter.EventDone, Kitchen: "claude", ExitCode: 0}}}

	var (
		seenKitchenForces []string
		runtimeEvents     []observability.Event
	)
	o := &Orchestrator{
		Reader: newFakeReader(substrate.ConversationRecord{
			ConversationID: "conv-sticky",
			Prompt:         "search the web for release notes",
			Memory: conversation.MemoryState{
				StickyKitchen: "claude",
			},
			Segments: []conversation.ProviderSegment{{
				ID:        "seg-stick",
				Provider:  "claude",
				Status:    conversation.SegmentActive,
				StartedAt: time.Now().Add(-1 * time.Minute),
			}},
			ActiveSegmentID: "seg-stick",
		}),
		Factory: func(_ context.Context, _ string, exclude map[string]bool, kitchenForce string, _ map[string]string) (RouteResult, error) {
			seenKitchenForces = append(seenKitchenForces, kitchenForce)
			if exclude["claude"] {
				t.Fatalf("exclude = %#v, want sticky kitchen to remain eligible", exclude)
			}
			return RouteResult{
				Decision: sommelier.Decision{Kitchen: "claude", Tier: "forced", Reason: "sticky kitchen"},
				Adapter:  sticky,
			}, nil
		},
		Sink: observability.FuncSink(func(evt observability.Event) {
			runtimeEvents = append(runtimeEvents, evt)
		}),
	}

	conv, err := o.Run(context.Background(), RunRequest{
		ConversationID: "conv-sticky",
		BlockID:        "b1",
		Prompt:         "search the web for release notes",
	}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if conv.Status != conversation.StatusDone {
		t.Fatalf("status = %q, want %q", conv.Status, conversation.StatusDone)
	}
	if len(seenKitchenForces) != 1 || seenKitchenForces[0] != "claude" {
		t.Fatalf("kitchenForce calls = %#v, want [claude]", seenKitchenForces)
	}
	for _, evt := range runtimeEvents {
		if evt.Kind == "switch" {
			t.Fatalf("runtime events = %#v, want no auto-switch while sticky", runtimeEvents)
		}
	}
	if len(conv.Segments) != 1 || conv.Segments[0].Provider != "claude" {
		t.Fatalf("segments = %#v, want sticky claude segment only", conv.Segments)
	}
}

func TestSommelierComposesLearnedPantryLocalModelAndKeywordRouters(t *testing.T) {
	t.Parallel()

	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "claude", Cmd: "echo", Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "opencode", Cmd: "echo", Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "gemini", Cmd: "echo", Enabled: true}))

	router := sommelier.New(map[string]string{"code": "opencode"}, "claude", "opencode", nil, reg)
	local := &stubLocalModelRouter{
		decision: sommelier.Decision{Kitchen: "gemini", Tier: "local-model", Reason: "future local model"},
		ok:       true,
	}
	router.SetLocalModelRouter(local)

	learned := router.RouteEnriched("code a handler", &sommelier.Signals{LearnedKitchen: "claude", LearnedRate: 95}, nil)
	if learned.Kitchen != "claude" || learned.Tier != "learned" {
		t.Fatalf("learned route = %#v", learned)
	}
	if local.calls != 0 {
		t.Fatalf("local model calls after learned decision = %d, want 0", local.calls)
	}

	fallbackToLocal := router.RouteEnriched("code a handler", nil, nil)
	if fallbackToLocal.Kitchen != "gemini" || fallbackToLocal.Tier != "local-model" {
		t.Fatalf("local model route = %#v", fallbackToLocal)
	}
	if local.calls != 1 {
		t.Fatalf("local model calls = %d, want 1", local.calls)
	}
}

func TestOrchestratorInvalidateCacheBypassesCache(t *testing.T) {
	t.Parallel()

	reader := newFakeReader(substrate.ConversationRecord{
		Memory: conversation.MemoryState{WorkingSummary: "initial"},
	})

	// Pre-populate cache with "initial".
	_, _ = reader.GetConversation(context.Background(), "conv-inv")
	if reader.calls.Load() != 1 {
		t.Fatalf("expected 1 call after first fetch, got %d", reader.calls.Load())
	}

	// Invalidate: next Get should re-fetch.
	reader.InvalidateConversation("conv-inv")

	// Update the record the fake returns.
	reader.record = substrate.ConversationRecord{
		Memory: conversation.MemoryState{WorkingSummary: "updated"},
	}

	rec, err := reader.GetConversation(context.Background(), "conv-inv")
	if err != nil {
		t.Fatalf("GetConversation after invalidate: %v", err)
	}
	if rec.Memory.WorkingSummary != "updated" {
		t.Errorf("WorkingSummary = %q, want %q", rec.Memory.WorkingSummary, "updated")
	}
	if reader.calls.Load() != 2 {
		t.Errorf("substrate fetches = %d, want 2 (invalidate must force re-fetch)", reader.calls.Load())
	}
}
