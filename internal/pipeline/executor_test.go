package pipeline

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
)

// mockAdapter implements adapter.Adapter for testing.
type mockAdapter struct {
	output   string
	exitCode int
	delay    time.Duration
	failExec bool
}

func (m *mockAdapter) Exec(ctx context.Context, task kitchen.Task) (<-chan adapter.Event, error) {
	if m.failExec {
		return nil, fmt.Errorf("exec failed")
	}

	ch := make(chan adapter.Event, 8)
	go func() {
		defer close(ch)

		if m.delay > 0 {
			select {
			case <-time.After(m.delay):
			case <-ctx.Done():
				ch <- adapter.Event{Type: adapter.EventDone, ExitCode: 1}
				return
			}
		}

		ch <- adapter.Event{Type: adapter.EventText, Text: m.output}
		ch <- adapter.Event{Type: adapter.EventDone, ExitCode: m.exitCode}
	}()
	return ch, nil
}

func (m *mockAdapter) Send(_ context.Context, _ string) error { return adapter.ErrNotInteractive }
func (m *mockAdapter) SupportsResume() bool                   { return false }
func (m *mockAdapter) SessionID() string                      { return "" }
func (m *mockAdapter) ProcessID() int                         { return 0 }
func (m *mockAdapter) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{}
}

func mockFactory(adapters map[string]*mockAdapter) AdapterFactory {
	return func(_ context.Context, kitchenName string) (adapter.Adapter, error) {
		a, ok := adapters[kitchenName]
		if !ok {
			return nil, fmt.Errorf("no mock adapter for %q", kitchenName)
		}
		return a, nil
	}
}

func TestExecutor_Sequential(t *testing.T) {
	adapters := map[string]*mockAdapter{
		"a": {output: "output-a"},
		"b": {output: "output-b"},
		"c": {output: "output-c"},
	}

	var order []string
	var mu sync.Mutex

	exec := NewExecutor(
		mockFactory(adapters),
		nil,
		func(stepID string, status StepStatus) {
			if status == StatusDone {
				mu.Lock()
				order = append(order, stepID)
				mu.Unlock()
			}
		},
	)

	p := &Pipeline{
		ID: "test-seq",
		Steps: []*Step{
			{ID: "a", Kitchen: "a", Status: StatusPending},
			{ID: "b", Kitchen: "b", DependsOn: []string{"a"}, Status: StatusPending},
			{ID: "c", Kitchen: "c", DependsOn: []string{"b"}, Status: StatusPending},
		},
	}

	err := exec.Run(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Status != StatusDone {
		t.Errorf("pipeline status = %s, want done", p.Status)
	}

	// Verify sequential order.
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Errorf("execution order = %v, want [a b c]", order)
	}
}

