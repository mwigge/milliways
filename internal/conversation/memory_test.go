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

import (
	"testing"
	"time"
)

func TestEvaluateMemoryCandidate(t *testing.T) {
	t.Parallel()

	now := time.Now()
	stale := now.Add(-time.Minute)

	tests := []struct {
		name      string
		candidate MemoryCandidate
		existing  []string
		accept    bool
		reason    string
	}{
		{
			name: "working memory accepted",
			candidate: MemoryCandidate{
				SourceKind: "provider_output",
				MemoryType: MemoryWorking,
				Text:       "continue editing dispatch.go",
			},
			accept: true,
			reason: "session-scoped memory",
		},
		{
			name: "untrusted semantic rejected",
			candidate: MemoryCandidate{
				SourceKind: "provider_output",
				MemoryType: MemorySemantic,
				Text:       "remember this forever",
				Confidence: 0.9,
			},
			accept: false,
			reason: "untrusted source",
		},
		{
			name: "duplicate rejected",
			candidate: MemoryCandidate{
				SourceKind: "user",
				MemoryType: MemorySemantic,
				Text:       "repo uses bubbletea",
				Confidence: 0.95,
			},
			existing: []string{"repo uses bubbletea"},
			accept:   false,
			reason:   "duplicate",
		},
		{
			name: "stale rejected",
			candidate: MemoryCandidate{
				SourceKind: "repo_context",
				MemoryType: MemorySemantic,
				Text:       "old fact",
				Confidence: 0.95,
				FreshUntil: &stale,
			},
			accept: false,
			reason: "stale",
		},
		{
			name: "trusted semantic accepted",
			candidate: MemoryCandidate{
				SourceKind: "spec",
				MemoryType: MemoryProcedural,
				Text:       "provider failover stays in one block",
				Confidence: 1.0,
			},
			accept: true,
			reason: "accepted",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EvaluateMemoryCandidate(tt.candidate, tt.existing, now)
			if got.Accept != tt.accept || got.Reason != tt.reason {
				t.Fatalf("EvaluateMemoryCandidate() = %#v", got)
			}
		})
	}
}

func TestDefaultRetrievalPlan(t *testing.T) {
	t.Parallel()

	got := DefaultRetrievalPlan()
	want := []MemoryType{MemoryWorking, MemoryEpisodic, MemoryProcedural, MemorySemantic}
	if len(got.OrderedTypes) != len(want) {
		t.Fatalf("OrderedTypes = %#v", got.OrderedTypes)
	}
	for i := range want {
		if got.OrderedTypes[i] != want[i] {
			t.Fatalf("OrderedTypes[%d] = %q, want %q", i, got.OrderedTypes[i], want[i])
		}
	}
	if !got.Bounded {
		t.Fatal("expected retrieval plan to be bounded")
	}
}
