package conversation

import (
	"fmt"
	"time"
)

// Status is the lifecycle state of a canonical conversation.
type Status string

const (
	StatusActive Status = "active"
	StatusDone   Status = "done"
	StatusFailed Status = "failed"
)

// TurnRole indicates who produced a transcript turn.
type TurnRole string

const (
	RoleUser      TurnRole = "user"
	RoleAssistant TurnRole = "assistant"
	RoleSystem    TurnRole = "system"
)

// Turn is one canonical transcript entry owned by Milliways.
type Turn struct {
	Role     TurnRole  `json:"role"`
	Provider string    `json:"provider"`
	Text     string    `json:"text"`
	At       time.Time `json:"at"`
}

// MemoryState is a compact working-memory representation for continuation.
type MemoryState struct {
	WorkingSummary string   `json:"working_summary"`
	OpenQuestions  []string `json:"open_questions"`
	ActiveGoals    []string `json:"active_goals"`
	NextAction     string   `json:"next_action"`
	StickyKitchen  string   `json:"sticky_kitchen,omitempty"`
}

// ContextBundle captures recovered context used to rebuild continuity.
type ContextBundle struct {
	SpecRefs               []string `json:"spec_refs"`
	CodeGraphText          string   `json:"codegraph_text"`
	MemPalaceText          string   `json:"mempalace_text"`
	InvalidatedMemoryCount int      `json:"invalidated_memory_count,omitempty"`
}

// SegmentStatus is the lifecycle of one provider segment.
type SegmentStatus string

const (
	SegmentActive    SegmentStatus = "active"
	SegmentDone      SegmentStatus = "done"
	SegmentFailed    SegmentStatus = "failed"
	SegmentExhausted SegmentStatus = "exhausted"
)

// ProviderSegment records one provider attachment to the conversation.
type ProviderSegment struct {
	ID              string        `json:"id"`
	Provider        string        `json:"provider"`
	NativeSessionID string        `json:"native_session_id,omitempty"`
	Status          SegmentStatus `json:"status"`
	StartedAt       time.Time     `json:"started_at"`
	EndedAt         *time.Time    `json:"ended_at,omitempty"`
	EndReason       string        `json:"end_reason,omitempty"`
}

// Conversation is the canonical Milliways-owned task state.
type Conversation struct {
	ID              string                   `json:"id"`
	BlockID         string                   `json:"block_id"`
	Prompt          string                   `json:"prompt"`
	Status          Status                   `json:"status"`
	Transcript      []Turn                   `json:"transcript"`
	Memory          MemoryState              `json:"memory"`
	Context         ContextBundle            `json:"context"`
	Segments        []ProviderSegment        `json:"segments"`
	Checkpoints     []ConversationCheckpoint `json:"checkpoints,omitempty"`
	ActiveSegmentID string                   `json:"active_segment_id,omitempty"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
}

// New creates a new canonical conversation.
func New(id, blockID, prompt string) *Conversation {
	now := time.Now()
	c := &Conversation{
		ID:        id,
		BlockID:   blockID,
		Prompt:    prompt,
		Status:    StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	c.AppendTurn(RoleUser, "user", prompt)
	return c
}

// AppendTurn records a transcript turn.
func (c *Conversation) AppendTurn(role TurnRole, provider, text string) {
	if c == nil || text == "" {
		return
	}
	now := time.Now()
	c.Transcript = append(c.Transcript, Turn{
		Role:     role,
		Provider: provider,
		Text:     text,
		At:       now,
	})
	c.UpdatedAt = now
}

// StartSegment adds and activates a provider segment.
func (c *Conversation) StartSegment(provider string) ProviderSegment {
	now := time.Now()
	seg := ProviderSegment{
		ID:        fmt.Sprintf("%s-seg-%d", c.ID, len(c.Segments)+1),
		Provider:  provider,
		Status:    SegmentActive,
		StartedAt: now,
	}
	c.Segments = append(c.Segments, seg)
	c.ActiveSegmentID = seg.ID
	c.UpdatedAt = now
	return seg
}

// EndActiveSegment finalizes the current provider segment.
func (c *Conversation) EndActiveSegment(status SegmentStatus, reason string) {
	if c == nil || c.ActiveSegmentID == "" {
		return
	}
	now := time.Now()
	for i := range c.Segments {
		if c.Segments[i].ID != c.ActiveSegmentID {
			continue
		}
		c.Segments[i].Status = status
		c.Segments[i].EndedAt = &now
		c.Segments[i].EndReason = reason
		break
	}
	c.ActiveSegmentID = ""
	c.UpdatedAt = now
}

// SetNativeSessionID records a provider-native session ID on the active segment.
func (c *Conversation) SetNativeSessionID(provider, sessionID string) {
	if c == nil || sessionID == "" {
		return
	}
	for i := range c.Segments {
		if c.Segments[i].Provider == provider && c.Segments[i].Status == SegmentActive {
			c.Segments[i].NativeSessionID = sessionID
			c.UpdatedAt = time.Now()
			return
		}
	}
}

// ActiveSegment returns the current active segment, if any.
func (c *Conversation) ActiveSegment() *ProviderSegment {
	if c == nil || c.ActiveSegmentID == "" {
		return nil
	}
	for i := range c.Segments {
		if c.Segments[i].ID == c.ActiveSegmentID {
			return &c.Segments[i]
		}
	}
	return nil
}

// NativeSessionIDs returns the latest known provider-native session IDs.
func (c *Conversation) NativeSessionIDs() map[string]string {
	if c == nil {
		return nil
	}
	out := make(map[string]string)
	for _, seg := range c.Segments {
		if seg.Provider == "" || seg.NativeSessionID == "" {
			continue
		}
		out[seg.Provider] = seg.NativeSessionID
	}
	return out
}
