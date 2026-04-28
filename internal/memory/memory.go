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

package memory

import (
	"context"
	"time"

	"github.com/mwigge/milliways/internal/mempalace"
	"github.com/mwigge/milliways/internal/session"
)

// Service ties together working memory, durable memory, persistence, and token tracking.
type Service struct {
	working   *WorkingMemory
	palace    mempalace.Palace
	persister session.Persister
	tokens    *TokenTracker
}

// NewService constructs a memory facade.
func NewService(working *WorkingMemory, palace mempalace.Palace, persister session.Persister, tokens *TokenTracker) *Service {
	if working == nil {
		working = NewWorkingMemory()
	}
	if tokens == nil {
		tokens = &TokenTracker{}
	}
	return &Service{working: working, palace: palace, persister: persister, tokens: tokens}
}

// Close stops any background cleanup owned by the service.
func (s *Service) Close() {
	if s == nil || s.working == nil {
		return
	}
	s.working.Close()
}

// Set stores a working-memory value.
func (s *Service) Set(key, value string, ttl time.Duration) {
	if s == nil {
		return
	}
	s.working.Set(key, value, ttl)
}

// Get loads a working-memory value.
func (s *Service) Get(key string) (string, bool) {
	if s == nil {
		return "", false
	}
	return s.working.Get(key)
}

// Entries returns active working-memory entries.
func (s *Service) Entries() []MemoryEntry {
	if s == nil {
		return nil
	}
	return s.working.Entries()
}

// BuildPrompt constructs a system prompt with durable context.
func (s *Service) BuildPrompt(hits []mempalace.SearchResult, sess *session.Session) string {
	if s == nil {
		return BuildSystemPrompt(hits, nil, sess)
	}
	return BuildSystemPrompt(hits, s.Entries(), sess)
}

// Search delegates to the configured MemPalace implementation.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]mempalace.SearchResult, error) {
	if s == nil || s.palace == nil {
		return nil, nil
	}
	return s.palace.Search(ctx, query, limit)
}

// Save persists a session snapshot.
func (s *Service) Save(sess session.Session) error {
	if s == nil || s.persister == nil {
		return nil
	}
	return s.persister.Save(sess)
}

// Load restores a session snapshot.
func (s *Service) Load(id string) (session.Session, error) {
	if s == nil || s.persister == nil {
		return session.Session{}, nil
	}
	return s.persister.Load(id)
}

// List returns saved session summaries.
func (s *Service) List() ([]session.SessionSummary, error) {
	if s == nil || s.persister == nil {
		return nil, nil
	}
	return s.persister.List()
}

// AddTokens updates running token totals.
func (s *Service) AddTokens(input, output int) {
	if s == nil {
		return
	}
	s.tokens.Add(input, output)
}

// Totals returns cumulative token totals.
func (s *Service) Totals() (input, output int) {
	if s == nil {
		return 0, 0
	}
	return s.tokens.Totals()
}

// SessionMemoryEntries converts active working memory into persisted session memory.
func (s *Service) SessionMemoryEntries() []session.MemoryEntry {
	entries := s.Entries()
	result := make([]session.MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		var expiresAt *time.Time
		if entry.TTL > 0 {
			expiry := entry.CreatedAt.Add(entry.TTL)
			expiresAt = &expiry
		}
		result = append(result, session.MemoryEntry{Key: entry.Key, Value: entry.Value, ExpiresAt: expiresAt})
	}
	return result
}
