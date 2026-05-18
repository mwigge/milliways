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
)

// CodingChangeSet is one durable coding-agent change set.
type CodingChangeSet struct {
	ID          int64
	Workspace   string
	SessionID   string
	Client      string
	TurnID      string
	ToolCallID  string
	Operation   string
	Status      string
	Reason      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt time.Time
}

// CodingFileChange is one file-level change attached to a change set.
type CodingFileChange struct {
	ID          int64
	ChangeSetID int64
	Workspace   string
	SessionID   string
	Client      string
	TurnID      string
	ToolCallID  string
	Operation   string
	Path        string
	BeforeHash  string
	AfterHash   string
	Diff        string
	Preview     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ToolApproval is one durable approval/audit record for a tool operation.
type ToolApproval struct {
	ID         int64
	Workspace  string
	SessionID  string
	Client     string
	TurnID     string
	ToolCallID string
	ToolName   string
	Operation  string
	Path       string
	BeforeHash string
	AfterHash  string
	Diff       string
	Preview    string
	Decision   string
	Reason     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// CodingStore provides access to coding change tracking and approval records.
type CodingStore struct {
	db *sql.DB
}

const codingTimeLayout = "2006-01-02T15:04:05Z"

func codingFormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(codingTimeLayout)
}

func codingParseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(codingTimeLayout, s)
	return t
}

