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

package maitre

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestNewHookRunner_GroupsByEvent(t *testing.T) {
	t.Parallel()
	hooks := []HookConfig{
		{Event: HookPreRoute, Command: "echo", Args: []string{"pre1"}},
		{Event: HookPreRoute, Command: "echo", Args: []string{"pre2"}},
		{Event: HookPostDispatch, Command: "echo", Args: []string{"post1"}},
	}
	runner := NewHookRunner(hooks)

	if !runner.HasHooks(HookPreRoute) {
		t.Error("expected PreRoute hooks")
	}
	if !runner.HasHooks(HookPostDispatch) {
		t.Error("expected PostDispatch hooks")
	}
	if runner.HasHooks(HookSessionStart) {
		t.Error("expected no SessionStart hooks")
	}
}

func TestHookRunner_RunNonBlocking(t *testing.T) {
	t.Parallel()
	hooks := []HookConfig{
		{Event: HookPreRoute, Command: "echo", Args: []string{"hello"}, Blocking: false},
	}
	runner := NewHookRunner(hooks)

	err := runner.Run(HookPreRoute, HookContext{Kitchen: "claude"})
	if err != nil {
		t.Errorf("non-blocking hook should not return error: %v", err)
	}
}

func TestHookRunner_RunBlockingSuccess(t *testing.T) {
	t.Parallel()
	hooks := []HookConfig{
		{Event: HookPreDispatch, Command: "true", Blocking: true},
	}
	runner := NewHookRunner(hooks)

	err := runner.Run(HookPreDispatch, HookContext{})
	if err != nil {
		t.Errorf("blocking hook with 'true' should succeed: %v", err)
	}
}

func TestHookRunner_RunBlockingFailure(t *testing.T) {
	t.Parallel()
	hooks := []HookConfig{
		{Event: HookPreDispatch, Command: "false", Blocking: true},
	}
	runner := NewHookRunner(hooks)

	err := runner.Run(HookPreDispatch, HookContext{})
	if err == nil {
		t.Error("blocking hook with 'false' should return error")
	}
}

func TestHookRunner_RunNoHooks(t *testing.T) {
	t.Parallel()
	runner := NewHookRunner(nil)

	err := runner.Run(HookSessionStart, HookContext{})
	if err != nil {
		t.Errorf("no hooks should not error: %v", err)
	}
}

func TestHookRunner_NonBlockingFailureDoesNotAbort(t *testing.T) {
	t.Parallel()
	hooks := []HookConfig{
		{Event: HookPostDispatch, Command: "false", Blocking: false},
		{Event: HookPostDispatch, Command: "true", Blocking: false},
	}
	runner := NewHookRunner(hooks)

	// Both run; first fails but doesn't abort
	err := runner.Run(HookPostDispatch, HookContext{})
	if err != nil {
		t.Errorf("non-blocking failures should not return error: %v", err)
	}
}

func TestParseHookEvent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  HookEvent
		err   bool
	}{
		{"session_start", HookSessionStart, false},
		{"pre-route", HookPreRoute, false},
		{"POST_DISPATCH", HookPostDispatch, false},
		{"session-end", HookSessionEnd, false},
		{"invalid", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseHookEvent(tt.input)
			if tt.err && err == nil {
				t.Error("expected error")
			}
			if !tt.err && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildHookEnv(t *testing.T) {
	t.Parallel()
	env := buildHookEnv(HookContext{
		Kitchen:  "claude",
		Mode:     "private",
		TaskType: "think",
		Risk:     "high",
	})

	found := map[string]bool{}
	for _, e := range env {
		if e == "MILLIWAYS_KITCHEN=claude" {
			found["kitchen"] = true
		}
		if e == "MILLIWAYS_MODE=private" {
			found["mode"] = true
		}
		if e == "MILLIWAYS_TASK_TYPE=think" {
			found["task_type"] = true
		}
		if e == "MILLIWAYS_RISK=high" {
			found["risk"] = true
		}
	}

	for _, key := range []string{"kitchen", "mode", "task_type", "risk"} {
		if !found[key] {
			t.Errorf("missing env var for %s", key)
		}
	}
}

func TestHookRunner_LogsStructuredMessages(t *testing.T) {
	capture := installHookTestLogger(t)
	runner := NewHookRunner([]HookConfig{{Event: HookPostDispatch, Command: "false", Blocking: false}})

	err := runner.Run(HookPostDispatch, HookContext{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	assertHookLogRecord(t, capture.records(), slog.LevelInfo, "hook executing", "command", "false")
	assertHookLogRecord(t, capture.records(), slog.LevelWarn, "hook failed", "command", "false")
	assertHookLogRecord(t, capture.records(), slog.LevelWarn, "hook failed", "event", HookPostDispatch)

	for _, record := range capture.records() {
		if record.Message != "hook failed" {
			continue
		}
		errAttr, ok := record.Attrs["err"].(error)
		if !ok || !strings.Contains(errAttr.Error(), "exit status") {
			t.Fatalf("hook failed err = %v, want exit status error", record.Attrs["err"])
		}
		return
	}
	t.Fatal("hook failed log not found")
}

var hookTestLoggerMu sync.Mutex

type hookTestLogRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

type hookTestLogCapture struct {
	mu      sync.Mutex
	entries []hookTestLogRecord
}

func installHookTestLogger(t *testing.T) *hookTestLogCapture {
	t.Helper()
	hookTestLoggerMu.Lock()
	capture := &hookTestLogCapture{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() {
		slog.SetDefault(previous)
		hookTestLoggerMu.Unlock()
	})
	return capture
}

func (c *hookTestLogCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *hookTestLogCapture) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]any, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, hookTestLogRecord{Level: record.Level, Message: record.Message, Attrs: attrs})
	return nil
}

func (c *hookTestLogCapture) WithAttrs(_ []slog.Attr) slog.Handler { return c }

func (c *hookTestLogCapture) WithGroup(string) slog.Handler { return c }

func (c *hookTestLogCapture) records() []hookTestLogRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	clone := make([]hookTestLogRecord, len(c.entries))
	copy(clone, c.entries)
	return clone
}

func assertHookLogRecord(t *testing.T, records []hookTestLogRecord, level slog.Level, message, key string, want any) {
	t.Helper()
	for _, record := range records {
		if record.Level != level || record.Message != message {
			continue
		}
		if got := record.Attrs[key]; got != want {
			t.Fatalf("log %q attr %q = %v, want %v", message, key, got, want)
		}
		return
	}
	t.Fatalf("log %q with %s=%v not found", message, key, want)
}