func TestExecutor_FanOut(t *testing.T) {
	adapters := map[string]*mockAdapter{
		"plan":    {output: "plan-output"},
		"worker1": {output: "w1-output", delay: 10 * time.Millisecond},
		"worker2": {output: "w2-output", delay: 10 * time.Millisecond},
		"worker3": {output: "w3-output", delay: 10 * time.Millisecond},
		"summary": {output: "summary-output"},
	}

	var mu sync.Mutex
	statusLog := make(map[string][]StepStatus)

	exec := NewExecutor(
		mockFactory(adapters),
		nil,
		func(stepID string, status StepStatus) {
			mu.Lock()
			statusLog[stepID] = append(statusLog[stepID], status)
			mu.Unlock()
		},
	)

	p := &Pipeline{
		ID: "test-fanout",
		Steps: []*Step{
			{ID: "plan", Kitchen: "plan", Status: StatusPending},
			{ID: "w1", Kitchen: "worker1", DependsOn: []string{"plan"}, Mode: Parallel, Status: StatusPending},
			{ID: "w2", Kitchen: "worker2", DependsOn: []string{"plan"}, Mode: Parallel, Status: StatusPending},
			{ID: "w3", Kitchen: "worker3", DependsOn: []string{"plan"}, Mode: Parallel, Status: StatusPending},
			{ID: "summarize", Kitchen: "summary", DependsOn: []string{"w1", "w2", "w3"}, Status: StatusPending},
		},
	}

	err := exec.Run(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Status != StatusDone {
		t.Errorf("pipeline status = %s, want done", p.Status)
	}

	for _, s := range p.Steps {
		if s.Status != StatusDone {
			t.Errorf("step %s status = %s, want done", s.ID, s.Status)
		}
	}

	// Summarize must have seen all workers complete.
	sumStep := p.StepByID("summarize")
	if sumStep.Output == "" {
		t.Error("summarize step has empty output")
	}
}

func TestExecutor_StepFailure(t *testing.T) {
	adapters := map[string]*mockAdapter{
		"ok":     {output: "ok-output"},
		"bad":    {output: "fail-output", exitCode: 1},
		"claude": {output: "summary"},
	}

	exec := NewExecutor(mockFactory(adapters), nil, nil)

	p := &Pipeline{
		ID: "test-fail",
		Steps: []*Step{
			{ID: "ok-step", Kitchen: "ok", Mode: Parallel, Status: StatusPending},
			{ID: "bad-step", Kitchen: "bad", Mode: Parallel, Status: StatusPending},
			{ID: "summarize", Kitchen: "claude", DependsOn: []string{"ok-step", "bad-step"}, Status: StatusPending},
		},
	}

	err := exec.Run(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Status != StatusFailed {
		t.Errorf("pipeline status = %s, want failed", p.Status)
	}

	if p.StepByID("bad-step").Status != StatusFailed {
		t.Error("bad-step should be failed")
	}

	// Summarize still runs (it's the last step and gets partial results).
	if p.StepByID("summarize").Status != StatusDone {
		t.Errorf("summarize status = %s, want done (should run with partial results)", p.StepByID("summarize").Status)
	}
}

func TestExecutor_ContextCancel(t *testing.T) {
	adapters := map[string]*mockAdapter{
		"slow": {output: "slow", delay: 5 * time.Second},
	}

	exec := NewExecutor(mockFactory(adapters), nil, nil)

	p := &Pipeline{
		ID: "test-cancel",
		Steps: []*Step{
			{ID: "slow-step", Kitchen: "slow", Status: StatusPending},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := exec.Run(ctx, p)
	if err == nil {
		t.Fatal("expected context error")
	}

	if p.Status != StatusFailed {
		t.Errorf("pipeline status = %s, want failed", p.Status)
	}
}

func TestExecutor_MaxSteps(t *testing.T) {
	exec := NewExecutor(nil, nil, nil)

	steps := make([]*Step, MaxSteps+1)
	for i := range steps {
		steps[i] = &Step{ID: fmt.Sprintf("s%d", i), Kitchen: "x", Status: StatusPending}
	}

	p := &Pipeline{ID: "test-max", Steps: steps}

	err := exec.Run(context.Background(), p)
	if err == nil {
		t.Fatal("expected max steps error")
	}
}

func TestValidateDAG_Cycle(t *testing.T) {
	p := &Pipeline{
		Steps: []*Step{
			{ID: "a", DependsOn: []string{"b"}},
			{ID: "b", DependsOn: []string{"a"}},
		},
	}

	err := validateDAG(p)
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestValidateDAG_MissingDep(t *testing.T) {
	p := &Pipeline{
		Steps: []*Step{
			{ID: "a", DependsOn: []string{"nonexistent"}},
		},
	}

	err := validateDAG(p)
	if err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func TestValidateDAG_DuplicateID(t *testing.T) {
	p := &Pipeline{
		Steps: []*Step{
			{ID: "a"},
			{ID: "a"},
		},
	}

	err := validateDAG(p)
	if err == nil {
		t.Fatal("expected duplicate ID error")
	}
}

func TestExecutor_AdapterFactoryError(t *testing.T) {
	factory := func(_ context.Context, _ string) (adapter.Adapter, error) {
		return nil, fmt.Errorf("factory error")
	}

	exec := NewExecutor(factory, nil, nil)

	p := &Pipeline{
		ID: "test-factory-err",
		Steps: []*Step{
			{ID: "a", Kitchen: "missing", Status: StatusPending},
		},
	}

	err := exec.Run(context.Background(), p)
	if err != nil {
		t.Fatalf("Run should not return error for step-level failures: %v", err)
	}

	if p.StepByID("a").Status != StatusFailed {
		t.Errorf("step a status = %s, want failed", p.StepByID("a").Status)
	}
}
