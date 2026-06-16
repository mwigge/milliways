package sommelier

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeLoader struct {
	mu      sync.Mutex
	loads   int
	unloads []string
	loadErr error
}

func (loader *fakeLoader) Load(_ context.Context, _ string, progress func(int)) error {
	loader.mu.Lock()
	loader.loads++
	err := loader.loadErr
	loader.mu.Unlock()
	progress(50)
	if err != nil {
		return err
	}
	progress(100)
	return nil
}

func (loader *fakeLoader) Unload(_ context.Context, alias string) error {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	loader.unloads = append(loader.unloads, alias)
	return nil
}

func TestSpecialistRouterUsesTierOrderAndReasons(t *testing.T) {
	router := SpecialistRouter{Profiles: []SpecialistProfile{
		{Alias: "rust-cold", Languages: []string{"rust"}, Capabilities: []string{"bug-fix"}, Qualified: true, Healthy: true, ContextTokens: 8192, MemoryBytes: 4, Score: 90, PredictedLatencyMS: 500},
		{Alias: "rust-warm", Languages: []string{"rust"}, Capabilities: []string{"bug-fix"}, Qualified: true, Healthy: true, ContextTokens: 8192, MemoryBytes: 4, Score: 85, Warm: true, PredictedLatencyMS: 100},
		{Alias: "general", Generalist: true, Capabilities: []string{"bug-fix"}, Qualified: true, Healthy: true, ContextTokens: 8192, MemoryBytes: 4},
	}}
	decision := router.DecideSpecialist(SpecialistRequest{Language: "rust", Capabilities: []string{"bug-fix"}, ContextTokens: 1000, AvailableMemory: 8})
	if decision.Tier != TierSpecialist || decision.Model != "rust-warm" || decision.Reason == "" {
		t.Fatalf("decision = %#v", decision)
	}
	if got := router.DecideSpecialist(SpecialistRequest{DeterministicTool: "gofmt"}); got.Tier != TierDeterministic {
		t.Fatalf("deterministic decision = %#v", got)
	}
}

func TestModelManagerSerializesColdLoad(t *testing.T) {
	loader := &fakeLoader{}
	manager := NewModelManager(10, time.Hour, loader)
	manager.Register(ModelRuntime{Alias: "model", MemoryBytes: 4})
	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			if _, _, err := manager.EnsureLoaded(context.Background(), "model"); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if loader.loads != 1 {
		t.Fatalf("loads = %d, want 1", loader.loads)
	}
}

func TestModelManagerEvictsLRUWithinBudget(t *testing.T) {
	loader := &fakeLoader{}
	manager := NewModelManager(8, time.Hour, loader)
	manager.Register(ModelRuntime{Alias: "old", State: ModelReady, MemoryBytes: 5, LastUsed: time.Unix(1, 0)})
	manager.Register(ModelRuntime{Alias: "new", MemoryBytes: 5})
	if _, _, err := manager.EnsureLoaded(context.Background(), "new"); err != nil {
		t.Fatal(err)
	}
	if len(loader.unloads) != 1 || loader.unloads[0] != "old" {
		t.Fatalf("unloads = %v", loader.unloads)
	}
}

