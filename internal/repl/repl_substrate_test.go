package repl

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// TestReplayTurnsToSubstrate_NilSubstrate verifies that replayTurnsToSubstrate
// does not panic when the substrate client is nil (the common case for tests
// and environments without a live MemPalace server).
func TestReplayTurnsToSubstrate_NilSubstrate(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	r := NewREPL(buf)

	// Substrate is nil by default — confirm the method is safe to call.
	if r.substrate != nil {
		t.Fatal("precondition: expected nil substrate")
	}

	// Should not panic regardless of turnBuffer contents.
	r.turnBuffer = []ConversationTurn{
		{Role: "user", Text: "hello", Runner: "claude", At: time.Now()},
	}

	// Must not panic.
	r.replayTurnsToSubstrate(context.Background())
}

// TestReplayTurnsToSubstrate_NilSession verifies the guard when session is nil.
func TestReplayTurnsToSubstrate_NilSession(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	r := NewREPL(buf)
	r.session = nil
	r.turnBuffer = []ConversationTurn{
		{Role: "user", Text: "hello", Runner: "claude", At: time.Now()},
	}

	// Must not panic.
	r.replayTurnsToSubstrate(context.Background())
}

// TestReplayTurnsToSubstrate_EmptyBuffer verifies the guard when turnBuffer is empty.
func TestReplayTurnsToSubstrate_EmptyBuffer(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	r := NewREPL(buf)
	r.turnBuffer = nil

	// Must not panic.
	r.replayTurnsToSubstrate(context.Background())
}
