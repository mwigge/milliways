package orchestrator

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/sommelier"
	"github.com/mwigge/milliways/internal/substrate"
)

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
