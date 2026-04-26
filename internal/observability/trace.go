package observability

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const (
	// AgentTraceThink records agent reasoning work.
	AgentTraceThink = "agent.think"
	// AgentTraceDelegate records delegation work.
	AgentTraceDelegate = "agent.delegate"
	// AgentTraceTool records tool execution work.
	AgentTraceTool = "agent.tool"
	// AgentTraceObserve records observations.
	AgentTraceObserve = "agent.observe"
	// AgentTraceDecide records decisions.
	AgentTraceDecide = "agent.decide"
)

// AgentTraceEvent is a normalized agent trace record.
type AgentTraceEvent struct {
	ID          string         `json:"id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	Timestamp   time.Time      `json:"timestamp"`
	At          time.Time      `json:"-"`
	Type        string         `json:"type"`
	Description string         `json:"description,omitempty"`
	Actor       string         `json:"actor,omitempty"`
	Parent      string         `json:"parent,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
}

type agentTraceEventAlias struct {
	ID             string                     `json:"id,omitempty"`
	SessionID      string                     `json:"session_id,omitempty"`
	TraceSession   string                     `json:"session,omitempty"`
	ConversationID string                     `json:"conversation_id,omitempty"`
	Timestamp      string                     `json:"timestamp,omitempty"`
	OccurredAt     string                     `json:"ts,omitempty"`
	At             string                     `json:"at,omitempty"`
	Time           string                     `json:"time,omitempty"`
	Type           string                     `json:"type,omitempty"`
	Kind           string                     `json:"kind,omitempty"`
	Description    string                     `json:"description,omitempty"`
	Text           string                     `json:"text,omitempty"`
	Message        string                     `json:"message,omitempty"`
	Actor          string                     `json:"actor,omitempty"`
	Provider       string                     `json:"provider,omitempty"`
	Parent         string                     `json:"parent,omitempty"`
	Data           map[string]json.RawMessage `json:"data,omitempty"`
	Fields         map[string]string          `json:"fields,omitempty"`
}

// TracePalaceWriter persists trace events to durable storage.
type TracePalaceWriter interface {
	WriteTraceEvent(ctx context.Context, event AgentTraceEvent) error
}

type traceFile interface {
	Close() error
	Write([]byte) (int, error)
	Sync() error
}

// TraceEmitter writes trace events to local and durable sinks.
type TraceEmitter struct {
	mu         sync.Mutex
	sessionID  string
	dir        string
	filePath   string
	otelTracer trace.Tracer
	palace     TracePalaceWriter
	file       traceFile
	buf        []AgentTraceEvent
	flushEvery int
	closed     bool
	lastErr    error
}

// UnmarshalJSON accepts both dedicated trace events and persisted runtime events.
func (e *AgentTraceEvent) UnmarshalJSON(data []byte) error {
	var raw agentTraceEventAlias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	e.ID = raw.ID
	e.SessionID = traceFirstNonEmpty(raw.SessionID, raw.TraceSession, raw.ConversationID)
	e.Type = traceFirstNonEmpty(raw.Type, raw.Kind)
	e.Description = traceFirstNonEmpty(raw.Description, raw.Text, raw.Message)
	e.Actor = traceFirstNonEmpty(raw.Actor, raw.Provider)
	e.Parent = raw.Parent
	e.Data = mergeFieldsIntoData(decodeRawData(raw.Data), raw.Fields)

	parsedAt, err := parseTraceTimestamp(traceFirstNonEmpty(raw.Timestamp, raw.OccurredAt, raw.At, raw.Time))
	if err != nil {
		return err
	}
	e.Timestamp = parsedAt
	e.At = parsedAt

	return nil
}

// MarshalJSON writes the canonical trace event representation.
func (e AgentTraceEvent) MarshalJSON() ([]byte, error) {
	payload := map[string]any{
		"id":      e.ID,
		"session": e.SessionID,
		"ts":      e.Timestamp.UTC().Format(time.RFC3339Nano),
		"type":    e.Type,
	}
	if e.Description != "" {
		payload["description"] = e.Description
	}
	if e.Actor != "" {
		payload["actor"] = e.Actor
	}
	if e.Parent != "" {
		payload["parent"] = e.Parent
	}
	if len(e.Data) > 0 {
		payload["data"] = e.Data
	}
	return json.Marshal(payload)
}

// NewTraceEmitter creates a trace emitter for a session.
func NewTraceEmitter(sessionID string, palace TracePalaceWriter) (*TraceEmitter, error) {
	dir, err := TraceDir()
	if err != nil {
		return nil, err
	}
	return newTraceEmitter(sessionID, dir, palace)
}

// NewTraceEmitterForDir creates a trace emitter rooted at the given directory.
func NewTraceEmitterForDir(sessionID, dir string) (*TraceEmitter, error) {
	return newTraceEmitter(sessionID, dir, nil)
}

func newTraceEmitter(sessionID, dir string, palace TracePalaceWriter) (*TraceEmitter, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("session id is required")
	}
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("trace dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create trace dir: %w", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open trace file: %w", err)
	}
	return &TraceEmitter{
		sessionID:  sessionID,
		dir:        dir,
		filePath:   path,
		otelTracer: otel.GetTracerProvider().Tracer(instrumentationName),
		palace:     palace,
		file:       file,
		flushEvery: 1,
	}, nil
}

