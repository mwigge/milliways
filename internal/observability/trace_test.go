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

package observability

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

type fakeTracePalace struct {
	events []AgentTraceEvent
	err    error
}

func (f *fakeTracePalace) WriteTraceEvent(_ context.Context, event AgentTraceEvent) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, event)
	return nil
}

func TestReadTraceEventsFromPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-1.jsonl")
	content := []byte("{\"session_id\":\"session-1\",\"timestamp\":\"2026-04-20T10:00:00Z\",\"type\":\"delegate\",\"description\":\"coder-go\"}\n{" +
		"\"conversation_id\":\"session-1\",\"at\":\"2026-04-20T10:01:00Z\",\"kind\":\"tool.called\",\"text\":\"Bash\",\"fields\":{\"tool_name\":\"Bash\"}}\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	events, err := ReadTraceEventsFromPath(path)
	if err != nil {
		t.Fatalf("ReadTraceEventsFromPath() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[1].SessionID != "session-1" {
		t.Fatalf("events[1].SessionID = %q, want session-1", events[1].SessionID)
	}
	if events[1].Type != "tool.called" {
		t.Fatalf("events[1].Type = %q, want tool.called", events[1].Type)
	}
	if events[1].Description != "Bash" {
		t.Fatalf("events[1].Description = %q, want Bash", events[1].Description)
	}
	if got := events[1].Timestamp.UTC(); !got.Equal(time.Date(2026, time.April, 20, 10, 1, 0, 0, time.UTC)) {
		t.Fatalf("events[1].Timestamp = %s", got)
	}
	if got, ok := events[1].Data["tool_name"].(string); !ok || got != "Bash" {
		t.Fatalf("events[1].Data[tool_name] = %#v, want Bash", events[1].Data["tool_name"])
	}
}

func TestListTraceSessions(t *testing.T) {
	tempDir := t.TempDir()
	oldTraceDir := traceDirPath
	traceDirPath = func() (string, error) { return tempDir, nil }
	t.Cleanup(func() { traceDirPath = oldTraceDir })

	for _, name := range []string{"b.jsonl", "a.jsonl", "a.20260517T120000Z.jsonl", "ignore.txt"} {
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte("\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	sessions, err := ListTraceSessions()
	if err != nil {
		t.Fatalf("ListTraceSessions() error = %v", err)
	}
	if !reflect.DeepEqual(sessions, []string{"a", "b"}) {
		t.Fatalf("sessions = %v, want [a b]", sessions)
	}
}

func TestReadTraceEventsIncludesRotatedFragments(t *testing.T) {
	tempDir := t.TempDir()
	oldTraceDir := traceDirPath
	traceDirPath = func() (string, error) { return tempDir, nil }
	t.Cleanup(func() { traceDirPath = oldTraceDir })

	writeTraceFixture := func(name, id string) {
		path := filepath.Join(tempDir, name)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatalf("OpenFile(%s): %v", name, err)
		}
		defer f.Close()
		if err := WriteTraceEvent(f, AgentTraceEvent{ID: id, SessionID: "sess", Type: AgentTraceTool, Timestamp: time.Now().UTC()}); err != nil {
			t.Fatalf("WriteTraceEvent(%s): %v", name, err)
		}
	}
	writeTraceFixture("sess.20260517T120000Z.jsonl", "old")
	writeTraceFixture("sess.jsonl", "new")

	events, err := ReadTraceEvents("sess")
	if err != nil {
		t.Fatalf("ReadTraceEvents: %v", err)
	}
	if len(events) != 2 || events[0].ID != "old" || events[1].ID != "new" {
		t.Fatalf("events = %#v, want old then new", events)
	}
}

func TestTraceEmitterEmitAndClose(t *testing.T) {
	tempDir := t.TempDir()
	oldTraceDir := traceDirPath
	traceDirPath = func() (string, error) { return tempDir, nil }
	t.Cleanup(func() { traceDirPath = oldTraceDir })

	palace := &fakeTracePalace{}
	emitter, err := NewTraceEmitter("sess-1", palace)
	if err != nil {
		t.Fatalf("NewTraceEmitter() error = %v", err)
	}

	emitter.Emit(context.Background(), AgentTraceEvent{
		Type: AgentTraceTool,
		Data: map[string]any{"tool": "read"},
	})

	if err := emitter.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if len(palace.events) != 1 {
		t.Fatalf("len(palace.events) = %d, want 1", len(palace.events))
	}
	if palace.events[0].SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want sess-1", palace.events[0].SessionID)
	}
	if palace.events[0].ID == "" {
		t.Fatal("expected generated event ID")
	}

	events, err := ReadTraceFile("sess-1")
	if err != nil {
		t.Fatalf("ReadTraceFile() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(ReadTraceFile()) = %d, want 1", len(events))
	}
	if got := events[0].Data["tool"]; got != "read" {
		t.Fatalf("tool data = %#v, want read", got)
	}
}

func TestTraceEmitterRotatesLargeTraceFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("MILLIWAYS_TRACE_MAX_BYTES", "1")
	if err := os.WriteFile(filepath.Join(tempDir, "rotate.jsonl"), []byte("existing\n"), 0o644); err != nil {
		t.Fatalf("prewrite trace file: %v", err)
	}
	emitter, err := NewTraceEmitterForDir("rotate", tempDir)
	if err != nil {
		t.Fatalf("NewTraceEmitterForDir() error = %v", err)
	}
	if err := emitter.Emit(context.Background(), AgentTraceEvent{
		Type: AgentTraceTool,
		Data: map[string]any{"tool": "read"},
	}); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	if err := emitter.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	var rotated bool
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "rotate.") && strings.HasSuffix(entry.Name(), ".jsonl") {
			rotated = true
		}
	}
	if !rotated {
		t.Fatalf("expected rotated trace file, entries=%v", entries)
	}
}

