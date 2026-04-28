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

// MemoryType identifies which memory layer a candidate belongs to.
type MemoryType string

const (
	MemoryWorking    MemoryType = "working"
	MemoryEpisodic   MemoryType = "episodic"
	MemorySemantic   MemoryType = "semantic"
	MemoryProcedural MemoryType = "procedural"
)

// MemoryCandidate is a proposed memory write evaluated before promotion.
type MemoryCandidate struct {
	SourceKind string     `json:"source_kind"`
	MemoryType MemoryType `json:"memory_type"`
	Text       string     `json:"text"`
	Scope      string     `json:"scope"`
	Confidence float64    `json:"confidence"`
	FreshUntil *time.Time `json:"fresh_until,omitempty"`
}

// MemoryDecision records whether a memory candidate should be persisted.
type MemoryDecision struct {
	Accept bool   `json:"accept"`
	Reason string `json:"reason"`
}

// RetrievalPlan describes which memory layers should be injected for
// continuation in the order they should be considered.
type RetrievalPlan struct {
	OrderedTypes []MemoryType `json:"ordered_types"`
	Bounded      bool         `json:"bounded"`
}

// DefaultRetrievalPlan returns the continuity-first memory retrieval order.
func DefaultRetrievalPlan() RetrievalPlan {
	return RetrievalPlan{
		OrderedTypes: []MemoryType{
			MemoryWorking,
			MemoryEpisodic,
			MemoryProcedural,
			MemorySemantic,
		},
		Bounded: true,
	}
}

// EvaluateMemoryCandidate applies the default long-lived memory write policy.
func EvaluateMemoryCandidate(candidate MemoryCandidate, existing []string, now time.Time) MemoryDecision {
	if candidate.Text == "" {
		return MemoryDecision{Accept: false, Reason: "empty text"}
	}
	if candidate.MemoryType == MemoryWorking || candidate.MemoryType == MemoryEpisodic {
		return MemoryDecision{Accept: true, Reason: "session-scoped memory"}
	}
	if candidate.SourceKind != "spec" && candidate.SourceKind != "user" && candidate.SourceKind != "repo_context" && candidate.SourceKind != "accepted_fact" {
		return MemoryDecision{Accept: false, Reason: "untrusted source"}
	}
	if candidate.Confidence > 0 && candidate.Confidence < 0.75 {
		return MemoryDecision{Accept: false, Reason: "low confidence"}
	}
	if candidate.FreshUntil != nil && now.After(*candidate.FreshUntil) {
		return MemoryDecision{Accept: false, Reason: "stale"}
	}
	for _, item := range existing {
		if item == candidate.Text {
			return MemoryDecision{Accept: false, Reason: "duplicate"}
		}
	}
	return MemoryDecision{Accept: true, Reason: "accepted"}
}
