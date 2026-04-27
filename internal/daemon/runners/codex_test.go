package runners

import (
	"context"
	"encoding/base64"
	"testing"
	"time"
)

func TestExtractCodexAssistantText_Message(t *testing.T) {
	t.Parallel()
	line := `{"type":"message","content":"hello world"}`
	got, ok := extractCodexAssistantText(line)
	if !ok {
		t.Fatalf("extractCodexAssistantText: expected ok=true")
	}
	if got != "hello world" {
		t.Errorf("text = %q, want %q", got, "hello world")
	}
}

func TestExtractCodexAssistantText_Delta(t *testing.T) {
	t.Parallel()
	line := `{"type":"response.output_text.delta","delta":"chunk"}`
	got, ok := extractCodexAssistantText(line)
	if !ok {
		t.Fatalf("expected ok=true for delta event")
	}
	if got != "chunk" {
		t.Errorf("text = %q, want %q", got, "chunk")
	}
}

func TestExtractCodexAssistantText_AgentMessage(t *testing.T) {
	t.Parallel()
	line := `{"type":"agent_message","message":"final answer"}`
	got, ok := extractCodexAssistantText(line)
	if !ok {
		t.Fatalf("expected ok=true for agent_message")
	}
	if got != "final answer" {
		t.Errorf("text = %q, want %q", got, "final answer")
	}
}

func TestExtractCodexAssistantText_ItemCompleted(t *testing.T) {
	t.Parallel()
	line := `{"type":"item.completed","item":{"item_type":"assistant_message","text":"done"}}`
	got, ok := extractCodexAssistantText(line)
	if !ok {
		t.Fatalf("expected ok=true for item.completed assistant_message")
	}
	if got != "done" {
		t.Errorf("text = %q, want %q", got, "done")
	}
}

func TestExtractCodexAssistantText_NotJSON(t *testing.T) {
	t.Parallel()
	if _, ok := extractCodexAssistantText("not json"); ok {
		t.Errorf("expected ok=false for non-JSON")
	}
}

func TestExtractCodexAssistantText_ReasoningSkipped(t *testing.T) {
	t.Parallel()
	// Non-message events (reasoning/tool/etc.) are not assistant text.
	line := `{"type":"reasoning","summary":"thinking..."}`
	if _, ok := extractCodexAssistantText(line); ok {
		t.Errorf("expected ok=false for reasoning event")
	}
}

func TestExtractCodexAssistantText_ErrorEnvelope(t *testing.T) {
	t.Parallel()
	line := `{"type":"error","error":{"message":"something broke"}}`
	got, ok := extractCodexAssistantText(line)
	if !ok {
		t.Fatalf("expected ok=true for error envelope")
	}
	if got != "something broke" {
		t.Errorf("text = %q, want %q", got, "something broke")
	}
}

// TestRunCodex_InputClose_PushesEnd verifies that closing the input
// channel triggers a final {"t":"end"} push.
func TestRunCodex_InputClose_PushesEnd(t *testing.T) {
	t.Parallel()
	pusher := &fakePusher{}
	in := make(chan []byte)
	done := make(chan struct{})
	go func() {
		RunCodex(context.Background(), in, pusher, nil)
		close(done)
	}()
	close(in)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunCodex did not return after input close")
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

// TestRunCodex_NilStream verifies nil-stream guard does not panic.
func TestRunCodex_NilStream(t *testing.T) {
	t.Parallel()
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	done := make(chan struct{})
	go func() {
		RunCodex(context.Background(), in, nil, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunCodex(nil stream) did not return")
	}
}

// TestRunCodex_NoBinary asserts that a missing codex binary surfaces
// an error_count tick (and no token observations).
func TestRunCodex_NoBinary(t *testing.T) {
	prev := codexBinary
	codexBinary = "/no/such/binary/that/should/not/exist"
	defer func() { codexBinary = prev }()

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)

	done := make(chan struct{})
	go func() {
		RunCodex(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunCodex did not return")
	}

	if got := obs.counterTotal(MetricErrorCount, AgentIDCodex); got < 1 {
		t.Errorf("error_count total = %v, want >= 1", got)
	}
	if n := obs.counterCount(MetricTokensIn, AgentIDCodex); n != 0 {
		t.Errorf("expected no tokens_in observations for codex, got %d", n)
	}
}

// Sanity: encodeData is shared with claude — confirm it produces the
// {"t":"data","b64":...} shape codex events use.
func TestEncodeData_Codex(t *testing.T) {
	t.Parallel()
	got := encodeData("codex hi")
	if got["t"] != "data" {
		t.Errorf("t = %v, want data", got["t"])
	}
	want := base64.StdEncoding.EncodeToString([]byte("codex hi"))
	if got["b64"] != want {
		t.Errorf("b64 = %v, want %v", got["b64"], want)
	}
}