func TestTraceEmitterMultipleRotationsKeepUniqueFragments(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("MILLIWAYS_TRACE_MAX_BYTES", "1")
	if err := os.WriteFile(filepath.Join(tempDir, "rotate-many.jsonl"), []byte("existing\n"), 0o600); err != nil {
		t.Fatalf("prewrite trace file: %v", err)
	}
	emitter, err := NewTraceEmitterForDir("rotate-many", tempDir)
	if err != nil {
		t.Fatalf("NewTraceEmitterForDir() error = %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := emitter.Emit(context.Background(), AgentTraceEvent{
			Type: AgentTraceTool,
			Data: map[string]any{"tool": "read"},
		}); err != nil {
			t.Fatalf("Emit(%d) error = %v", i, err)
		}
	}
	if err := emitter.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	var rotated int
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "rotate-many.") && strings.HasSuffix(entry.Name(), ".jsonl") {
			rotated++
		}
	}
	if rotated < 3 {
		t.Fatalf("rotated fragments = %d, want at least 3; entries=%v", rotated, entries)
	}
	oldTraceDir := traceDirPath
	traceDirPath = func() (string, error) { return tempDir, nil }
	t.Cleanup(func() { traceDirPath = oldTraceDir })
	sessions, err := ListTraceSessions()
	if err != nil {
		t.Fatalf("ListTraceSessionsForTest: %v", err)
	}
	if !reflect.DeepEqual(sessions, []string{"rotate-many"}) {
		t.Fatalf("sessions = %#v, want rotate-many", sessions)
	}
}

func TestTraceEmitterUsesPrivatePermissions(t *testing.T) {
	tempDir := t.TempDir()
	oldTraceDir := traceDirPath
	traceDirPath = func() (string, error) { return tempDir, nil }
	t.Cleanup(func() { traceDirPath = oldTraceDir })

	emitter, err := NewTraceEmitter("private", nil)
	if err != nil {
		t.Fatalf("NewTraceEmitter() error = %v", err)
	}
	if err := emitter.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	dirInfo, err := os.Stat(tempDir)
	if err != nil {
		t.Fatalf("stat trace dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("trace dir mode = %v, want 0700", got)
	}
	fileInfo, err := os.Stat(filepath.Join(tempDir, "private.jsonl"))
	if err != nil {
		t.Fatalf("stat trace file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("trace file mode = %v, want 0600", got)
	}
}

func TestWriteAndParseTraceEvents(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	ts := time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC)
	event := AgentTraceEvent{
		ID:        "evt-1",
		Type:      AgentTraceObserve,
		SessionID: "sess-1",
		Timestamp: ts,
		Data: map[string]any{
			"blocked": true,
			"count":   3,
			"tool":    "bash",
		},
	}

	if err := WriteTraceEvent(nopSyncFile{Buffer: buf}, event); err != nil {
		t.Fatalf("WriteTraceEvent() error = %v", err)
	}

	parsed, err := ParseTraceEvents(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ParseTraceEvents() error = %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("len(ParseTraceEvents()) = %d, want 1", len(parsed))
	}
	if parsed[0].ID != event.ID || parsed[0].Type != event.Type || parsed[0].SessionID != event.SessionID {
		t.Fatalf("parsed event identity = %#v", parsed[0])
	}
	if !parsed[0].Timestamp.Equal(ts) {
		t.Fatalf("Timestamp = %v, want %v", parsed[0].Timestamp, ts)
	}
	if !reflect.DeepEqual(parsed[0].Data, map[string]any{"blocked": true, "count": float64(3), "tool": "bash"}) {
		t.Fatalf("Data = %#v, want %#v", parsed[0].Data, event.Data)
	}
}

func TestWriteAndParseFlattenedTraceEventWithCanonicalDataKeys(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	ts := time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC)
	event := AgentTraceEvent{
		ID:        "evt-collision",
		Type:      AgentTraceObserve,
		SessionID: "sess-1",
		Timestamp: ts,
		Data: map[string]any{
			"timestamp": "tool-start",
			"data":      "payload",
		},
	}

	if err := WriteTraceEvent(nopSyncFile{Buffer: buf}, event); err != nil {
		t.Fatalf("WriteTraceEvent() error = %v", err)
	}
	parsed, err := ParseTraceEvents(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ParseTraceEvents() error = %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("len(ParseTraceEvents()) = %d, want 1", len(parsed))
	}
	if parsed[0].Data["timestamp"] != "tool-start" || parsed[0].Data["data"] != "payload" {
		t.Fatalf("Data = %#v", parsed[0].Data)
	}
}

