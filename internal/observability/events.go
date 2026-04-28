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

import "time"

const (
	// EventKindContextProjectHits records injected project context from MemPalace.
	EventKindContextProjectHits = "context.project_hits"
	// EventKindToolCalled records a tool execution.
	EventKindToolCalled = "tool.called"
	// EventKindToolBlocked records a blocked tool execution.
	EventKindToolBlocked = "tool.blocked"
	// EventKindSessionCompact records transcript compaction.
	EventKindSessionCompact = "session.compact"
	// EventKindSessionSummary records an end-of-session summary.
	EventKindSessionSummary = "session.summary"
	// EventKindMemoryRetrieve records durable memory retrieval.
	EventKindMemoryRetrieve = "memory.retrieve"
	// EventKindMemoryWrite records durable memory writes.
	EventKindMemoryWrite = "memory.write"
)

// Event is a structured runtime event in Milliways.
type Event struct {
	ID             string
	ConversationID string
	BlockID        string
	SegmentID      string
	Kind           string
	Provider       string
	Text           string
	At             time.Time
	Fields         map[string]string
}

// Sink consumes runtime events.
type Sink interface {
	Emit(Event)
}

// NopSink discards runtime events.
type NopSink struct{}

// Emit discards the event.
func (NopSink) Emit(Event) {}

// FuncSink adapts a function to a Sink.
type FuncSink func(Event)

// Emit forwards the event to the wrapped function.
func (f FuncSink) Emit(evt Event) {
	if f != nil {
		f(evt)
	}
}

// MultiSink fans out events to multiple sinks.
type MultiSink []Sink

// Emit forwards the event to each sink.
func (m MultiSink) Emit(evt Event) {
	for _, sink := range m {
		if sink != nil {
			sink.Emit(evt)
		}
	}
}
