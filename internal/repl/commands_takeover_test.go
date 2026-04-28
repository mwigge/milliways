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
	"io"
	"strings"
	"testing"
	"time"
)

// stubRunner is a minimal Runner for testing.
type stubRunner struct {
	name    string
	execErr error
	output  string
}

func (s *stubRunner) Name() string { return s.name }
func (s *stubRunner) Execute(_ context.Context, _ DispatchRequest, w io.Writer) error {
	if s.output != "" {
		_, _ = w.Write([]byte(s.output))
	}
	return s.execErr
}
func (s *stubRunner) Quota() (*QuotaInfo, error)  { return nil, nil }
func (s *stubRunner) AuthStatus() (bool, error)    { return true, nil }
func (s *stubRunner) Login() error                 { return nil }
func (s *stubRunner) Logout() error                { return nil }

func newTakeoverREPL() (*REPL, *bytes.Buffer) {
	buf := new(bytes.Buffer)
	r := NewREPL(buf)
	r.runners["claude"] = &stubRunner{name: "claude"}
	r.runners["codex"] = &stubRunner{name: "codex"}
	r.runners["minimax"] = &stubRunner{name: "minimax"}
	_ = r.SetRunner("claude")
	return r, buf
}

func TestHandleTakeover_ExplicitRunner_SwitchesAndInjectsBriefing(t *testing.T) {
	t.Parallel()

	r, buf := newTakeoverREPL()

	// Pre-load some turns so briefing has content.
	r.turnBuffer = []ConversationTurn{
		{Role: "user", Text: "implement feature X", Runner: "claude", At: time.Now()},
		{Role: "assistant", Text: "I will implement feature X now. Done.", Runner: "claude", At: time.Now()},
	}

	ctx := context.Background()
	err := handleTakeover(ctx, r, "codex")
	if err != nil {
		t.Fatalf("handleTakeover returned unexpected error: %v", err)
	}

	// Runner must have switched to codex.
	if r.runner.Name() != "codex" {
		t.Errorf("runner = %q, want %q", r.runner.Name(), "codex")
	}

	// A synthetic briefing turn must have been prepended.
	if len(r.turnBuffer) == 0 {
		t.Fatal("turnBuffer is empty after takeover")
	}
	first := r.turnBuffer[0]
	if first.Role != "user" {
		t.Errorf("first turn role = %q, want %q", first.Role, "user")
	}
	if !strings.Contains(first.Text, "[TAKEOVER from claude → codex]") {
		t.Errorf("first turn missing TAKEOVER header; got: %q", first.Text)
	}

	// Confirmation message must appear in output.
	out := buf.String()
	if !strings.Contains(out, "[takeover] claude → codex — briefing injected") {
		t.Errorf("output missing confirmation message; got: %q", out)
	}
}

func TestHandleTakeover_UnknownRunner_ReturnsError(t *testing.T) {
	t.Parallel()

	r, _ := newTakeoverREPL()
	ctx := context.Background()

	err := handleTakeover(ctx, r, "gemini")
	if err == nil {
		t.Fatal("expected error for unknown runner, got nil")
	}
	if !strings.Contains(err.Error(), "Unknown runner: gemini") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "Unknown runner: gemini")
	}

	// Runner must not have changed.
	if r.runner.Name() != "claude" {
		t.Errorf("runner changed unexpectedly to %q", r.runner.Name())
	}
}

func TestHandleTakeover_SameRunner_ReturnsError(t *testing.T) {
	t.Parallel()

	r, _ := newTakeoverREPL()
	ctx := context.Background()

	err := handleTakeover(ctx, r, "claude")
	if err == nil {
		t.Fatal("expected error when targeting same runner, got nil")
	}
	if !strings.Contains(err.Error(), "Already on claude") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "Already on claude")
	}
}

func TestHandleTakeover_NoArgs_NoRing_ReturnsError(t *testing.T) {
	t.Parallel()

	r, _ := newTakeoverREPL()
	ctx := context.Background()

	err := handleTakeover(ctx, r, "")
	if err == nil {
		t.Fatal("expected error with no args and no ring, got nil")
	}
	if !strings.Contains(err.Error(), "No target runner") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "No target runner")
	}
}

func TestHandleTakeover_NoArgs_RingActive_AdvancesRing(t *testing.T) {
	t.Parallel()

	r, buf := newTakeoverREPL()
	ctx := context.Background()

	// Configure ring: claude(pos=0) → codex → minimax
	r.ring = &RingConfig{Runners: []string{"claude", "codex", "minimax"}, Pos: 0}

	err := handleTakeover(ctx, r, "")
	if err != nil {
		t.Fatalf("handleTakeover returned unexpected error: %v", err)
	}

	// Ring position must have advanced.
	if r.ring.Pos != 1 {
		t.Errorf("ring.Pos = %d, want 1", r.ring.Pos)
	}

	// Runner must be codex (the next in ring after pos=0).
	if r.runner.Name() != "codex" {
		t.Errorf("runner = %q, want %q", r.runner.Name(), "codex")
	}

	out := buf.String()
	if !strings.Contains(out, "[takeover]") {
		t.Errorf("output missing [takeover] confirmation; got: %q", out)
	}
}