func TestParseCanonicalTraceEventWithDataAndFlattenedKeys(t *testing.T) {
	ts := time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC)
	line, err := AgentTraceEvent{
		ID:        "evt-canonical",
		Type:      AgentTraceObserve,
		SessionID: "sess-1",
		Timestamp: ts,
		Data: map[string]any{
			"data": "nested payload",
			"tool": "bash",
		},
	}.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	parsed, err := ParseTraceEvents(bytes.NewReader(append(line, '\n')))
	if err != nil {
		t.Fatalf("ParseTraceEvents() error = %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("len(ParseTraceEvents()) = %d, want 1", len(parsed))
	}
	if parsed[0].ID != "evt-canonical" || parsed[0].SessionID != "sess-1" || parsed[0].Type != AgentTraceObserve {
		t.Fatalf("parsed event identity = %#v", parsed[0])
	}
	if !parsed[0].Timestamp.Equal(ts) {
		t.Fatalf("Timestamp = %v, want %v", parsed[0].Timestamp, ts)
	}
	if !reflect.DeepEqual(parsed[0].Data, map[string]any{"data": "nested payload", "tool": "bash"}) {
		t.Fatalf("Data = %#v", parsed[0].Data)
	}
}

func TestStartAgentHelpersAndEvents(t *testing.T) {
	t.Parallel()

	ctx, span := StartAgentThinkSpan(context.Background(), "sess-1", "inspect")
	AddEvent(ctx, "step", attribute.String("name", "read"))
	if SpanFromCtx(ctx) == nil {
		t.Fatal("SpanFromCtx() returned nil")
	}
	span.End()

	ctx, span = StartAgentDelegateSpan(context.Background(), "sess-1", "coder-go", "trace", 12, "ok")
	span.End()
	ctx, span = StartAgentToolSpan(ctx, "sess-1", "bash", 5, false)
	span.End()
	ctx, span = StartAgentObserveSpan(ctx, "sess-1", "file", "otel.go")
	span.End()
	_, span = StartAgentDecideSpan(ctx, "sess-1", []string{"a", "b"}, "a")
	span.End()
}

func TestTraceSessionStartEmitClose(t *testing.T) {
	tempDir := t.TempDir()
	oldTraceDir := traceDirPath
	traceDirPath = func() (string, error) { return tempDir, nil }
	t.Cleanup(func() { traceDirPath = oldTraceDir })

	palace := &fakeTracePalace{}
	oldNewTracePalace := newTracePalace
	newTracePalace = func() (TracePalaceWriter, error) { return palace, nil }
	t.Cleanup(func() { newTracePalace = oldNewTracePalace })

	session, err := StartTraceSession()
	if err != nil {
		t.Fatalf("StartTraceSession() error = %v", err)
	}
	if session.ID == "" {
		t.Fatal("expected session ID")
	}

	session.Emit(context.Background(), AgentTraceEvent{Type: AgentTraceDecide, Data: map[string]any{"choice": "tool"}})
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if len(palace.events) != 1 {
		t.Fatalf("len(palace.events) = %d, want 1", len(palace.events))
	}
	path := filepath.Join(tempDir, session.ID+".jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	if len(session.Events) != 1 {
		t.Fatalf("len(session.Events) = %d, want 1", len(session.Events))
	}
}

type nopSyncFile struct {
	*bytes.Buffer
}

func (f nopSyncFile) Close() error { return nil }

func (f nopSyncFile) Sync() error { return nil }
