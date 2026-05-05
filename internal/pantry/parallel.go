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
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ParallelStatus is the lifecycle state of a group or slot.
type ParallelStatus string

const (
	ParallelStatusRunning     ParallelStatus = "running"
	ParallelStatusDone        ParallelStatus = "done"
	ParallelStatusError       ParallelStatus = "error"
	ParallelStatusInterrupted ParallelStatus = "interrupted"
)

// ParallelGroupRecord is one persisted parallel-dispatch group.
type ParallelGroupRecord struct {
	ID          string
	Prompt      string
	Status      ParallelStatus
	CreatedAt   time.Time
	CompletedAt time.Time
	Slots       []ParallelSlotRecord
}

// ParallelSlotRecord is one provider slot within a group.
type ParallelSlotRecord struct {
	ID          int64
	GroupID     string
	Handle      int64
	Provider    string
	Status      ParallelStatus
	StartedAt   time.Time
	CompletedAt time.Time
	TokensIn    int
	TokensOut   int
}

// ParallelStore provides access to mw_parallel_groups and mw_parallel_slots.
type ParallelStore struct {
	db *sql.DB
}

const timeLayout = "2006-01-02T15:04:05Z"

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		slog.Debug("parallel: parseTime: unrecognised timestamp", "raw", s, "err", err)
	}
	return t
}

// InsertGroup writes a new parallel group record.
func (s *ParallelStore) InsertGroup(g ParallelGroupRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO mw_parallel_groups (id, prompt, status, created_at, completed_at)
		 VALUES (?, ?, ?, ?, ?)`,
		g.ID, g.Prompt, string(g.Status), formatTime(g.CreatedAt), formatTime(g.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("insert parallel group: %w", err)
	}
	return nil
}

// InsertSlot writes a new slot record for an existing group.
func (s *ParallelStore) InsertSlot(slot ParallelSlotRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO mw_parallel_slots (group_id, handle, provider, status, started_at, completed_at, tokens_in, tokens_out)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		slot.GroupID, slot.Handle, slot.Provider, string(slot.Status),
		formatTime(slot.StartedAt), formatTime(slot.CompletedAt),
		slot.TokensIn, slot.TokensOut,
	)
	if err != nil {
		return fmt.Errorf("insert parallel slot: %w", err)
	}
	return nil
}

// UpdateSlotStatus updates a slot's status and token counts by handle.
func (s *ParallelStore) UpdateSlotStatus(handle int64, status ParallelStatus, tokensIn, tokensOut int) error {
	completedAt := ""
	if status == ParallelStatusDone || status == ParallelStatusError || status == ParallelStatusInterrupted {
		completedAt = formatTime(time.Now().UTC())
	}
	res, err := s.db.Exec(
		`UPDATE mw_parallel_slots
		 SET status = ?, completed_at = ?, tokens_in = ?, tokens_out = ?
		 WHERE handle = ?`,
		string(status), completedAt, tokensIn, tokensOut, handle,
	)
	if err != nil {
		return fmt.Errorf("update slot status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("update slot status: handle %d not found", handle)
	}
	return nil
}

// GetGroup returns a group and all its slots by group ID.
func (s *ParallelStore) GetGroup(id string) (ParallelGroupRecord, error) {
	var g ParallelGroupRecord
	var createdAt, completedAt string
	err := s.db.QueryRow(
		`SELECT id, prompt, status, created_at, completed_at FROM mw_parallel_groups WHERE id = ?`, id,
	).Scan(&g.ID, &g.Prompt, (*string)(&g.Status), &createdAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ParallelGroupRecord{}, fmt.Errorf("group %q not found", id)
	}
	if err != nil {
		return ParallelGroupRecord{}, fmt.Errorf("query parallel group: %w", err)
	}
	g.CreatedAt = parseTime(createdAt)
	g.CompletedAt = parseTime(completedAt)

	rows, err := s.db.Query(
		`SELECT id, group_id, handle, provider, status, started_at, completed_at, tokens_in, tokens_out
		 FROM mw_parallel_slots WHERE group_id = ? ORDER BY id ASC`, id,
	)
	if err != nil {
		return g, fmt.Errorf("query parallel slots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var sl ParallelSlotRecord
		var startedAt, slCompletedAt string
		if err := rows.Scan(&sl.ID, &sl.GroupID, &sl.Handle, &sl.Provider,
			(*string)(&sl.Status), &startedAt, &slCompletedAt, &sl.TokensIn, &sl.TokensOut); err != nil {
			return g, fmt.Errorf("scan slot: %w", err)
		}
		sl.StartedAt = parseTime(startedAt)
		sl.CompletedAt = parseTime(slCompletedAt)
		g.Slots = append(g.Slots, sl)
	}
	return g, rows.Err()
}

// ListGroups returns up to n most-recent groups (summary only, no slots).
func (s *ParallelStore) ListGroups(n int) ([]ParallelGroupRecord, error) {
	if n <= 0 {
		n = 20
	}
	rows, err := s.db.Query(
		`SELECT id, prompt, status, created_at, completed_at
		 FROM mw_parallel_groups
		 ORDER BY created_at DESC LIMIT ?`, n,
	)
	if err != nil {
		return nil, fmt.Errorf("list parallel groups: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var groups []ParallelGroupRecord
	for rows.Next() {
		var g ParallelGroupRecord
		var createdAt, completedAt string
		if err := rows.Scan(&g.ID, &g.Prompt, (*string)(&g.Status), &createdAt, &completedAt); err != nil {
			return nil, fmt.Errorf("scan group: %w", err)
		}
		g.CreatedAt = parseTime(createdAt)
		g.CompletedAt = parseTime(completedAt)
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// MarkGroupSlotsInterrupted sets all running slots for a specific group to
// interrupted. Used when a dispatch fails mid-way to avoid leaving a partially
// created group stuck in running state without touching other groups.
func (s *ParallelStore) MarkGroupSlotsInterrupted(groupID string) error {
	now := formatTime(time.Now().UTC())
	_, err := s.db.Exec(
		`UPDATE mw_parallel_slots SET status = ?, completed_at = ?
		 WHERE group_id = ? AND status = ?`,
		string(ParallelStatusInterrupted), now, groupID, string(ParallelStatusRunning),
	)
	if err != nil {
		return fmt.Errorf("mark group slots interrupted: %w", err)
	}
	return nil
}

// MarkInterruptedSlots sets all running slots to interrupted status. Called on
// daemon restart to clean up slots that were in-flight when the daemon stopped.
func (s *ParallelStore) MarkInterruptedSlots() error {
	now := formatTime(time.Now().UTC())
	_, err := s.db.Exec(
		`UPDATE mw_parallel_slots SET status = ?, completed_at = ? WHERE status = ?`,
		string(ParallelStatusInterrupted), now, string(ParallelStatusRunning),
	)
	if err != nil {
		return fmt.Errorf("mark interrupted slots: %w", err)
	}
	return nil
}