// Emit records a single trace event.
func (t *TraceEmitter) Emit(ctx context.Context, event AgentTraceEvent) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return t.lastErr
	}
	t.buf = append(t.buf, t.normalizeEvent(event))
	if t.flushEvery > 0 && len(t.buf) >= t.flushEvery {
		t.lastErr = errors.Join(t.lastErr, t.flushLocked(ctx))
	}
	return t.lastErr
}

// EmitBatch records multiple trace events.
func (t *TraceEmitter) EmitBatch(ctx context.Context, events []AgentTraceEvent) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	for _, event := range events {
		t.buf = append(t.buf, t.normalizeEvent(event))
	}
	if t.flushEvery > 0 && len(t.buf) >= t.flushEvery {
		t.lastErr = errors.Join(t.lastErr, t.flushLocked(ctx))
	}
}

// Close flushes buffered events and closes underlying sinks.
func (t *TraceEmitter) Close(ctx context.Context) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return t.lastErr
	}
	err := errors.Join(t.lastErr, t.flushLocked(ctx))
	if t.file != nil {
		err = errors.Join(err, t.file.Close())
	}
	t.closed = true
	t.lastErr = err
	return err
}

// TraceDir returns the default directory that stores trace JSONL files.
func TraceDir() (string, error) {
	return traceDirPath()
}

// ListTraceSessions returns the available trace session IDs.
func ListTraceSessions() ([]string, error) {
	traceDir, err := TraceDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(traceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read trace directory: %w", err)
	}

	sessions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sessions = append(sessions, strings.TrimSuffix(name, ".jsonl"))
	}
	sort.Strings(sessions)
	return sessions, nil
}

// ReadTraceEvents loads and normalizes all events for a session ID.
func ReadTraceEvents(sessionID string) ([]AgentTraceEvent, error) {
	traceDir, err := TraceDir()
	if err != nil {
		return nil, err
	}
	return ReadTraceEventsFromPath(filepath.Join(traceDir, sessionID+".jsonl"))
}

// ReadTraceEventsFromPath loads and normalizes all events from a trace JSONL file.
func ReadTraceEventsFromPath(path string) ([]AgentTraceEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open trace file: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)

	events := make([]AgentTraceEvent, 0)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event AgentTraceEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("decode trace line %d: %w", lineNumber, err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan trace file: %w", err)
	}

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	if len(events) > 0 && events[0].SessionID != "" {
		for i := range events {
			if events[i].SessionID == "" {
				events[i].SessionID = events[0].SessionID
			}
		}
	}

	return events, nil
}

func decodeRawData(in map[string]json.RawMessage) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			out[key] = string(value)
			continue
		}
		out[key] = decoded
	}
	return out
}

func mergeFieldsIntoData(data map[string]any, fields map[string]string) map[string]any {
	if len(fields) == 0 {
		return data
	}
	if data == nil {
		data = make(map[string]any, len(fields))
	}
	for key, value := range fields {
		if _, exists := data[key]; exists {
			continue
		}
		data[key] = value
	}
	return data
}

func cloneTraceData(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func parseTraceTimestamp(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return parsed.UTC(), nil
	}
	parsed, err = time.Parse(time.RFC3339, raw)
	if err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("parse timestamp %q: %w", raw, err)
}

func traceFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (t *TraceEmitter) flushLocked(ctx context.Context) error {
	if len(t.buf) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	pending := append([]AgentTraceEvent(nil), t.buf...)
	for _, event := range pending {
		if t.file != nil {
			if err := WriteTraceEvent(t.file, event); err != nil {
				return fmt.Errorf("write trace file: %w", err)
			}
		}
		if t.palace != nil {
			if err := t.palace.WriteTraceEvent(ctx, event); err != nil {
				return fmt.Errorf("write trace palace: %w", err)
			}
		}
	}
	if t.file != nil {
		if err := t.file.Sync(); err != nil {
			return fmt.Errorf("sync trace file: %w", err)
		}
	}
	t.buf = t.buf[:0]
	return nil
}

func (t *TraceEmitter) normalizeEvent(event AgentTraceEvent) AgentTraceEvent {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.Type == "" {
		event.Type = AgentTraceObserve
	}
	if event.SessionID == "" {
		event.SessionID = t.sessionID
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	event.Data = cloneTraceData(event.Data)
	event.At = event.Timestamp
	return event
}

// TraceFilePath returns the emitter's file path.
func (t *TraceEmitter) TraceFilePath() string {
	if t == nil {
		return ""
	}
	return t.filePath
}

// SessionID returns the emitter session identifier.
func (t *TraceEmitter) SessionID() string {
	if t == nil {
		return ""
	}
	return t.sessionID
}

// MermaidTrace renders a sequence diagram view of the events.
func MermaidTrace(events []AgentTraceEvent) string {
	lines := []string{"sequenceDiagram"}
	for _, event := range sortedTraceEvents(events) {
		lines = append(lines, fmt.Sprintf("    Note over Orchestrator: %s", event.Type))
	}
	return strings.Join(lines, "\n")
}
