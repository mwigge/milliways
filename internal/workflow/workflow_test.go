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

package workflow

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateAcceptsAcyclicWorkflowWithSecurityAndMemoryMetadata(t *testing.T) {
	t.Parallel()

	wf := Workflow{
		ID:     "wf-1",
		Goal:   "implement capability matrix",
		Status: StatusQueued,
		Nodes: []Node{
			{
				ID:     "context",
				Type:   NodeContext,
				Status: StatusCompleted,
				Security: SecurityEnvelope{
					Operation: "read",
					Paths:     []string{"internal/daemon"},
					Approval:  ApprovalNotRequired,
					Risk:      "workspace-read",
				},
				Memory: MemoryLink{Reads: []string{"project-summary"}},
			},
			{
				ID:     "edit",
				Type:   NodeToolCall,
				Status: StatusQueued,
				Security: SecurityEnvelope{
					Operation: "edit",
					Paths:     []string{"cmd/milliwaysctl/capabilities.go"},
					Approval:  ApprovalRequired,
					Risk:      "workspace-write",
				},
				Artifacts: []Artifact{{Kind: ArtifactDiff, Path: "cmd/milliwaysctl/capabilities.go"}},
			},
		},
		Edges: []Edge{{From: "context", To: "edit"}},
	}

	if err := Validate(wf); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestValidateRejectsInvalidWorkflowGraphs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		wf   Workflow
		want error
	}{
		{
			name: "missing workflow id",
			wf:   Workflow{Nodes: []Node{{ID: "a"}}},
			want: ErrMissingWorkflowID,
		},
		{
			name: "duplicate node id",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "a"}, {ID: "a"}}},
			want: ErrDuplicateNode,
		},
		{
			name: "missing edge endpoint",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "a"}}, Edges: []Edge{{From: "a", To: "b"}}},
			want: ErrUnknownNode,
		},
		{
			name: "cycle",
			wf: Workflow{
				ID:    "wf-1",
				Nodes: []Node{{ID: "a"}, {ID: "b"}},
				Edges: []Edge{{From: "a", To: "b"}, {From: "b", To: "a"}},
			},
			want: ErrCycle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.wf)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Validate error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestReadyNodesReturnsQueuedNodesWithCompletedDependencies(t *testing.T) {
	t.Parallel()

	wf := Workflow{
		ID: "wf-1",
		Nodes: []Node{
			{ID: "context", Status: StatusCompleted},
			{ID: "edit", Status: StatusQueued},
			{ID: "test", Status: StatusQueued},
			{ID: "release", Status: StatusQueued},
			{ID: "blocked", Status: StatusWaitingApproval},
		},
		Edges: []Edge{
			{From: "context", To: "edit"},
			{From: "edit", To: "test"},
			{From: "test", To: "release"},
			{From: "context", To: "blocked"},
		},
	}

	ready, err := ReadyNodes(wf)
	if err != nil {
		t.Fatalf("ReadyNodes returned error: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "edit" {
		t.Fatalf("ready nodes = %#v, want only edit", ready)
	}
}

func TestReadyNodesUsesCanonicalTrimmedIDs(t *testing.T) {
	t.Parallel()

	wf := Workflow{
		ID: "wf-1",
		Nodes: []Node{
			{ID: " context ", Status: StatusCompleted},
			{ID: " edit ", Status: StatusQueued},
		},
		Edges: []Edge{{From: "context", To: "edit"}},
	}

	ready, err := ReadyNodes(wf)
	if err != nil {
		t.Fatalf("ReadyNodes returned error: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != " edit " {
		t.Fatalf("ready nodes = %#v, want queued edit node", ready)
	}
}

func TestWorkflowJSONRoundTripPreservesContractFields(t *testing.T) {
	t.Parallel()

	wf := Workflow{
		ID:     "wf-1",
		Goal:   "ship first slice",
		Status: StatusRunning,
		Nodes: []Node{{
			ID:     "approval-1",
			Type:   NodeApproval,
			Status: StatusWaitingApproval,
			Client: "codex",
			Agent:  "worker-1",
			Security: SecurityEnvelope{
				Operation: "write",
				Paths:     []string{"nextgen.md"},
				Approval:  ApprovalRequired,
				Risk:      "workspace-write",
				Reason:    "file write requires explicit approval",
			},
			Memory:    MemoryLink{Reads: []string{"repo-context"}, Writes: []string{"approval-decision"}},
			Artifacts: []Artifact{{Kind: ArtifactApproval, Ref: "approval:42"}},
		}},
	}

	raw, err := json.Marshal(wf)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Workflow
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Nodes[0].Security.Approval != ApprovalRequired {
		t.Fatalf("approval = %q, want %q", got.Nodes[0].Security.Approval, ApprovalRequired)
	}
	if got.Nodes[0].Memory.Writes[0] != "approval-decision" {
		t.Fatalf("memory writes = %#v, want approval-decision", got.Nodes[0].Memory.Writes)
	}
	if got.Nodes[0].Artifacts[0].Kind != ArtifactApproval {
		t.Fatalf("artifact kind = %q, want %q", got.Nodes[0].Artifacts[0].Kind, ArtifactApproval)
	}
}
