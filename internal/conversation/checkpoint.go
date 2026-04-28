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
