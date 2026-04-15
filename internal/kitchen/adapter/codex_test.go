package adapter

import (
	"context"
	"testing"
)

func TestCodexAdapter_Send_WithoutPipe(t *testing.T) {
	t.Parallel()

	a := NewCodexAdapter(newTestKitchen("echo"), AdapterOpts{})
	if err := a.Send(context.Background(), "msg"); err != ErrNotInteractive {
		t.Errorf("Send without pipe = %v, want ErrNotInteractive", err)
	}
}

func TestCodexAdapter_Resume(t *testing.T) {
	t.Parallel()

	a := NewCodexAdapter(newTestKitchen("echo"), AdapterOpts{})
	if a.SupportsResume() {
		t.Error("SupportsResume() = true, want false")
	}
	if a.SessionID() != "" {
		t.Errorf("SessionID() = %q, want empty", a.SessionID())
	}
	caps := a.Capabilities()
	if caps.NativeResume {
		t.Error("Capabilities.NativeResume = true, want false")
	}
	if !caps.StructuredEvents {
		t.Error("Capabilities.StructuredEvents = false, want true")
	}
}

func TestParseGenericExhaustionText_Codex(t *testing.T) {
	t.Parallel()

	evt := parseGenericExhaustionText("codex", "rate limit exceeded for current plan", "stdout_text")
	if evt == nil {
		t.Fatal("expected exhaustion event")
	}
	if evt.RateLimit == nil || !evt.RateLimit.IsExhaustion {
		t.Fatalf("rate limit = %#v", evt.RateLimit)
	}
}
