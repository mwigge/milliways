package pantry

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mwigge/milliways/internal/conversation"
)

// CheckpointStore provides access to mw_checkpoints.
type CheckpointStore struct {
	db *sql.DB
}

// CheckpointRecord is the persisted form of a conversation checkpoint.
type CheckpointRecord struct {
	ID             int64
	ConversationID string
	CheckpointID   string
	BlockID        string
	SegmentID      string
	Provider       string
	Reason         string
	TakenAt        string
	SnapshotJSON   string
}

// Insert writes a checkpoint snapshot to the database.
func (s *CheckpointStore) Insert(ckpt conversation.ConversationCheckpoint) (int64, error) {
	data, err := json.Marshal(ckpt)
	if err != nil {
		return 0, fmt.Errorf("marshalling checkpoint: %w", err)
	}
	result, err := s.db.Exec(
		`INSERT INTO mw_checkpoints (conversation_id, checkpoint_id, block_id, segment_id, provider, reason, taken_at, snapshot_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ckpt.ConversationID, ckpt.ID, ckpt.BlockID, ckpt.SegmentID, ckpt.SegmentProvider, ckpt.Reason, ckpt.TakenAt.UTC().Format("2006-01-02T15:04:05Z07:00"), string(data),
	)
	if err != nil {
		return 0, fmt.Errorf("inserting checkpoint: %w", err)
	}
	return result.LastInsertId()
}

// LatestByConversation returns the newest checkpoint snapshot for a conversation.
func (s *CheckpointStore) LatestByConversation(conversationID string) (*conversation.ConversationCheckpoint, error) {
	var snapshotJSON string
	err := s.db.QueryRow(`
		SELECT snapshot_json
		FROM mw_checkpoints
		WHERE conversation_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, conversationID).Scan(&snapshotJSON)
	if err != nil {
		return nil, err
	}
	var ckpt conversation.ConversationCheckpoint
	if err := json.Unmarshal([]byte(snapshotJSON), &ckpt); err != nil {
		return nil, fmt.Errorf("unmarshalling checkpoint: %w", err)
	}
	return &ckpt, nil
}
