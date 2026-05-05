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

package daemon

import (
	"errors"
	"sync"
	"time"
)

// ErrGroupNotFound is returned when a group_id is not in the store.
var ErrGroupNotFound = errors.New("group not found")

// parallelSlotRecord holds per-slot state for a parallel group.
type parallelSlotRecord struct {
	Handle      int64
	Provider    string
	Status      string
	StartedAt   time.Time
	CompletedAt time.Time
	TokensIn    int
	TokensOut   int
}

// parallelGroupRecord holds a parallel dispatch group.
type parallelGroupRecord struct {
	GroupID     string
	Prompt      string
	Status      string // "running" | "done" | "error"
	CreatedAt   time.Time
	CompletedAt time.Time
	Slots       []parallelSlotRecord
}

// parallelStore is an in-memory store for parallel dispatch groups.
// Groups are retained in insertion order; the store keeps at most maxGroups.
type parallelStore struct {
	mu       sync.RWMutex
	groups   []*parallelGroupRecord
	byID     map[string]*parallelGroupRecord
	maxItems int
}

const defaultParallelMaxGroups = 20

// newParallelStore returns an initialised store.
func newParallelStore() *parallelStore {
	return &parallelStore{
		byID:     make(map[string]*parallelGroupRecord),
		maxItems: defaultParallelMaxGroups,
	}
}

// put inserts or replaces a group record. If the store is at capacity the
// oldest entry is evicted.
func (s *parallelStore) put(g *parallelGroupRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.byID[g.GroupID]; ok {
		*existing = *g
		return
	}
	if len(s.groups) >= s.maxItems {
		evict := s.groups[0]
		s.groups = s.groups[1:]
		delete(s.byID, evict.GroupID)
	}
	s.groups = append(s.groups, g)
	s.byID[g.GroupID] = g
}

// get returns a copy of the group record, or ErrGroupNotFound.
func (s *parallelStore) get(groupID string) (parallelGroupRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.byID[groupID]
	if !ok {
		return parallelGroupRecord{}, ErrGroupNotFound
	}
	// Return a shallow copy to prevent callers mutating store state.
	cp := *g
	cp.Slots = make([]parallelSlotRecord, len(g.Slots))
	copy(cp.Slots, g.Slots)
	return cp, nil
}

// list returns copies of the most recent groups (newest first), up to limit.
func (s *parallelStore) list(limit int) []parallelGroupRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.groups)
	if limit > 0 && n > limit {
		n = limit
	}
	out := make([]parallelGroupRecord, n)
	// Return newest first (reverse iteration).
	for i := 0; i < n; i++ {
		src := s.groups[len(s.groups)-1-i]
		out[i] = *src
		out[i].Slots = make([]parallelSlotRecord, len(src.Slots))
		copy(out[i].Slots, src.Slots)
	}
	return out
}
