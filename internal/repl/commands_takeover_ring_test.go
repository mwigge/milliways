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
	"strings"
	"testing"
)

func newTestREPLWithRunners(runners ...string) (*REPL, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	r := NewREPL(buf)
	for _, name := range runners {
		r.Register(name, &mockRunner{nameVal: name})
	}
	return r, buf
}

func TestHandleTakeoverRing_SetsRing(t *testing.T) {
	t.Parallel()

	r, buf := newTestREPLWithRunners("claude", "codex", "minimax")

	if err := handleTakeoverRing(context.Background(), r, "claude,codex,minimax"); err != nil {
		t.Fatalf("handleTakeoverRing() = %v", err)
	}

	if r.ring == nil {
		t.Fatal("ring is nil after setting")
	}
	if len(r.ring.Runners) != 3 {
		t.Errorf("ring.Runners = %v, want 3 runners", r.ring.Runners)
	}
	if r.ring.Runners[0] != "claude" || r.ring.Runners[1] != "codex" || r.ring.Runners[2] != "minimax" {
		t.Errorf("ring.Runners = %v, unexpected order", r.ring.Runners)
	}
	if r.ring.Pos != 0 {
		t.Errorf("ring.Pos = %d, want 0", r.ring.Pos)
	}

	out := buf.String()
	if !strings.Contains(out, "claude") || !strings.Contains(out, "codex") || !strings.Contains(out, "minimax") {
		t.Errorf("output %q does not mention ring runners", out)
	}
}

func TestHandleTakeoverRing_ClearsRingWithOff(t *testing.T) {
	t.Parallel()

	r, buf := newTestREPLWithRunners("claude", "codex")
	r.ring = &RingConfig{Runners: []string{"claude", "codex"}, Pos: 0}

	if err := handleTakeoverRing(context.Background(), r, "off"); err != nil {
		t.Fatalf("handleTakeoverRing(off) = %v", err)
	}

	if r.ring != nil {
		t.Errorf("ring = %v, want nil after off", r.ring)
	}

	out := buf.String()
	if !strings.Contains(out, "cleared") {
		t.Errorf("output %q does not mention cleared", out)
	}
}

func TestHandleTakeoverRing_ClearsRingWithClear(t *testing.T) {
	t.Parallel()

	r, _ := newTestREPLWithRunners("claude", "codex")
	r.ring = &RingConfig{Runners: []string{"claude", "codex"}, Pos: 0}

	if err := handleTakeoverRing(context.Background(), r, "clear"); err != nil {
		t.Fatalf("handleTakeoverRing(clear) = %v", err)
	}

	if r.ring != nil {
		t.Errorf("ring = %v, want nil after clear", r.ring)
	}
}

func TestHandleTakeoverRing_UnknownRunnerRejected(t *testing.T) {
	t.Parallel()

	r, buf := newTestREPLWithRunners("claude", "codex")

	if err := handleTakeoverRing(context.Background(), r, "claude,unknown"); err != nil {
		t.Fatalf("handleTakeoverRing() returned error = %v, want nil (rejection via output)", err)
	}

	if r.ring != nil {
		t.Error("ring should not be set when an unknown runner is present")
	}

	out := buf.String()
	if !strings.Contains(out, "Unknown runner") {
		t.Errorf("output %q does not mention Unknown runner", out)
	}
}

func TestHandleTakeoverRing_BareShowsStatus_NoRing(t *testing.T) {
	t.Parallel()

	r, buf := newTestREPLWithRunners("claude", "codex")

	if err := handleTakeoverRing(context.Background(), r, ""); err != nil {
		t.Fatalf("handleTakeoverRing() = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "No rotation ring") {
		t.Errorf("output %q does not mention no ring configured", out)
	}
}

func TestHandleTakeoverRing_BareShowsStatus_WithRing(t *testing.T) {
	t.Parallel()

	r, buf := newTestREPLWithRunners("claude", "codex", "minimax")
	r.ring = &RingConfig{Runners: []string{"claude", "codex", "minimax"}, Pos: 1}

	if err := handleTakeoverRing(context.Background(), r, ""); err != nil {
		t.Fatalf("handleTakeoverRing() = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "claude") {
		t.Errorf("output %q does not include ring runners", out)
	}
	// Should show current position (codex at pos 1)
	if !strings.Contains(out, "codex") {
		t.Errorf("output %q does not include current pos runner", out)
	}
}

func TestHandleTakeoverRing_RegisteredInCommandMap(t *testing.T) {
	t.Parallel()

	if _, ok := commandHandlers["takeover-ring"]; !ok {
		t.Error("takeover-ring not registered in commandHandlers")
	}
}
