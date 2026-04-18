package substrate

import (
	"context"
	"fmt"

	"github.com/mwigge/milliways/internal/conversation"
)

// Writer is the orchestrator-facing interface for the substrate write path.
// All operations are ordered: Begin → StartSegment → AppendTurn(s) →
// EndSegment → CheckpointOnExhaustion / Finish.
// Implementations mirror in-memory canonical state; errors are returned but
// do not interrupt the caller's own state management.
type Writer interface {
	Begin(ctx context.Context, convID, blockID, provider, prompt string) error
	StartSegment(ctx context.Context, provider string) error
	AppendTurn(ctx context.Context, role conversation.TurnRole, provider, text string) error
	EndSegment(ctx context.Context, status, reason string) error
	CheckpointOnExhaustion(ctx context.Context, reason string) (CheckpointResponse, error)
	Finish(ctx context.Context, status, reason string) error
}

// SessionWriter orchestrates the substrate write path for one orchestrator
// dispatch. It sequences MCP calls in the canonical order and is not safe for
// concurrent use from multiple goroutines.
type SessionWriter struct {
	client    *Client
	convID    string
	segmentID string
}

// NewSessionWriter returns a SessionWriter backed by client.
func NewSessionWriter(client *Client) *SessionWriter {
	return &SessionWriter{client: client}
}

// Begin starts a conversation on substrate and appends the initial user turn.
// It must be called before any other method.
func (w *SessionWriter) Begin(ctx context.Context, convID, blockID, provider, prompt string) error {
	w.convID = convID
	if _, err := w.client.ConversationStart(ctx, StartRequest{
		ConversationID: convID,
		BlockID:        blockID,
		Prompt:         prompt,
	}); err != nil {
		return fmt.Errorf("substrate writer Begin: %w", err)
	}
	return w.client.ConversationAppendTurn(ctx, AppendTurnRequest{
		ConversationID: convID,
		Turn: conversation.Turn{
			Role:     conversation.RoleUser,
			Provider: provider,
			Text:     prompt,
		},
	})
}

// StartSegment opens a new provider segment on substrate. The returned segment
// ID is retained and used by subsequent EndSegment / CheckpointOnExhaustion calls.
func (w *SessionWriter) StartSegment(ctx context.Context, provider string) error {
	resp, err := w.client.ConversationStartSegment(ctx, StartSegmentRequest{
		ConversationID: w.convID,
		Provider:       provider,
	})
	if err != nil {
		return fmt.Errorf("substrate writer StartSegment: %w", err)
	}
	w.segmentID = resp.SegmentID
	return nil
}

// AppendTurn mirrors one transcript turn to substrate.
func (w *SessionWriter) AppendTurn(ctx context.Context, role conversation.TurnRole, provider, text string) error {
	return w.client.ConversationAppendTurn(ctx, AppendTurnRequest{
		ConversationID: w.convID,
		Turn: conversation.Turn{
			Role:     role,
			Provider: provider,
			Text:     text,
		},
	})
}

// EndSegment closes the active provider segment with the given status
// ("done" | "failed" | "exhausted") and reason.
func (w *SessionWriter) EndSegment(ctx context.Context, status, reason string) error {
	if err := w.client.ConversationEndSegment(ctx, EndSegmentRequest{
		ConversationID: w.convID,
		SegmentID:      w.segmentID,
		Status:         status,
		Reason:         reason,
	}); err != nil {
		return fmt.Errorf("substrate writer EndSegment: %w", err)
	}
	w.segmentID = ""
	return nil
}

// CheckpointOnExhaustion closes the active segment as "exhausted" and writes a
// durable checkpoint. This is the combined write for context-limit failover;
// it must be called in place of EndSegment when exhaustion is detected.
func (w *SessionWriter) CheckpointOnExhaustion(ctx context.Context, reason string) (CheckpointResponse, error) {
	if err := w.EndSegment(ctx, "exhausted", reason); err != nil {
		return CheckpointResponse{}, fmt.Errorf("substrate writer CheckpointOnExhaustion end segment: %w", err)
	}
	resp, err := w.client.ConversationCheckpoint(ctx, CheckpointRequest{
		ConversationID: w.convID,
		Reason:         reason,
	})
	if err != nil {
		return CheckpointResponse{}, fmt.Errorf("substrate writer CheckpointOnExhaustion checkpoint: %w", err)
	}
	return resp, nil
}

// Finish ends the conversation on substrate with the given status ("done" | "failed").
func (w *SessionWriter) Finish(ctx context.Context, status, reason string) error {
	return w.client.ConversationEnd(ctx, EndRequest{
		ConversationID: w.convID,
		Status:         status,
		Reason:         reason,
	})
}
