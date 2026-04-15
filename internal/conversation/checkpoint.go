package conversation

import "time"

// ConversationCheckpoint is a serializable continuity snapshot taken at a
// meaningful boundary such as failover, completion, or explicit save.
type ConversationCheckpoint struct {
	ID              string            `json:"id"`
	ConversationID  string            `json:"conversation_id"`
	BlockID         string            `json:"block_id"`
	Reason          string            `json:"reason"`
	SegmentID       string            `json:"segment_id,omitempty"`
	SegmentProvider string            `json:"segment_provider,omitempty"`
	Status          Status            `json:"status"`
	TranscriptTurns int               `json:"transcript_turns"`
	Transcript      []Turn            `json:"transcript,omitempty"`
	Segments        []ProviderSegment `json:"segments,omitempty"`
	WorkingMemory   MemoryState       `json:"working_memory"`
	Context         ContextBundle     `json:"context"`
	TakenAt         time.Time         `json:"taken_at"`
}

// Snapshot creates a checkpoint from the current canonical conversation state.
func (c *Conversation) Snapshot(reason string) ConversationCheckpoint {
	checkpoint := ConversationCheckpoint{
		ID:              c.ID + "-ckpt-" + time.Now().UTC().Format("20060102T150405.000000000"),
		ConversationID:  c.ID,
		BlockID:         c.BlockID,
		Reason:          reason,
		Status:          c.Status,
		TranscriptTurns: len(c.Transcript),
		Transcript:      append([]Turn(nil), c.Transcript...),
		Segments:        append([]ProviderSegment(nil), c.Segments...),
		WorkingMemory:   c.Memory,
		Context:         c.Context,
		TakenAt:         time.Now().UTC(),
	}
	if seg := c.ActiveSegment(); seg != nil {
		checkpoint.SegmentID = seg.ID
		checkpoint.SegmentProvider = seg.Provider
	}
	return checkpoint
}
