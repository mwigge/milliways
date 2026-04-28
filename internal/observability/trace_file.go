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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var traceDirPath = defaultTraceDirPath

// OpenTraceFile opens a trace file under ~/.config/milliways/traces.
func OpenTraceFile(sessionID string) (*os.File, error) {
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	dir, err := traceDirPath()
	if err != nil {
		return nil, fmt.Errorf("resolve trace dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create trace dir: %w", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open trace file %s: %w", path, err)
	}
	return file, nil
}

// WriteTraceEvent writes one flattened trace event as JSONL.
func WriteTraceEvent(f traceFile, event AgentTraceEvent) error {
	if f == nil {
		return errors.New("trace file is nil")
	}
	line, err := json.Marshal(flattenTraceEvent(event))
	if err != nil {
		return fmt.Errorf("marshal trace event: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write trace event: %w", err)
	}
	return nil
}

// ReadTraceFile reads all trace events for a session.
func ReadTraceFile(sessionID string) ([]AgentTraceEvent, error) {
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	if looksLikeTracePath(sessionID) {
		file, err := os.Open(sessionID)
		if err != nil {
			return nil, fmt.Errorf("open trace file: %w", err)
		}
		defer file.Close()
		return ParseTraceEvents(file)
	}
	dir, err := traceDirPath()
	if err != nil {
		return nil, fmt.Errorf("resolve trace dir: %w", err)
	}
	file, err := os.Open(filepath.Join(dir, sessionID+".jsonl"))
	if err != nil {
		return nil, fmt.Errorf("open trace file: %w", err)
	}
	defer file.Close()
	return ParseTraceEvents(file)
}

// ParseTraceEvents parses flattened trace JSONL content.
func ParseTraceEvents(reader io.Reader) ([]AgentTraceEvent, error) {
	if reader == nil {
		return nil, errors.New("reader is nil")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var events []AgentTraceEvent
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(line) == 0 {
			continue
		}
		event, err := parseFlattenedTraceEvent(line)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan trace events: %w", err)
	}
	return events, nil
}

func defaultTraceDirPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "milliways", "traces"), nil
}

func flattenTraceEvent(event AgentTraceEvent) map[string]any {
	flat := map[string]any{
		"id":      event.ID,
		"type":    event.Type,
		"session": event.SessionID,
		"ts":      event.Timestamp.UTC().Format(time.RFC3339Nano),
	}
	for key, value := range event.Data {
		if _, exists := flat[key]; exists {
			continue
		}
		flat[key] = value
	}
	return flat
}

func parseFlattenedTraceEvent(line []byte) (AgentTraceEvent, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return AgentTraceEvent{}, fmt.Errorf("decode flattened trace event: %w", err)
	}
	var event AgentTraceEvent
	for key, value := range raw {
		switch key {
		case "id":
			if err := json.Unmarshal(value, &event.ID); err != nil {
				return AgentTraceEvent{}, fmt.Errorf("decode trace id: %w", err)
			}
		case "type":
			if err := json.Unmarshal(value, &event.Type); err != nil {
				return AgentTraceEvent{}, fmt.Errorf("decode trace type: %w", err)
			}
		case "session":
			if err := json.Unmarshal(value, &event.SessionID); err != nil {
				return AgentTraceEvent{}, fmt.Errorf("decode trace session: %w", err)
			}
		case "ts":
			var ts string
			if err := json.Unmarshal(value, &ts); err != nil {
				return AgentTraceEvent{}, fmt.Errorf("decode trace timestamp: %w", err)
			}
			parsed, err := time.Parse(time.RFC3339Nano, ts)
			if err != nil {
				return AgentTraceEvent{}, fmt.Errorf("parse trace timestamp: %w", err)
			}
			event.Timestamp = parsed.UTC()
		default:
			if event.Data == nil {
				event.Data = make(map[string]any)
			}
			var decoded any
			if err := json.Unmarshal(value, &decoded); err != nil {
				return AgentTraceEvent{}, fmt.Errorf("decode trace data %s: %w", key, err)
			}
			event.Data[key] = decoded
		}
	}
	event.At = event.Timestamp
	return event, nil
}

func looksLikeTracePath(value string) bool {
	if filepath.Ext(value) != ".jsonl" {
		return false
	}
	return strings.Contains(value, string(os.PathSeparator))
}
