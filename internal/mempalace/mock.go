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
	"sort"
	"strings"
	"sync"
)

// MockPalace is an in-memory Palace for tests.
type MockPalace struct {
	mu       sync.Mutex
	hits     []SearchResult
	writes   []SearchResult
	byWing   map[string][]string
	allWings []string
}

var _ Palace = (*MockPalace)(nil)

// NewMockPalace returns a deterministic in-memory Palace.
func NewMockPalace(hits []SearchResult) *MockPalace {
	clonedHits := append([]SearchResult(nil), hits...)
	byWing := make(map[string][]string)
	wingSet := make(map[string]struct{})
	for _, hit := range clonedHits {
		if hit.Wing != "" {
			wingSet[hit.Wing] = struct{}{}
		}
		if hit.Room != "" {
			byWing[hit.Wing] = appendUnique(byWing[hit.Wing], hit.Room)
		}
	}
	allWings := make([]string, 0, len(wingSet))
	for wing := range wingSet {
		allWings = append(allWings, wing)
	}
	sort.Strings(allWings)
	for wing := range byWing {
		sort.Strings(byWing[wing])
	}
	return &MockPalace{hits: clonedHits, byWing: byWing, allWings: allWings}
}

// Search returns deterministic matches filtered by substring.
func (m *MockPalace) Search(_ context.Context, query string, limit int) ([]SearchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	needle := strings.ToLower(strings.TrimSpace(query))
	results := make([]SearchResult, 0, len(m.hits)+len(m.writes))
	for _, candidate := range append(append([]SearchResult(nil), m.hits...), m.writes...) {
		if needle == "" || strings.Contains(strings.ToLower(candidate.Content), needle) || strings.Contains(strings.ToLower(candidate.FactSummary), needle) {
			results = append(results, candidate)
		}
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// Write records a durable memory write.
func (m *MockPalace) Write(_ context.Context, wing, room, drawer string, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes = append(m.writes, SearchResult{Wing: wing, Room: room, DrawerID: drawer, Content: content, Relevance: 1})
	m.byWing[wing] = appendUnique(m.byWing[wing], room)
	if !contains(m.allWings, wing) {
		m.allWings = append(m.allWings, wing)
		sort.Strings(m.allWings)
	}
	return nil
}

// ListWings returns known wings.
func (m *MockPalace) ListWings(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.allWings...), nil
}

// ListRooms returns known rooms for a wing.
func (m *MockPalace) ListRooms(_ context.Context, wing string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.byWing[wing]...), nil
}

func appendUnique(items []string, value string) []string {
	if value == "" || contains(items, value) {
		return items
	}
	return append(items, value)
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