// InsertChangeSet records a new coding-agent change set and returns its ID.
func (s *CodingStore) InsertChangeSet(cs CodingChangeSet) (int64, error) {
	if cs.Status == "" {
		cs.Status = "open"
	}
	if cs.CreatedAt.IsZero() {
		cs.CreatedAt = time.Now().UTC()
	}
	if cs.UpdatedAt.IsZero() {
		cs.UpdatedAt = cs.CreatedAt
	}
	res, err := s.db.Exec(`
		INSERT INTO mw_coding_change_sets
			(workspace, session_id, client, turn_id, tool_call_id, operation,
			 status, reason, created_at, updated_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cs.Workspace, cs.SessionID, cs.Client, cs.TurnID, cs.ToolCallID,
		cs.Operation, cs.Status, cs.Reason, codingFormatTime(cs.CreatedAt),
		codingFormatTime(cs.UpdatedAt), codingFormatTime(cs.CompletedAt))
	if err != nil {
		return 0, fmt.Errorf("insert coding change set: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("coding change set id: %w", err)
	}
	return id, nil
}

// UpdateChangeSet updates mutable lifecycle fields for an existing change set.
func (s *CodingStore) UpdateChangeSet(cs CodingChangeSet) error {
	if cs.UpdatedAt.IsZero() {
		cs.UpdatedAt = time.Now().UTC()
	}
	res, err := s.db.Exec(`
		UPDATE mw_coding_change_sets
		SET status = ?, reason = ?, updated_at = ?, completed_at = ?
		WHERE id = ?`,
		cs.Status, cs.Reason, codingFormatTime(cs.UpdatedAt),
		codingFormatTime(cs.CompletedAt), cs.ID)
	if err != nil {
		return fmt.Errorf("update coding change set: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("update coding change set: id %d not found", cs.ID)
	}
	return nil
}

// ListChangeSets returns recent change sets, optionally scoped by workspace
// and session. Results are newest first.
func (s *CodingStore) ListChangeSets(workspace, sessionID string, limit int) ([]CodingChangeSet, error) {
	if limit <= 0 {
		limit = 20
	}
	query := `
		SELECT id, workspace, session_id, client, turn_id, tool_call_id, operation,
		       status, reason, created_at, updated_at, completed_at
		FROM mw_coding_change_sets
		WHERE (? = '' OR workspace = ?) AND (? = '' OR session_id = ?)
		ORDER BY created_at DESC, id DESC
		LIMIT ?`
	rows, err := s.db.Query(query, workspace, workspace, sessionID, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list coding change sets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sets []CodingChangeSet
	for rows.Next() {
		var cs CodingChangeSet
		var createdAt, updatedAt, completedAt string
		if err := rows.Scan(&cs.ID, &cs.Workspace, &cs.SessionID, &cs.Client,
			&cs.TurnID, &cs.ToolCallID, &cs.Operation, &cs.Status, &cs.Reason,
			&createdAt, &updatedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("scan coding change set: %w", err)
		}
		cs.CreatedAt = codingParseTime(createdAt)
		cs.UpdatedAt = codingParseTime(updatedAt)
		cs.CompletedAt = codingParseTime(completedAt)
		sets = append(sets, cs)
	}
	return sets, rows.Err()
}

// InsertFileChange records one file-level change for a change set.
func (s *CodingStore) InsertFileChange(fc CodingFileChange) (int64, error) {
	if fc.CreatedAt.IsZero() {
		fc.CreatedAt = time.Now().UTC()
	}
	if fc.UpdatedAt.IsZero() {
		fc.UpdatedAt = fc.CreatedAt
	}
	res, err := s.db.Exec(`
		INSERT INTO mw_coding_file_changes
			(change_set_id, workspace, session_id, client, turn_id, tool_call_id,
			 operation, path, before_hash, after_hash, diff, preview, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fc.ChangeSetID, fc.Workspace, fc.SessionID, fc.Client, fc.TurnID,
		fc.ToolCallID, fc.Operation, fc.Path, fc.BeforeHash, fc.AfterHash,
		fc.Diff, fc.Preview, codingFormatTime(fc.CreatedAt), codingFormatTime(fc.UpdatedAt))
	if err != nil {
		return 0, fmt.Errorf("insert coding file change: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("coding file change id: %w", err)
	}
	return id, nil
}

// ListFileChanges returns file changes for one change set in insertion order.
func (s *CodingStore) ListFileChanges(changeSetID int64) ([]CodingFileChange, error) {
	rows, err := s.db.Query(`
		SELECT id, change_set_id, workspace, session_id, client, turn_id, tool_call_id,
		       operation, path, before_hash, after_hash, diff, preview, created_at, updated_at
		FROM mw_coding_file_changes
		WHERE change_set_id = ?
		ORDER BY id ASC`, changeSetID)
	if err != nil {
		return nil, fmt.Errorf("list coding file changes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var changes []CodingFileChange
	for rows.Next() {
		var fc CodingFileChange
		var createdAt, updatedAt string
		if err := rows.Scan(&fc.ID, &fc.ChangeSetID, &fc.Workspace, &fc.SessionID,
			&fc.Client, &fc.TurnID, &fc.ToolCallID, &fc.Operation, &fc.Path,
			&fc.BeforeHash, &fc.AfterHash, &fc.Diff, &fc.Preview, &createdAt,
			&updatedAt); err != nil {
			return nil, fmt.Errorf("scan coding file change: %w", err)
		}
		fc.CreatedAt = codingParseTime(createdAt)
		fc.UpdatedAt = codingParseTime(updatedAt)
		changes = append(changes, fc)
	}
	return changes, rows.Err()
}

// RecordToolApproval records a tool approval/audit event and returns its ID.
func (s *CodingStore) RecordToolApproval(a ToolApproval) (int64, error) {
	if a.Decision == "" {
		a.Decision = "pending"
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = a.CreatedAt
	}
	res, err := s.db.Exec(`
		INSERT INTO mw_tool_approvals
			(workspace, session_id, client, turn_id, tool_call_id, tool_name,
			 operation, path, before_hash, after_hash, diff, preview, decision,
			 reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Workspace, a.SessionID, a.Client, a.TurnID, a.ToolCallID, a.ToolName,
		a.Operation, a.Path, a.BeforeHash, a.AfterHash, a.Diff, a.Preview,
		a.Decision, a.Reason, codingFormatTime(a.CreatedAt), codingFormatTime(a.UpdatedAt))
	if err != nil {
		return 0, fmt.Errorf("record tool approval: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("tool approval id: %w", err)
	}
	return id, nil
}

// UpdateToolApprovalDecision updates the user or policy decision for an
// existing approval record.
func (s *CodingStore) UpdateToolApprovalDecision(id int64, decision, reason string) error {
	now := codingFormatTime(time.Now().UTC())
	res, err := s.db.Exec(`
		UPDATE mw_tool_approvals
		SET decision = ?,
		    reason = ?,
		    updated_at = CASE WHEN created_at > ? THEN created_at ELSE ? END
		WHERE id = ?`,
		decision, reason, now, now, id)
	if err != nil {
		return fmt.Errorf("update tool approval decision: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("update tool approval decision: id %d not found", id)
	}
	return nil
}

// ListToolApprovals returns recent approval records, optionally scoped by
// workspace and session. Results are newest first.
func (s *CodingStore) ListToolApprovals(workspace, sessionID string, limit int) ([]ToolApproval, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`
		SELECT id, workspace, session_id, client, turn_id, tool_call_id, tool_name,
		       operation, path, before_hash, after_hash, diff, preview, decision,
		       reason, created_at, updated_at
		FROM mw_tool_approvals
		WHERE (? = '' OR workspace = ?) AND (? = '' OR session_id = ?)
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, workspace, workspace, sessionID, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list tool approvals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var approvals []ToolApproval
	for rows.Next() {
		var a ToolApproval
		var createdAt, updatedAt string
		if err := rows.Scan(&a.ID, &a.Workspace, &a.SessionID, &a.Client,
			&a.TurnID, &a.ToolCallID, &a.ToolName, &a.Operation, &a.Path,
			&a.BeforeHash, &a.AfterHash, &a.Diff, &a.Preview, &a.Decision,
			&a.Reason, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan tool approval: %w", err)
		}
		a.CreatedAt = codingParseTime(createdAt)
		a.UpdatedAt = codingParseTime(updatedAt)
		approvals = append(approvals, a)
	}
	return approvals, rows.Err()
}
