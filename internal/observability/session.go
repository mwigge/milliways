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
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var newTracePalace = func() (TracePalaceWriter, error) {
	return nil, nil
}

// TraceSession tracks one agent trace session lifecycle.
type TraceSession struct {
	mu         sync.Mutex
	ID         string
	StartedAt  time.Time
	Events     []AgentTraceEvent
	TokenCount int
	emitter    *TraceEmitter
}

// StartTraceSession creates a new trace session and sink emitter.
func StartTraceSession() (*TraceSession, error) {
	sessionID := uuid.NewString()
	palace, err := newTracePalace()
	if err != nil {
		return nil, fmt.Errorf("open trace palace: %w", err)
	}
	emitter, err := NewTraceEmitter(sessionID, palace)
	if err != nil {
		return nil, err
	}
	return &TraceSession{
		ID:        sessionID,
		StartedAt: time.Now().UTC(),
		emitter:   emitter,
	}, nil
}

// Emit records a trace event in memory and to configured sinks.
func (s *TraceSession) Emit(ctx context.Context, event AgentTraceEvent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if event.SessionID == "" {
		event.SessionID = s.ID
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Data != nil {
		event.Data = cloneTraceData(event.Data)
	}
	s.Events = append(s.Events, event)
	emitter := s.emitter
	s.mu.Unlock()
	if emitter != nil {
		emitter.Emit(ctx, event)
	}
}

// Close flushes all trace sinks for the session.
func (s *TraceSession) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	emitter := s.emitter
	s.emitter = nil
	s.mu.Unlock()
	if emitter == nil {
		return errors.New("trace session already closed")
	}
	return emitter.Close(ctx)
}
