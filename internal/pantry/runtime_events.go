package pantry

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/observability"
)

// RuntimeEventRecord is the persisted form of a runtime event.
type RuntimeEventRecord struct {
	ID             int64
	ConversationID string
	BlockID        string
	SegmentID      string
	Kind           string
	Provider       string
	Text           string
	At             string
	Fields         map[string]string
}

// RuntimeEventStore provides access to mw_runtime_events.
type RuntimeEventStore struct {
	db *sql.DB
}

// Insert writes a runtime event record.
func (s *RuntimeEventStore) Insert(e RuntimeEventRecord) (int64, error) {
	at := e.At
	if at == "" {
		at = time.Now().UTC().Format(time.RFC3339)
	}
	fieldsJSON := "{}"
	if e.Fields != nil {
		data, err := json.Marshal(e.Fields)
		if err != nil {
			return 0, fmt.Errorf("marshalling runtime event fields: %w", err)
		}
		fieldsJSON = string(data)
	}
	result, err := s.db.Exec(
		`INSERT INTO mw_runtime_events (conversation_id, block_id, segment_id, kind, provider, text, at, fields_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ConversationID, e.BlockID, e.SegmentID, e.Kind, e.Provider, e.Text, at, fieldsJSON,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting runtime event: %w", err)
	}
	return result.LastInsertId()
}

// ListByConversation returns runtime events ordered by insertion for a conversation.
func (s *RuntimeEventStore) ListByConversation(conversationID string) ([]RuntimeEventRecord, error) {
	return s.listByConversationWhere(conversationID, "", "")
}

// ListByConversationSince returns runtime events after the supplied timestamp.
func (s *RuntimeEventStore) ListByConversationSince(conversationID, at string) ([]RuntimeEventRecord, error) {
	return s.listByConversationWhere(conversationID, "AND at > ?", at)
}

func (s *RuntimeEventStore) listByConversationWhere(conversationID, extraWhere, extraArg string) ([]RuntimeEventRecord, error) {
	query := `
		SELECT id, conversation_id, block_id, segment_id, kind, provider, text, at, fields_json
		FROM mw_runtime_events
		WHERE conversation_id = ? ` + extraWhere + `
		ORDER BY id ASC
	`
	args := []any{conversationID}
	if extraArg != "" {
		args = append(args, extraArg)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying runtime events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RuntimeEventRecord
	for rows.Next() {
		var rec RuntimeEventRecord
		var fieldsJSON string
		if err := rows.Scan(&rec.ID, &rec.ConversationID, &rec.BlockID, &rec.SegmentID, &rec.Kind, &rec.Provider, &rec.Text, &rec.At, &fieldsJSON); err != nil {
			return nil, fmt.Errorf("scanning runtime event: %w", err)
		}
		if fieldsJSON != "" && fieldsJSON != "{}" {
			_ = json.Unmarshal([]byte(fieldsJSON), &rec.Fields)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ReconstructConversation rebuilds a canonical conversation and matching runtime
// event stream from persisted observability records.
func (s *RuntimeEventStore) ReconstructConversation(conversationID, blockID, prompt string, exitCode int) (*conversation.Conversation, []observability.Event, error) {
	records, err := s.ListByConversation(conversationID)
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, sql.ErrNoRows
	}
	if blockID == "" {
		blockID = records[0].BlockID
	}

	conv := conversation.New(conversationID, blockID, prompt)
	runtimeEvents := make([]observability.Event, 0, len(records))
	applyRuntimeRecords(conv, records, &runtimeEvents)

	finalizeReplayedConversation(conv, exitCode)
	return conv, runtimeEvents, nil
}

// ReconstructConversationFromCheckpoint rebuilds a conversation by starting from
// a durable checkpoint and replaying only subsequent runtime events.
func (s *RuntimeEventStore) ReconstructConversationFromCheckpoint(ckpt *conversation.ConversationCheckpoint, exitCode int) (*conversation.Conversation, []observability.Event, error) {
	if ckpt == nil {
		return nil, nil, sql.ErrNoRows
	}
	records, err := s.ListByConversationSince(ckpt.ConversationID, ckpt.TakenAt.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, nil, err
	}
	conv := &conversation.Conversation{
		ID:          ckpt.ConversationID,
		BlockID:     ckpt.BlockID,
		Prompt:      firstUserPrompt(ckpt.Transcript),
		Status:      ckpt.Status,
		Transcript:  append([]conversation.Turn(nil), ckpt.Transcript...),
		Memory:      ckpt.WorkingMemory,
		Context:     ckpt.Context,
		Segments:    append([]conversation.ProviderSegment(nil), ckpt.Segments...),
		CreatedAt:   ckpt.TakenAt,
		UpdatedAt:   ckpt.TakenAt,
		Checkpoints: []conversation.ConversationCheckpoint{*ckpt},
	}
	for i := range conv.Segments {
		if conv.Segments[i].Status == conversation.SegmentActive {
			conv.ActiveSegmentID = conv.Segments[i].ID
		}
	}
	runtimeEvents := make([]observability.Event, 0, len(records))
	applyRuntimeRecords(conv, records, &runtimeEvents)
	finalizeReplayedConversation(conv, exitCode)
	return conv, runtimeEvents, nil
}

func applyRuntimeRecords(conv *conversation.Conversation, records []RuntimeEventRecord, runtimeEvents *[]observability.Event) {
	for _, rec := range records {
		at, parseErr := time.Parse(time.RFC3339, rec.At)
		if parseErr != nil {
			at = time.Now()
		}
		evt := observability.Event{
			ConversationID: rec.ConversationID,
			BlockID:        rec.BlockID,
			SegmentID:      rec.SegmentID,
			Kind:           rec.Kind,
			Provider:       rec.Provider,
			Text:           rec.Text,
			At:             at,
			Fields:         rec.Fields,
		}
		*runtimeEvents = append(*runtimeEvents, evt)

		switch rec.Kind {
		case "segment_start":
			seg := conv.StartSegment(rec.Provider, nil)
			conv.Segments[len(conv.Segments)-1].StartedAt = at
			if rec.SegmentID != "" {
				conv.Segments[len(conv.Segments)-1].ID = rec.SegmentID
				conv.ActiveSegmentID = rec.SegmentID
			} else {
				conv.ActiveSegmentID = seg.ID
			}
		case "provider_output":
			eventType := rec.Fields["event_type"]
			switch eventType {
			case "code_block":
				if code := rec.Fields["code"]; code != "" {
					conv.AppendTurn(conversation.RoleAssistant, rec.Provider, code)
				}
			case "tool_use":
				text := rec.Text
				if text == "" {
					text = fmt.Sprintf("[tool:%s] %s", rec.Fields["tool_name"], rec.Fields["tool_status"])
				}
				conv.AppendTurn(conversation.RoleSystem, rec.Provider, text)
			case "question", "confirm", "error":
				if rec.Text != "" {
					conv.AppendTurn(conversation.RoleSystem, rec.Provider, rec.Text)
				}
			default:
				if rec.Text == "" {
					continue
				}
				role := conversation.RoleAssistant
				if rec.Provider == "milliways" {
					role = conversation.RoleSystem
				}
				conv.AppendTurn(role, rec.Provider, rec.Text)
			}
		case "segment_end":
			status := conversation.SegmentDone
			switch rec.Fields["status"] {
			case string(conversation.SegmentExhausted):
				status = conversation.SegmentExhausted
			case string(conversation.SegmentFailed):
				status = conversation.SegmentFailed
			}
			conv.EndActiveSegment(status, rec.Fields["reason"])
			if len(conv.Segments) > 0 {
				last := &conv.Segments[len(conv.Segments)-1]
				last.EndedAt = &at
				last.EndReason = rec.Fields["reason"]
				last.Status = status
			}
		case "failover":
			if conv.ActiveSegment() != nil {
				conv.EndActiveSegment(conversation.SegmentExhausted, rec.Text)
				if len(conv.Segments) > 0 {
					last := &conv.Segments[len(conv.Segments)-1]
					last.EndedAt = &at
					last.EndReason = rec.Text
					last.Status = conversation.SegmentExhausted
				}
			}
		}
	}

	sort.SliceStable(*runtimeEvents, func(i, j int) bool {
		return (*runtimeEvents)[i].At.Before((*runtimeEvents)[j].At)
	})
}

func finalizeReplayedConversation(conv *conversation.Conversation, exitCode int) {
	if conv.ActiveSegment() != nil {
		status := conversation.SegmentDone
		if exitCode != 0 {
			status = conversation.SegmentFailed
			conv.Status = conversation.StatusFailed
		} else {
			conv.Status = conversation.StatusDone
		}
		conv.EndActiveSegment(status, "replayed terminal state")
		return
	}
	if exitCode != 0 {
		conv.Status = conversation.StatusFailed
	} else {
		conv.Status = conversation.StatusDone
	}
}

func firstUserPrompt(turns []conversation.Turn) string {
	for _, turn := range turns {
		if turn.Role == conversation.RoleUser && turn.Text != "" {
			return turn.Text
		}
	}
	return ""
}
