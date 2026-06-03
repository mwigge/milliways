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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultTraceMaxBytes = int64(100 * 1024 * 1024)

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
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create trace dir: %w", err)
	}
	_ = os.Chmod(dir, 0o700)
	path := filepath.Join(dir, sessionID+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open trace file: %w", err)
	}
	_ = os.Chmod(path, 0o600)
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

	seen := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		seen[baseTraceSessionName(name)] = struct{}{}
	}
	sessions := make([]string, 0, len(seen))
	for session := range seen {
		sessions = append(sessions, session)
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
	paths, err := traceSessionPaths(traceDir, sessionID)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return ReadTraceEventsFromPath(filepath.Join(traceDir, sessionID+".jsonl"))
	}
	var all []AgentTraceEvent
	for _, path := range paths {
		events, err := ReadTraceEventsFromPath(path)
		if err != nil {
			return nil, err
		}
		all = append(all, events...)
	}
	return all, nil
}

func baseTraceSessionName(name string) string {
	name = strings.TrimSuffix(name, ".jsonl")
	parts := strings.Split(name, ".")
	for i := 1; i < len(parts); i++ {
		if looksLikeTraceRotationStamp(strings.Join(parts[i:], ".")) {
			return strings.Join(parts[:i], ".")
		}
	}
	return name
}

func looksLikeTraceRotationStamp(value string) bool {
	for _, layout := range []string{"20060102T150405Z", "20060102T150405.000000000Z", "20060102T150405.000000000Z.000"} {
		if len(value) == len(layout) {
			if _, err := time.Parse(layout, value); err == nil {
				return true
			}
		}
	}
	return false
}

func traceSessionPaths(traceDir, sessionID string) ([]string, error) {
	entries, err := os.ReadDir(traceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read trace directory: %w", err)
	}
	var rotated, current []string
	currentName := sessionID + ".jsonl"
	prefix := sessionID + "."
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case name == currentName:
			current = append(current, filepath.Join(traceDir, name))
		case strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".jsonl") && baseTraceSessionName(name) == sessionID:
			rotated = append(rotated, filepath.Join(traceDir, name))
		}
	}
	sort.Strings(rotated)
	return append(rotated, current...), nil
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
		event, err := parseTraceEventLine([]byte(line))
		if err != nil {
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
			if err := t.rotateTraceFileIfNeededLocked(); err != nil {
				return err
			}
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

func (t *TraceEmitter) rotateTraceFileIfNeededLocked() error {
	maxBytes := traceMaxBytes()
	if maxBytes <= 0 || t.file == nil || t.filePath == "" {
		return nil
	}
	info, err := os.Stat(t.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat trace file: %w", err)
	}
	if info.Size() < maxBytes {
		return nil
	}
	if err := t.file.Close(); err != nil {
		return fmt.Errorf("close trace file for rotation: %w", err)
	}
	rotated, err := t.nextRotatedTracePathLocked()
	if err != nil {
		return err
	}
	if err := os.Rename(t.filePath, rotated); err != nil {
		return fmt.Errorf("rotate trace file: %w", err)
	}
	_ = os.Chmod(rotated, 0o600)
	if err := t.cleanupRotatedTraceFilesLocked(); err != nil {
		return err
	}
	file, err := os.OpenFile(t.filePath, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("reopen trace file after rotation: %w", err)
	}
	_ = os.Chmod(t.filePath, 0o600)
	t.file = file
	return nil
}

func (t *TraceEmitter) nextRotatedTracePathLocked() (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("%s.%s.%03d.jsonl", t.sessionID, stamp, i)
		path := filepath.Join(t.dir, name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", fmt.Errorf("stat rotated trace candidate: %w", err)
		}
	}
	return "", fmt.Errorf("rotate trace file: exhausted unique name attempts")
}

func (t *TraceEmitter) cleanupRotatedTraceFilesLocked() error {
	maxFiles := traceMaxRotatedFiles()
	if maxFiles <= 0 {
		return nil
	}
	paths, err := traceSessionPaths(t.dir, t.sessionID)
	if err != nil {
		return err
	}
	var rotated []string
	current := filepath.Clean(t.filePath)
	for _, path := range paths {
		if filepath.Clean(path) != current {
			rotated = append(rotated, path)
		}
	}
	if len(rotated) <= maxFiles {
		return nil
	}
	sort.Strings(rotated)
	for _, path := range rotated[:len(rotated)-maxFiles] {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove old trace rotation: %w", err)
		}
	}
	return nil
}

func traceMaxBytes() int64 {
	value := strings.TrimSpace(os.Getenv("MILLIWAYS_TRACE_MAX_BYTES"))
	if value == "" {
		return defaultTraceMaxBytes
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return defaultTraceMaxBytes
	}
	return n
}

func traceMaxRotatedFiles() int {
	value := strings.TrimSpace(os.Getenv("MILLIWAYS_TRACE_MAX_FILES"))
	if value == "" {
		return 16
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 16
	}
	return n
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
