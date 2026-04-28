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

package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/sommelier"
	"github.com/mwigge/milliways/internal/substrate"
)

type restartAwareReader struct {
	record substrate.ConversationRecord
	reads  int
}

func (r *restartAwareReader) GetConversation(_ context.Context, id string) (substrate.ConversationRecord, error) {
	r.reads++
	rec := r.record
	rec.ConversationID = id
	return rec, nil
}

func (r *restartAwareReader) InvalidateConversation(string) {}

type resumeAdapter struct {
	sessionID string
	events    []adapter.Event
	seenTasks []kitchen.Task
}

func (r *resumeAdapter) Exec(_ context.Context, task kitchen.Task) (<-chan adapter.Event, error) {
	r.seenTasks = append(r.seenTasks, task)
	ch := make(chan adapter.Event, len(r.events))
	for _, evt := range r.events {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func (r *resumeAdapter) Send(context.Context, string) error { return nil }
func (r *resumeAdapter) SupportsResume() bool               { return true }
func (r *resumeAdapter) SessionID() string                  { return r.sessionID }
func (r *resumeAdapter) ProcessID() int                     { return 0 }
func (r *resumeAdapter) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{NativeResume: true, InteractiveSend: true, StructuredEvents: true}
}

func TestOrchestratorResumesFromSubstrateAfterRestartSimulation(t *testing.T) {
	t.Parallel()

	startedAt := time.Now().Add(-1 * time.Minute)
	reader := &restartAwareReader{record: substrate.ConversationRecord{
		ConversationID: "conv-resume",
		BlockID:        "b1",
		Prompt:         "finish the migration",
		Status:         string(conversation.StatusActive),
		Transcript: []conversation.Turn{
			{Role: conversation.RoleUser, Provider: "user", Text: "finish the migration"},
			{Role: conversation.RoleAssistant, Provider: "claude", Text: "I already migrated the schema."},
		},
		Memory: conversation.MemoryState{
			WorkingSummary: "schema migrated, need verification",
			NextAction:     "run the verification step",
		},
		Segments: []conversation.ProviderSegment{{
			ID:              "seg-active",
			Provider:        "claude",
			NativeSessionID: "sess-claude-1",
			Status:          conversation.SegmentActive,
			StartedAt:       startedAt,
		}},
		ActiveSegmentID: "seg-active",
	}}

	adapter := &resumeAdapter{
		sessionID: "sess-claude-1",
		events: []adapter.Event{
			{Type: adapter.EventText, Kitchen: "claude", Text: "Verification is complete."},
			{Type: adapter.EventDone, Kitchen: "claude", ExitCode: 0},
		},
	}

	var seenResumeSessionIDs map[string]string
	o := Orchestrator{
		Reader: reader,
		Factory: func(_ context.Context, prompt string, exclude map[string]bool, kitchenForce string, resumeSessionIDs map[string]string) (RouteResult, error) {
			seenResumeSessionIDs = resumeSessionIDs
			if prompt != "finish the migration" {
				t.Fatalf("prompt = %q, want original prompt", prompt)
			}
			if len(exclude) != 0 {
				t.Fatalf("exclude = %#v, want empty", exclude)
			}
			if kitchenForce != "" {
				t.Fatalf("kitchenForce = %q, want empty", kitchenForce)
			}
			return RouteResult{
				Decision: sommelier.Decision{Kitchen: "claude", Reason: "resuming existing segment"},
				Adapter:  adapter,
			}, nil
		},
	}

	conv, err := o.Run(context.Background(), RunRequest{
		ConversationID: "conv-resume",
		BlockID:        "b1",
		Prompt:         "finish the migration",
	}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if conv.Status != conversation.StatusDone {
		t.Fatalf("status = %q, want %q", conv.Status, conversation.StatusDone)
	}
	if reader.reads == 0 {
		t.Fatal("expected restart reader to restore substrate state")
	}
	if got := seenResumeSessionIDs["claude"]; got != "sess-claude-1" {
		t.Fatalf("resumeSessionIDs[claude] = %q, want sess-claude-1", got)
	}
	if len(conv.Segments) != 1 {
		t.Fatalf("segments = %d, want 1 resumed segment", len(conv.Segments))
	}
	if conv.Segments[0].ID != "seg-active" {
		t.Fatalf("segment ID = %q, want seg-active", conv.Segments[0].ID)
	}
	if conv.Segments[0].Status != conversation.SegmentDone {
		t.Fatalf("segment status = %q, want %q", conv.Segments[0].Status, conversation.SegmentDone)
	}
	if len(adapter.seenTasks) != 1 || adapter.seenTasks[0].Prompt != "finish the migration" {
		t.Fatalf("seen tasks = %#v, want original resumed prompt", adapter.seenTasks)
	}
	if conv.Memory.WorkingSummary != "schema migrated, need verification" {
		t.Fatalf("WorkingSummary = %q, want restored summary", conv.Memory.WorkingSummary)
	}
	if len(conv.Transcript) != 3 || conv.Transcript[1].Text != "I already migrated the schema." || conv.Transcript[2].Text != "Verification is complete." {
		t.Fatalf("Transcript = %#v, want restored transcript plus resumed output", conv.Transcript)
	}
}
