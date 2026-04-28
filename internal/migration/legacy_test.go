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

package migration

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mwigge/milliways/internal/substrate"
)

type fakeSubstrate struct {
	checkpointCalls []substrate.CheckpointRequest
	eventCalls      []substrate.Event
	checkpointErr   error
	eventErr        error
	eventErrAfter   int
	checkpointResp  substrate.CheckpointResponse
}

func (f *fakeSubstrate) ConversationCheckpoint(_ context.Context, req substrate.CheckpointRequest) (substrate.CheckpointResponse, error) {
	if f.checkpointErr != nil {
		return substrate.CheckpointResponse{}, f.checkpointErr
	}
	f.checkpointCalls = append(f.checkpointCalls, req)
	resp := f.checkpointResp
	if resp.CheckpointID == "" {
		resp.CheckpointID = "ckpt-migrated"
		resp.TakenAt = time.Unix(0, 0).UTC()
	}
	return resp, nil
}

func (f *fakeSubstrate) ConversationEventsAppend(_ context.Context, ev substrate.Event) error {
	if f.eventErr != nil && len(f.eventCalls) >= f.eventErrAfter {
		return f.eventErr
	}
	f.eventCalls = append(f.eventCalls, ev)
	return nil
}

func TestMigrateOnceCopiesAllCheckpointsAndEvents(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	seedLegacyData(t, db)

	fake := &fakeSubstrate{}
	if err := migrateOnce(context.Background(), db, fake); err != nil {
		t.Fatalf("migrateOnce() error = %v", err)
	}

	if got, want := len(fake.checkpointCalls), 2; got != want {
		t.Fatalf("checkpoint calls = %d, want %d", got, want)
	}
	if got, want := len(fake.eventCalls), 3; got != want {
		t.Fatalf("event calls = %d, want %d", got, want)
	}
	if fake.eventCalls[0].Kind != "segment_start" {
		t.Fatalf("first event kind = %q, want %q", fake.eventCalls[0].Kind, "segment_start")
	}
	if got := migrationLogCount(t, db); got != 1 {
		t.Fatalf("migration log count = %d, want 1", got)
	}
	if fake.eventCalls[0].Payload == "" {
		t.Fatal("expected migrated event payload to be populated")
	}
}

func TestMigrateOnceSecondRunIsNoOp(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	seedLegacyData(t, db)

	fake := &fakeSubstrate{}
	if err := migrateOnce(context.Background(), db, fake); err != nil {
		t.Fatalf("first migrateOnce() error = %v", err)
	}
	if err := migrateOnce(context.Background(), db, fake); err != nil {
		t.Fatalf("second migrateOnce() error = %v", err)
	}

	if got, want := len(fake.checkpointCalls), 2; got != want {
		t.Fatalf("checkpoint calls after second run = %d, want %d", got, want)
	}
	if got, want := len(fake.eventCalls), 3; got != want {
		t.Fatalf("event calls after second run = %d, want %d", got, want)
	}
	if got := migrationLogCount(t, db); got != 1 {
		t.Fatalf("migration log count = %d, want 1", got)
	}
	tx := mustBeginTx(t, db)
	defer func() { _ = tx.Rollback() }()
	if migrated, err := migrationAlreadyCompleted(context.Background(), tx); err != nil || !migrated {
		t.Fatalf("migrationAlreadyCompleted() = %v, %v, want true, nil", migrated, err)
	}
}

func TestMigrateOnceReturnsErrorWithoutSentinelOnSubstrateFailure(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	seedLegacyData(t, db)

	wantErr := errors.New("substrate append failed")
	fake := &fakeSubstrate{eventErr: wantErr, eventErrAfter: 0}
	err := migrateOnce(context.Background(), db, fake)
	if !errors.Is(err, wantErr) {
		t.Fatalf("migrateOnce() error = %v, want %v", err, wantErr)
	}
	if got := migrationLogCount(t, db); got != 0 {
		t.Fatalf("migration log count = %d, want 0", got)
	}
	if got, want := len(fake.checkpointCalls), 2; got != want {
		t.Fatalf("checkpoint calls before failure = %d, want %d", got, want)
	}
	if got := len(fake.eventCalls); got != 0 {
		t.Fatalf("event calls before failure = %d, want 0", got)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	createLegacyTables(t, db)
	return db
}

func createLegacyTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE mw_checkpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE mw_runtime_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			block_id TEXT NOT NULL DEFAULT '',
			segment_id TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL,
			provider TEXT NOT NULL DEFAULT '',
			text TEXT NOT NULL DEFAULT '',
			at TEXT NOT NULL,
			fields_json TEXT NOT NULL DEFAULT '{}'
		);
	`)
	if err != nil {
		t.Fatalf("creating legacy tables: %v", err)
	}
}

func seedLegacyData(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO mw_checkpoints (conversation_id, reason) VALUES
			('conv-1', 'provider exhausted'),
			('conv-2', 'manual checkpoint');
		INSERT INTO mw_runtime_events (conversation_id, block_id, segment_id, kind, provider, text, at, fields_json) VALUES
			('conv-1', 'block-1', 'seg-1', 'segment_start', 'claude', 'started', '2026-04-18T10:00:00Z', '{"status":"active"}'),
			('conv-1', 'block-1', 'seg-1', 'provider_output', 'claude', 'hello', '2026-04-18T10:00:01Z', '{"event_type":"text"}'),
			('conv-2', 'block-2', 'seg-2', 'segment_end', 'gemini', 'done', '2026-04-18T10:01:00Z', '{"status":"done"}');
	`)
	if err != nil {
		t.Fatalf("seeding legacy data: %v", err)
	}
}

func migrationLogCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(1) FROM mw_migration_log WHERE name = ?`, legacyConversationMigrationName).Scan(&count); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0
		}
		if err.Error() == "no such table: mw_migration_log" {
			return 0
		}
		t.Fatalf("query migration log count: %v", err)
	}
	return count
}

func mustBeginTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	return tx
}
