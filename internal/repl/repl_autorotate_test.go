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
)

// limitRunner returns ErrSessionLimit on Execute after writing partial output.
type limitRunner struct {
	name    string
	calledN int
}

func (l *limitRunner) Name() string { return l.name }
func (l *limitRunner) Execute(_ context.Context, _ DispatchRequest, w io.Writer) error {
	l.calledN++
	_, _ = w.Write([]byte("partial output\n"))
	return ErrSessionLimit
}
func (l *limitRunner) Quota() (*QuotaInfo, error)  { return nil, nil }
func (l *limitRunner) AuthStatus() (bool, error)    { return true, nil }
func (l *limitRunner) Login() error                 { return nil }
func (l *limitRunner) Logout() error                { return nil }

// normalRunner answers normally without hitting the session limit.
type normalRunner struct {
	name    string
	calledN int
	output  string
}

func (n *normalRunner) Name() string { return n.name }
func (n *normalRunner) Execute(_ context.Context, _ DispatchRequest, w io.Writer) error {
	n.calledN++
	if n.output != "" {
		_, _ = w.Write([]byte(n.output))
	}
	return nil
}
func (n *normalRunner) Quota() (*QuotaInfo, error)  { return nil, nil }
func (n *normalRunner) AuthStatus() (bool, error)    { return true, nil }
func (n *normalRunner) Login() error                 { return nil }
func (n *normalRunner) Logout() error                { return nil }

func newAutoRotateREPL() (*REPL, *bytes.Buffer, *limitRunner, *normalRunner) {
	out := new(bytes.Buffer)
	r := NewREPL(out)

	runnerA := &limitRunner{name: "runner-a"}
	runnerB := &normalRunner{name: "runner-b", output: "response from runner-b\n"}

	r.runners["runner-a"] = runnerA
	r.runners["runner-b"] = runnerB
	_ = r.SetRunner("runner-a")

	r.ring = &RingConfig{Runners: []string{"runner-a", "runner-b"}, Pos: 0}

	return r, out, runnerA, runnerB
}

func TestAutoRotate_SessionLimit_SwitchesToNextRunner(t *testing.T) {
	t.Parallel()

	r, out, runnerA, runnerB := newAutoRotateREPL()
	ctx := context.Background()

	err := r.handlePrompt(ctx, "do the thing")
	if err != nil {
		t.Fatalf("handlePrompt returned unexpected error: %v", err)
	}

	// runner-a must have been called (it emits the sentinel).
	if runnerA.calledN == 0 {
		t.Error("runner-a was never called")
	}

	// runner-b must have been called (auto-rotate switches to it).
	if runnerB.calledN == 0 {
		t.Error("runner-b was never called after auto-rotate")
	}

	// Active runner must now be runner-b.
	if r.runner.Name() != "runner-b" {
		t.Errorf("active runner = %q after auto-rotate, want %q", r.runner.Name(), "runner-b")
	}

	output := out.String()
	// Must print the auto-takeover message.
	if !strings.Contains(output, "[auto-takeover] runner-a session limit — continuing on runner-b") {
		t.Errorf("output missing [auto-takeover] message; got: %q", output)
	}

	// Sentinel must not appear in visible output sent to the user.
	// Note: teeWriter flushes bytes directly, but the sentinel should not
	// appear as readable text (it's null-delimited). We verify it is stripped
	// from the turn buffer.
	for _, turn := range r.turnBuffer {
		if strings.Contains(turn.Text, SessionLimitSentinel) {
			t.Errorf("turn buffer contains raw sentinel in role=%q runner=%q", turn.Role, turn.Runner)
		}
	}
}

func TestAutoRotate_NoRing_PrintsGuidance(t *testing.T) {
	t.Parallel()

	out := new(bytes.Buffer)
	r := NewREPL(out)

	runnerA := &limitRunner{name: "runner-a"}
	r.runners["runner-a"] = runnerA
	_ = r.SetRunner("runner-a")
	// No ring configured.

	ctx := context.Background()
	err := r.handlePrompt(ctx, "do the thing")
	if err != nil {
		t.Fatalf("handlePrompt returned error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "[session limit]") {
		t.Errorf("output missing [session limit] guidance; got: %q", output)
	}
	if !strings.Contains(output, "/takeover-ring") {
		t.Errorf("output missing /takeover-ring hint; got: %q", output)
	}
}

func TestAutoRotate_RotationCap_HaltsWhenAllRunnersExhausted(t *testing.T) {
	t.Parallel()

	out := new(bytes.Buffer)
	r := NewREPL(out)

	// Both runners hit session limits.
	runnerA := &limitRunner{name: "runner-a"}
	runnerB := &limitRunner{name: "runner-b"}
	r.runners["runner-a"] = runnerA
	r.runners["runner-b"] = runnerB
	_ = r.SetRunner("runner-a")

	r.ring = &RingConfig{Runners: []string{"runner-a", "runner-b"}, Pos: 0}

	ctx := context.Background()
	// Should not loop infinitely — cap by ring length (2).
	err := r.handlePrompt(ctx, "do the thing")
	if err != nil {
		t.Fatalf("handlePrompt returned unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "all runners hit session limits") {
		t.Errorf("output missing exhaustion message; got: %q", output)
	}
}
