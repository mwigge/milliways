package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mwigge/milliways/internal/substrate"
)

const legacyConversationMigrationName = "legacy_conversation_to_substrate_v1"

type checkpointMigrator interface {
	ConversationCheckpoint(ctx context.Context, req substrate.CheckpointRequest) (substrate.CheckpointResponse, error)
}

type eventAppender interface {
	ConversationEventsAppend(ctx context.Context, ev substrate.Event) error
}

type substrateMigrator interface {
	checkpointMigrator
	eventAppender
}

type legacyCheckpointRow struct {
	ConversationID string
	Reason         string
}

type legacyRuntimeEventRow struct {
	ConversationID string
	Kind           string
	Payload        string
}

type legacyEventPayload struct {
	BlockID   string            `json:"block_id,omitempty"`
	SegmentID string            `json:"segment_id,omitempty"`
	Provider  string            `json:"provider,omitempty"`
	Text      string            `json:"text,omitempty"`
	At        string            `json:"at,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// MigrateOnce copies legacy pantry conversation data into substrate exactly once.
// When the sentinel row already exists, the migration is a no-op.
func MigrateOnce(ctx context.Context, pantryDB *sql.DB, substrateClient *substrate.Client) error {
	if pantryDB == nil {
		return errors.New("legacy migration: pantry db is nil")
	}
	if substrateClient == nil {
		return errors.New("legacy migration: substrate client is nil")
	}
	return migrateOnce(ctx, pantryDB, substrateClient)
}

func migrateOnce(ctx context.Context, pantryDB *sql.DB, substrateClient substrateMigrator) error {
	tx, err := pantryDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("legacy migration: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS mw_migration_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			completed_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`); err != nil {
		return fmt.Errorf("legacy migration: ensure migration log: %w", err)
	}

	completed, err := migrationAlreadyCompleted(ctx, tx)
	if err != nil {
		return err
	}
	if completed {
		return tx.Commit()
	}

	checkpoints, err := loadLegacyCheckpoints(ctx, tx)
	if err != nil {
		return err
	}
	events, err := loadLegacyRuntimeEvents(ctx, tx)
	if err != nil {
		return err
	}

	for _, ckpt := range checkpoints {
		if _, err := substrateClient.ConversationCheckpoint(ctx, substrate.CheckpointRequest{
			ConversationID: ckpt.ConversationID,
			Reason:         ckpt.Reason,
		}); err != nil {
			return fmt.Errorf("legacy migration: migrate checkpoint for %s: %w", ckpt.ConversationID, err)
		}
	}

	for _, evt := range events {
		if err := substrateClient.ConversationEventsAppend(ctx, substrate.Event{
			ConversationID: evt.ConversationID,
			Kind:           evt.Kind,
			Payload:        evt.Payload,
		}); err != nil {
			return fmt.Errorf("legacy migration: migrate runtime event for %s/%s: %w", evt.ConversationID, evt.Kind, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO mw_migration_log (name) VALUES (?)`, legacyConversationMigrationName); err != nil {
		return fmt.Errorf("legacy migration: write sentinel: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("legacy migration: commit: %w", err)
	}
	return nil
}

func migrationAlreadyCompleted(ctx context.Context, tx *sql.Tx) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM mw_migration_log WHERE name = ?`, legacyConversationMigrationName).Scan(&count); err != nil {
		return false, fmt.Errorf("legacy migration: check sentinel: %w", err)
	}
	return count > 0, nil
}

func loadLegacyCheckpoints(ctx context.Context, tx *sql.Tx) ([]legacyCheckpointRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT conversation_id, reason
		FROM mw_checkpoints
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("legacy migration: query checkpoints: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var checkpoints []legacyCheckpointRow
	for rows.Next() {
		var row legacyCheckpointRow
		if err := rows.Scan(&row.ConversationID, &row.Reason); err != nil {
			return nil, fmt.Errorf("legacy migration: scan checkpoint: %w", err)
		}
		checkpoints = append(checkpoints, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("legacy migration: iterate checkpoints: %w", err)
	}
	return checkpoints, nil
}

func loadLegacyRuntimeEvents(ctx context.Context, tx *sql.Tx) ([]legacyRuntimeEventRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT conversation_id, kind, block_id, segment_id, provider, text, at, fields_json
		FROM mw_runtime_events
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("legacy migration: query runtime events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []legacyRuntimeEventRow
	for rows.Next() {
		var (
			row        legacyRuntimeEventRow
			payload    legacyEventPayload
			fieldsJSON string
		)
		if err := rows.Scan(&row.ConversationID, &row.Kind, &payload.BlockID, &payload.SegmentID, &payload.Provider, &payload.Text, &payload.At, &fieldsJSON); err != nil {
			return nil, fmt.Errorf("legacy migration: scan runtime event: %w", err)
		}
		if fieldsJSON != "" && fieldsJSON != "{}" {
			if err := json.Unmarshal([]byte(fieldsJSON), &payload.Fields); err != nil {
				return nil, fmt.Errorf("legacy migration: decode runtime event fields: %w", err)
			}
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("legacy migration: encode runtime event payload: %w", err)
		}
		row.Payload = string(payloadJSON)
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("legacy migration: iterate runtime events: %w", err)
	}
	return events, nil
}
