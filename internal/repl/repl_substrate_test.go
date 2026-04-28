// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
