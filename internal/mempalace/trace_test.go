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

package mempalace

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/observability"
)

func TestClientWriteAndReadTraceEvents(t *testing.T) {
	t.Parallel()

	event := observability.AgentTraceEvent{
		ID:        "evt-1",
		Type:      observability.AgentTraceTool,
		SessionID: "sess-1",
		Timestamp: time.Date(2026, time.April, 20, 11, 0, 0, 0, time.UTC),
		Data:      map[string]any{"tool": "bash"},
	}
	searchResult := mustTraceSearchResult(t, []SearchResult{{Wing: traceWing, Room: "sess-1", DrawerID: "evt-1", Content: string(mustJSON(t, event)), Relevance: 1}})
	fake := &fakeRPC{resultByTool: map[string]json.RawMessage{
		"mempalace_search": searchResult,
	}}
	client := &Client{rpc: fake}

	if err := client.WriteTraceEvent(context.Background(), event); err != nil {
		t.Fatalf("WriteTraceEvent() error = %v", err)
	}
	if got := fake.argsByTool["mempalace_add_drawer"]["wing"]; got != traceWing {
		t.Fatalf("wing arg = %#v, want %q", got, traceWing)
	}
	if got := fake.argsByTool["mempalace_add_drawer"]["room"]; got != event.SessionID {
		t.Fatalf("room arg = %#v, want %q", got, event.SessionID)
	}

	gotEvents, err := client.ReadTraceEvents(context.Background(), "sess-1", 10)
	if err != nil {
		t.Fatalf("ReadTraceEvents() error = %v", err)
	}
	if len(gotEvents) != 1 {
		t.Fatalf("len(ReadTraceEvents()) = %d, want 1", len(gotEvents))
	}
	event.At = event.Timestamp
	if !reflect.DeepEqual(gotEvents[0], event) {
		t.Fatalf("ReadTraceEvents() = %#v, want %#v", gotEvents[0], event)
	}
}

func TestClientSearchTraces(t *testing.T) {
	t.Parallel()

	newer := observability.AgentTraceEvent{ID: "evt-2", Type: observability.AgentTraceObserve, SessionID: "sess-2", Timestamp: time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC), Data: map[string]any{"path": "otel.go"}}
	older := observability.AgentTraceEvent{ID: "evt-1", Type: observability.AgentTraceObserve, SessionID: "sess-1", Timestamp: time.Date(2026, time.April, 20, 11, 0, 0, 0, time.UTC), Data: map[string]any{"path": "trace.go"}}
	fake := &fakeRPC{resultByTool: map[string]json.RawMessage{
		"mempalace_search": mustTraceSearchResult(t, []SearchResult{{Wing: traceWing, Room: "sess-1", DrawerID: "evt-1", Content: string(mustJSON(t, older)), Relevance: 0.9}, {Wing: traceWing, Room: "sess-2", DrawerID: "evt-2", Content: string(mustJSON(t, newer)), Relevance: 0.95}}),
	}}
	client := &Client{rpc: fake}

	got, err := client.SearchTraces(context.Background(), "trace")
	if err != nil {
		t.Fatalf("SearchTraces() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(SearchTraces()) = %d, want 2", len(got))
	}
	if got[0].ID != newer.ID || got[1].ID != older.ID {
		t.Fatalf("SearchTraces() order = %#v", got)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return encoded
}

func mustTraceSearchResult(t *testing.T, results []SearchResult) json.RawMessage {
	t.Helper()
	inner := mustJSON(t, results)
	wrapper := map[string]any{
		"content": []map[string]string{{
			"type": "text",
			"text": string(inner),
		}},
	}
	return mustJSON(t, wrapper)
}
