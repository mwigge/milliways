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
	"errors"
	"fmt"
	"sort"

	"github.com/mwigge/milliways/internal/observability"
)

const traceWing = "agent-trace"

// WriteTraceEvent stores a trace event in MemPalace.
func (c *Client) WriteTraceEvent(ctx context.Context, event observability.AgentTraceEvent) error {
	if c == nil || c.rpc == nil {
		return errors.New("nil mempalace client")
	}
	if event.SessionID == "" {
		return errors.New("trace session id is required")
	}
	if event.ID == "" {
		return errors.New("trace event id is required")
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal trace event: %w", err)
	}
	return c.Write(ctx, traceWing, event.SessionID, event.ID, string(encoded))
}

// ReadTraceEvents reads trace events for a single session.
func (c *Client) ReadTraceEvents(ctx context.Context, sessionID string, limit int) ([]observability.AgentTraceEvent, error) {
	if c == nil || c.rpc == nil {
		return nil, errors.New("nil mempalace client")
	}
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	searchLimit := limit
	if searchLimit <= 0 {
		searchLimit = 100
	}
	results, err := c.Search(ctx, sessionID, searchLimit)
	if err != nil {
		return nil, err
	}
	events, err := decodeTraceResults(results, func(result SearchResult) bool {
		return result.Wing == traceWing && result.Room == sessionID
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}

// SearchTraces searches trace events by content.
func (c *Client) SearchTraces(ctx context.Context, query string) ([]observability.AgentTraceEvent, error) {
	if c == nil || c.rpc == nil {
		return nil, errors.New("nil mempalace client")
	}
	results, err := c.Search(ctx, query, 100)
	if err != nil {
		return nil, err
	}
	events, err := decodeTraceResults(results, func(result SearchResult) bool {
		return result.Wing == traceWing
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})
	return events, nil
}

func decodeTraceResults(results []SearchResult, keep func(SearchResult) bool) ([]observability.AgentTraceEvent, error) {
	events := make([]observability.AgentTraceEvent, 0, len(results))
	for _, result := range results {
		if keep != nil && !keep(result) {
			continue
		}
		var event observability.AgentTraceEvent
		if err := json.Unmarshal([]byte(result.Content), &event); err != nil {
			return nil, fmt.Errorf("decode trace event %q: %w", result.DrawerID, err)
		}
		events = append(events, event)
	}
	return events, nil
}
