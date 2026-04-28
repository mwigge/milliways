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

package pantry

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
)

// MemoryItemRecord is a durable promoted memory item owned by Milliways.
type MemoryItemRecord struct {
	ID             int64
	ConversationID string
	MemoryType     string
	SourceKind     string
	Scope          string
	Text           string
	Confidence     float64
	Status         string
	ValidUntil     string
	CreatedAt      string
	InvalidatedAt  string
}

// MemoryItemStore provides access to mw_memory_items.
type MemoryItemStore struct {
	db *sql.DB
}

// Insert writes a durable memory item.
func (s *MemoryItemStore) Insert(candidate conversation.MemoryCandidate, conversationID string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	validUntil := ""
	if candidate.FreshUntil != nil {
		validUntil = candidate.FreshUntil.UTC().Format(time.RFC3339)
	}
	result, err := s.db.Exec(
		`INSERT INTO mw_memory_items (conversation_id, memory_type, source_kind, scope, text, confidence, status, valid_until, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
		conversationID, string(candidate.MemoryType), candidate.SourceKind, candidate.Scope, candidate.Text, candidate.Confidence, validUntil, now,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting memory item: %w", err)
	}
	return result.LastInsertId()
}

// ListActiveByType returns active memory texts for a given type and optional scope.
func (s *MemoryItemStore) ListActiveByType(memoryType conversation.MemoryType, scope string) ([]string, error) {
	query := `SELECT text FROM mw_memory_items WHERE memory_type = ? AND status = 'active'`
	args := []any{string(memoryType)}
	if scope != "" {
		query += ` AND scope = ?`
		args = append(args, scope)
	}
	query += ` ORDER BY id ASC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying memory items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return nil, fmt.Errorf("scanning memory item: %w", err)
		}
		out = append(out, text)
	}
	return out, rows.Err()
}

// InvalidateExpired marks expired active memory items as invalidated.
func (s *MemoryItemStore) InvalidateExpired(now time.Time) (int64, error) {
	result, err := s.db.Exec(
		`UPDATE mw_memory_items
		 SET status = 'invalidated', invalidated_at = ?
		 WHERE status = 'active' AND valid_until != '' AND valid_until < ?`,
		now.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("invalidating expired memory items: %w", err)
	}
	return result.RowsAffected()
}
