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

package main

import (
	"bytes"
	"strings"
	"testing"
)

// captureHandoffWriter is a stub handoffWriter that records the last call.
type captureHandoffWriter struct {
	calledTarget   string
	calledFrom     string
	calledBriefing string
	err            error
}

func (c *captureHandoffWriter) WriteHandoff(targetProvider, fromProvider, briefing string) error {
	c.calledTarget = targetProvider
	c.calledFrom = fromProvider
	c.calledBriefing = briefing
	return c.err
}

// TestTakeoverWritesHandoffToMempalace verifies that switchAgent calls
// WriteHandoff on the handoffWriter when a briefing exists and the writer
// is non-nil.
func TestTakeoverWritesHandoffToMempalace(t *testing.T) {
	t.Parallel()

	writer := &captureHandoffWriter{}
	var stdout, stderr bytes.Buffer

	loop := &chatLoop{
		client:        nil, // no real daemon needed
		handoffWriter: writer,
		out:           &stdout,
		errw:          &stderr,
		ring:          []string{},
		turnLog: []chatTurn{
			{Role: "user", Text: "implement cross-pane takeover"},
			{Role: "assistant", AgentID: "claude", Text: "I'll implement the MemPalace handoff"},
		},
	}

	// Simulate that the loop was on "claude" and is switching to "codex".
	// We call the internal handoff function directly so we don't need a
	// real daemon session.
	briefing, ok := loop.buildBriefing("claude", "codex")
	if !ok {
		t.Fatal("buildBriefing should produce a briefing with user turns")
	}

	loop.writeHandoffBriefing("codex", "claude", briefing)

	if writer.calledTarget != "codex" {
		t.Errorf("WriteHandoff target = %q, want %q", writer.calledTarget, "codex")
	}
	if writer.calledFrom != "claude" {
		t.Errorf("WriteHandoff from = %q, want %q", writer.calledFrom, "claude")
	}
	if !strings.Contains(writer.calledBriefing, "implement cross-pane takeover") {
		t.Errorf("WriteHandoff briefing does not contain user turn; got: %q", writer.calledBriefing)
	}
}

// TestTakeoverFallsBackWhenMempalaceUnavailable verifies that when the
// handoffWriter is nil (MemPalace unavailable), writeHandoffBriefing is
// a no-op and does not panic.
func TestTakeoverFallsBackWhenMempalaceUnavailable(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	loop := &chatLoop{
		client:        nil,
		handoffWriter: nil, // writer absent
		out:           &stdout,
		errw:          &stderr,
		ring:          []string{},
		turnLog: []chatTurn{
			{Role: "user", Text: "some prompt"},
		},
	}

	briefing, ok := loop.buildBriefing("claude", "codex")
	if !ok {
		t.Fatal("buildBriefing should return briefing")
	}

	// Must not panic even when handoffWriter is nil.
	loop.writeHandoffBriefing("codex", "claude", briefing)
}

// TestTakeoverWritesHandoff_WriterErrorDoesNotFail verifies that a write
// error from the handoffWriter does not prevent the takeover from completing
// (best-effort, fire-and-forget semantics).
func TestTakeoverWritesHandoff_WriterErrorDoesNotFail(t *testing.T) {
	t.Parallel()

	writer := &captureHandoffWriter{err: errHandoffFailed}
	var stdout, stderr bytes.Buffer

	loop := &chatLoop{
		client:        nil,
		handoffWriter: writer,
		out:           &stdout,
		errw:          &stderr,
		ring:          []string{},
		turnLog: []chatTurn{
			{Role: "user", Text: "some prompt"},
			{Role: "assistant", AgentID: "claude", Text: "some response"},
		},
	}

	briefing, ok := loop.buildBriefing("claude", "codex")
	if !ok {
		t.Fatal("buildBriefing should return briefing")
	}

	// Must not panic or propagate the error.
	loop.writeHandoffBriefing("codex", "claude", briefing)

	// The call was attempted.
	if writer.calledTarget != "codex" {
		t.Errorf("WriteHandoff was not called; target = %q", writer.calledTarget)
	}
}
