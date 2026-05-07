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

package rpc

// ParallelDispatchParams are the params for the parallel.dispatch RPC method.
type ParallelDispatchParams struct {
	Prompt    string   `json:"prompt"`
	Providers []string `json:"providers,omitempty"`
	GroupID   string   `json:"group_id,omitempty"`
}

// ParallelSlotInfo describes one successfully opened provider slot.
type ParallelSlotInfo struct {
	Handle   int64  `json:"handle"`
	Provider string `json:"provider"`
}

// SkippedSlot describes a provider that could not be opened.
type SkippedSlot struct {
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
}

// ParallelDispatchResult is the result of a parallel.dispatch call.
type ParallelDispatchResult struct {
	GroupID string             `json:"group_id"`
	Slots   []ParallelSlotInfo `json:"slots"`
	Skipped []SkippedSlot      `json:"skipped,omitempty"`
}

// GroupStatusParams are the params for the group.status RPC method.
type GroupStatusParams struct {
	GroupID string `json:"group_id"`
}

// GroupSlotStatus holds per-slot status in a group.status response.
type GroupSlotStatus struct {
	Handle       int64  `json:"handle"`
	Provider     string `json:"provider"`
	Status       string `json:"status"`
	StartedAt    string `json:"started_at,omitempty"`
	CompletedAt  string `json:"completed_at,omitempty"`
	TokensIn     int    `json:"tokens_in"`
	TokensOut    int    `json:"tokens_out"`
	Model        string `json:"model,omitempty"`
	Text         string `json:"text,omitempty"`
	LastError    string `json:"last_error,omitempty"`
	LastThinking string `json:"last_thinking,omitempty"`
}

// GroupStatusResult is the response for group.status.
type GroupStatusResult struct {
	GroupID     string            `json:"group_id"`
	Prompt      string            `json:"prompt"`
	Status      string            `json:"status"`
	CreatedAt   string            `json:"created_at"`
	CompletedAt string            `json:"completed_at,omitempty"`
	Slots       []GroupSlotStatus `json:"slots"`
}

// GroupSummary is one entry in the group.list response.
type GroupSummary struct {
	GroupID   string `json:"group_id"`
	Prompt    string `json:"prompt"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	SlotCount int    `json:"slot_count"`
}

// GroupListResult is the response for group.list.
type GroupListResult struct {
	Groups []GroupSummary `json:"groups"`
}

// ConsensusAggregateParams are the params for consensus.aggregate.
type ConsensusAggregateParams struct {
	GroupID string `json:"group_id"`
}

// ConsensusAggregateResult is the response for consensus.aggregate.
type ConsensusAggregateResult struct {
	Summary string `json:"summary"`
}