func TestModelManagerEnsureLoadedReturnsLoadError(t *testing.T) {
	loadErr := errors.New("load failed")
	loader := &fakeLoader{loadErr: loadErr}
	manager := NewModelManager(10, time.Hour, loader)
	manager.Register(ModelRuntime{Alias: "model", MemoryBytes: 4})

	_, _, err := manager.EnsureLoaded(context.Background(), "model")
	if !errors.Is(err, loadErr) && (err == nil || err.Error() != loadErr.Error()) {
		t.Fatalf("err = %v, want %v", err, loadErr)
	}

	snapshot := manager.Snapshot()
	if len(snapshot) != 1 || snapshot[0].State != ModelFailed || snapshot[0].Error != loadErr.Error() {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestModelManagerQuarantineTransitionsState(t *testing.T) {
	loader := &fakeLoader{}
	manager := NewModelManager(10, time.Hour, loader)
	manager.Register(ModelRuntime{Alias: "model", State: ModelReady, MemoryBytes: 4})

	if err := manager.Quarantine("model", "unsafe output detected"); err != nil {
		t.Fatal(err)
	}

	snapshot := manager.Snapshot()
	if len(snapshot) != 1 || snapshot[0].State != ModelQuarantined || snapshot[0].Error != "unsafe output detected" {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	if _, _, err := manager.EnsureLoaded(context.Background(), "model"); err == nil {
		t.Fatal("expected quarantined model to fail EnsureLoaded")
	}

	if err := manager.Quarantine("missing", "n/a"); err == nil {
		t.Fatal("expected error for unknown alias")
	}
}

func TestModelManagerCountsLoadingAndDrainingMemoryInBudget(t *testing.T) {
	loader := &fakeLoader{}
	manager := NewModelManager(10, time.Hour, loader)
	// "busy" is mid-unload (draining) but still holds its memory.
	manager.Register(ModelRuntime{Alias: "busy", State: ModelDraining, MemoryBytes: 5})
	// "pinned" is ready but cannot be evicted to make room.
	manager.Register(ModelRuntime{Alias: "pinned", State: ModelReady, Pinned: true, MemoryBytes: 5, LastUsed: time.Unix(1, 0)})
	manager.Register(ModelRuntime{Alias: "new", MemoryBytes: 1})

	// busy(5) + pinned(5) + new(1) = 11 > budget(10), and neither busy nor
	// pinned can be evicted, so loading "new" must fail.
	if _, _, err := manager.EnsureLoaded(context.Background(), "new"); err == nil {
		t.Fatal("expected budget error, got nil")
	}
	if len(loader.unloads) != 0 {
		t.Fatalf("unloads = %v, want none", loader.unloads)
	}
}

func TestModelManagerQuarantinedModelCountsAgainstBudget(t *testing.T) {
	loader := &fakeLoader{}
	manager := NewModelManager(10*1024*1024, time.Hour, loader)
	// A quarantined model still occupies 8 MB of warm-set memory.
	manager.Register(ModelRuntime{Alias: "quarantined", State: ModelReady, MemoryBytes: 8 * 1024 * 1024})
	if err := manager.Quarantine("quarantined", "unsafe output detected"); err != nil {
		t.Fatal(err)
	}
	// A new 5 MB model cannot fit: 8+5=13 MB > 10 MB budget.
	manager.Register(ModelRuntime{Alias: "new", MemoryBytes: 5 * 1024 * 1024})
	_, _, err := manager.EnsureLoaded(context.Background(), "new")
	if err == nil {
		t.Fatal("expected budget-exceeded error, got nil")
	}
	// The quarantined model must not have been evicted.
	snapshot := manager.Snapshot()
	for _, m := range snapshot {
		if m.Alias == "quarantined" && m.State != ModelQuarantined {
			t.Fatalf("quarantined model state changed to %q", m.State)
		}
	}
}

func TestDecideSpecialistDefensiveClamps(t *testing.T) {
	t.Parallel()
	router := SpecialistRouter{Profiles: []SpecialistProfile{
		{
			Alias: "go-specialist", Languages: []string{"go"},
			Capabilities: []string{}, Qualified: true, Healthy: true,
			ContextTokens: 8192, MemoryBytes: 4,
		},
	}}

	tests := []struct {
		name    string
		request SpecialistRequest
	}{
		{
			name:    "negative context tokens clamped to zero",
			request: SpecialistRequest{Language: "go", ContextTokens: -1},
		},
		{
			name:    "nil capabilities treated as empty",
			request: SpecialistRequest{Language: "go", Capabilities: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Must not panic and must return a valid (non-supervisor) decision
			// because the profile matches after the defensive clamp is applied.
			decision := router.DecideSpecialist(tt.request)
			if decision.Tier == TierSupervisor {
				t.Errorf("DecideSpecialist(%+v) = %+v, want non-supervisor tier", tt.request, decision)
			}
		})
	}
}

func TestManagedLifecyclePrefersNativeRsLlmctl(t *testing.T) {
	adapters := ManagedLifecycleAdapters()
	if adapters[0].ID != "rs-llmctl" || !adapters[0].ProcessEviction {
		t.Fatalf("adapters = %#v", adapters)
	}
	if adapters[1].ID != "llama-swap" || adapters[1].ProcessEviction {
		t.Fatalf("adapters = %#v", adapters)
	}
}
