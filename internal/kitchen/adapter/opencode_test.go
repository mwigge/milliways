package adapter

import (
	"context"
	"testing"
)

func TestOpenCodeAdapter_Send_WithoutPipe(t *testing.T) {
	t.Parallel()

	a := NewOpenCodeAdapter(newTestKitchen("echo"), AdapterOpts{})
	if err := a.Send(context.Background(), "msg"); err != ErrNotInteractive {
		t.Errorf("Send without pipe = %v, want ErrNotInteractive", err)
	}
}

func TestOpenCodeAdapter_Resume(t *testing.T) {
	t.Parallel()

	a := NewOpenCodeAdapter(newTestKitchen("echo"), AdapterOpts{})
	if !a.SupportsResume() {
		t.Error("SupportsResume() = false, want true")
	}
	if a.SessionID() != "" {
		t.Errorf("SessionID() = %q, want empty", a.SessionID())
	}
	caps := a.Capabilities()
	if !caps.NativeResume {
		t.Error("Capabilities.NativeResume = false, want true")
	}
	if caps.ExhaustionDetection == "" || caps.ExhaustionDetection == "none" {
		t.Error("Capabilities.ExhaustionDetection should be set")
	}
}

func TestParseGenericExhaustionText_OpenCode(t *testing.T) {
	t.Parallel()

	evt := parseGenericExhaustionText("opencode", "usage limit reached, try later", "stdout_text")
	if evt == nil {
		t.Fatal("expected exhaustion event")
	}
	if evt.RateLimit == nil || evt.RateLimit.DetectionKind != "stdout_text" {
		t.Fatalf("rate limit = %#v", evt.RateLimit)
	}
}
