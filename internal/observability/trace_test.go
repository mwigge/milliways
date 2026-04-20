package observability

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
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

	for _, name := range []string{"b.jsonl", "a.jsonl", "ignore.txt"} {
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
