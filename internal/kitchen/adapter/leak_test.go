package adapter

import (
	"context"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestGenericAdapter_ContextCancel_NoLeak(t *testing.T) {
	t.Parallel()

	k := kitchen.NewGeneric(kitchen.GenericConfig{
		Name:    "test",
		Cmd:     "sleep",
		Args:    []string{"30"},
		Enabled: true,
	})
	a := NewGenericAdapter(k, AdapterOpts{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch, err := a.Exec(ctx, kitchen.Task{Prompt: ""})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	// Drain — channel must close after context cancellation
	var gotDone bool
	for e := range ch {
		if e.Type == EventDone {
			gotDone = true
		}
	}
	if !gotDone {
		t.Error("expected EventDone after cancel")
	}
	// goleak.VerifyTestMain will catch any leaked goroutines
}
