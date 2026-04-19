package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

func newTestKitchen(cmd string, args ...string) *kitchen.GenericKitchen {
	return kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "test",
		Cmd:      cmd,
		Args:     args,
		Stations: []string{"test"},
		Tier:     kitchen.Free,
		Enabled:  true,
	})
}

func TestGenericAdapter_Exec_HappyPath(t *testing.T) {
	t.Parallel()

	k := newTestKitchen("echo", "-e", "line1\nline2\nline3")
	adapter := NewGenericAdapter(k, AdapterOpts{})

	ctx := context.Background()
	ch, err := adapter.Exec(ctx, kitchen.Task{Prompt: ""})
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	var events []Event
	for e := range ch {
		events = append(events, e)
	}

	// Should have text events + done event
	textCount := 0
	var doneEvent *Event
	for i := range events {
		switch events[i].Type {
		case EventText:
			textCount++
		case EventDone:
			doneEvent = &events[i]
		}
	}

	if textCount == 0 {
		t.Error("expected at least one EventText, got 0")
	}
	if doneEvent == nil {
		t.Fatal("expected EventDone, got none")
	}
	if doneEvent.ExitCode != 0 {
		t.Errorf("EventDone.ExitCode = %d, want 0", doneEvent.ExitCode)
	}
}

func TestGenericAdapter_Exec_NonZeroExit(t *testing.T) {
	t.Parallel()

	k := newTestKitchen("false")
	adapter := NewGenericAdapter(k, AdapterOpts{})

	ctx := context.Background()
	ch, err := adapter.Exec(ctx, kitchen.Task{Prompt: ""})
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	var doneEvent *Event
	for e := range ch {
		if e.Type == EventDone {
			doneEvent = &e
		}
	}

	if doneEvent == nil {
		t.Fatal("expected EventDone, got none")
	}
	if doneEvent.ExitCode == 0 {
		t.Error("expected non-zero exit code from 'false'")
	}
}

func TestGenericAdapter_Exec_ContextCancel(t *testing.T) {
	t.Parallel()

	k := newTestKitchen("sleep", "10")
	adapter := NewGenericAdapter(k, AdapterOpts{})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch, err := adapter.Exec(ctx, kitchen.Task{Prompt: ""})
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Drain the channel — should close after context cancellation
	var gotDone bool
	for e := range ch {
		if e.Type == EventDone {
			gotDone = true
		}
	}

	if !gotDone {
		t.Error("expected EventDone after context cancellation")
	}
}

func TestGenericAdapter_Send_ReturnsNotInteractive(t *testing.T) {
	t.Parallel()

	k := newTestKitchen("echo", "hi")
	adapter := NewGenericAdapter(k, AdapterOpts{})

	err := adapter.Send(context.Background(), "message")
	if !errors.Is(err, ErrNotInteractive) {
		t.Errorf("Send() = %v, want ErrNotInteractive", err)
	}
}

func TestGenericAdapter_Capabilities(t *testing.T) {
	t.Parallel()

	adapter := NewGenericAdapter(newTestKitchen("echo", "hi"), AdapterOpts{})
	caps := adapter.Capabilities()
	if caps.NativeResume {
		t.Error("Capabilities.NativeResume = true, want false")
	}
	if !caps.InteractiveSend {
		t.Error("Capabilities.InteractiveSend = false, want true")
	}
	if caps.StructuredEvents {
		t.Error("Capabilities.StructuredEvents = true, want false")
	}
	if caps.ExhaustionDetection != "none" {
		t.Errorf("Capabilities.ExhaustionDetection = %q, want none", caps.ExhaustionDetection)
	}
}

func TestGenericAdapter_SupportsResume(t *testing.T) {
	t.Parallel()

	k := newTestKitchen("echo", "hi")
	adapter := NewGenericAdapter(k, AdapterOpts{})

	if adapter.SupportsResume() {
		t.Error("SupportsResume() = true, want false")
	}
	if adapter.SessionID() != "" {
		t.Errorf("SessionID() = %q, want empty", adapter.SessionID())
	}
}
