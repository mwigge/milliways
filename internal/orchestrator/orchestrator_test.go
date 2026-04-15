package orchestrator

import (
	"context"
	"testing"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/sommelier"
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
