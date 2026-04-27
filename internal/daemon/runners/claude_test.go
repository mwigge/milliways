package runners

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"
)

// fakePusher captures pushed events for assertions.
type fakePusher struct {
	mu     sync.Mutex
	events []map[string]any
}

func (p *fakePusher) Push(event any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if m, ok := event.(map[string]any); ok {
		p.events = append(p.events, m)
	}
}

func (p *fakePusher) snapshot() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]map[string]any, len(p.events))
	copy(out, p.events)
	return out
}

func TestExtractAssistantText_Happy(t *testing.T) {
	t.Parallel()
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"hello world"}]}}`
	got, ok := extractAssistantText(line)
	if !ok {
		t.Fatalf("extractAssistantText: expected ok=true, got false")
	}
	if got != "hello world" {
		t.Errorf("text = %q, want %q", got, "hello world")
	}
}

func TestExtractAssistantText_NotAssistant(t *testing.T) {
	t.Parallel()
	line := `{"type":"system","subtype":"init"}`
	if _, ok := extractAssistantText(line); ok {
		t.Errorf("extractAssistantText: expected ok=false for system event")
	}
}

func TestExtractAssistantText_NotJSON(t *testing.T) {
	t.Parallel()
	if _, ok := extractAssistantText("not json"); ok {
		t.Errorf("extractAssistantText: expected ok=false for non-JSON")
	}
}

func TestExtractAssistantText_EmptyContent(t *testing.T) {
	t.Parallel()
	line := `{"type":"assistant","message":{"content":[]}}`
	if _, ok := extractAssistantText(line); ok {
		t.Errorf("expected ok=false for empty content")
	}
}

func TestExtractResultCost(t *testing.T) {
	t.Parallel()
	line := `{"type":"result","total_cost_usd":0.0123}`
	cost, ok := extractResultCost(line)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if cost != 0.0123 {
		t.Errorf("cost = %v, want 0.0123", cost)
	}
}

func TestExtractResultCost_IsError(t *testing.T) {
	t.Parallel()
	line := `{"type":"result","is_error":true,"total_cost_usd":0.5}`
	if _, ok := extractResultCost(line); ok {
		t.Errorf("expected ok=false when is_error=true")
	}
}

// TestRunClaude_InputClose_PushesEnd verifies that closing the input channel
// triggers a final {"t":"end"} push and Close() on the stream.
func TestRunClaude_InputClose_PushesEnd(t *testing.T) {
	t.Parallel()
	pusher := &fakePusher{}
	in := make(chan []byte)
	done := make(chan struct{})
	go func() {
		RunClaude(context.Background(), in, pusher)
		close(done)
	}()
	close(in)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunClaude did not return after input close")
	}
	events := pusher.snapshot()
	if len(events) == 0 {
		t.Fatalf("expected at least one event, got 0")
	}
	last := events[len(events)-1]
	if last["t"] != "end" {
		t.Errorf("last event t = %v, want end", last["t"])
	}
}

// Sanity: assert encodeData wraps text correctly (drives the output shape
// the bridge expects without needing a subprocess).
func TestEncodeData(t *testing.T) {
	t.Parallel()
	got := encodeData("hi")
	if got["t"] != "data" {
		t.Errorf("t = %v, want data", got["t"])
	}
	want := base64.StdEncoding.EncodeToString([]byte("hi"))
	if got["b64"] != want {
		t.Errorf("b64 = %v, want %v", got["b64"], want)
	}
}
