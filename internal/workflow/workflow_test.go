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
	"reflect"
	"testing"
	"time"
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

func TestStartReadyNodeMovesQueuedReadyNodeToRunning(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 5, 18, 12, 30, 0, 0, time.UTC)
	createdAt := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 18, 11, 5, 0, 0, time.UTC)
	wf := Workflow{
		ID:        "wf-1",
		Goal:      "ship queue runtime",
		Status:    StatusQueued,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Nodes: []Node{
			{
				ID:      "context",
				Type:    NodeContext,
				Status:  StatusCompleted,
				Outputs: map[string]string{"summary": "ready"},
			},
			{
				ID:     " edit ",
				Type:   NodeAgent,
				Status: StatusQueued,
				Client: "codex",
				Agent:  "worker-1",
				Inputs: map[string]string{"task": "runtime"},
				Security: SecurityEnvelope{
					Operation: "edit",
					Paths:     []string{"internal/workflow/workflow.go"},
					Approval:  ApprovalNotRequired,
					Risk:      "workspace-write",
				},
				Memory:    MemoryLink{Reads: []string{"workflow-ready"}},
				Artifacts: []Artifact{{Kind: ArtifactDiff, Path: "internal/workflow/workflow.go"}},
			},
			{
				ID:     "verify",
				Type:   NodeVerification,
				Status: StatusQueued,
			},
		},
		Edges: []Edge{
			{From: "context", To: "edit"},
			{From: "edit", To: "verify"},
		},
	}

	got, err := StartReadyNode(wf, "edit", startedAt)
	if err != nil {
		t.Fatalf("StartReadyNode returned error: %v", err)
	}

	if got.Status != wf.Status || got.ID != wf.ID || got.Goal != wf.Goal || !got.CreatedAt.Equal(createdAt) || !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("workflow fields changed: got %#v, want preserved from %#v", got, wf)
	}
	if !reflect.DeepEqual(got.Edges, wf.Edges) {
		t.Fatalf("edges = %#v, want %#v", got.Edges, wf.Edges)
	}
	if !reflect.DeepEqual(got.Nodes[0], wf.Nodes[0]) || !reflect.DeepEqual(got.Nodes[2], wf.Nodes[2]) {
		t.Fatalf("non-target nodes changed: got %#v, want %#v", got.Nodes, wf.Nodes)
	}
	if got.Nodes[1].Status != StatusRunning {
		t.Fatalf("target status = %q, want %q", got.Nodes[1].Status, StatusRunning)
	}
	if !got.Nodes[1].StartedAt.Equal(startedAt) {
		t.Fatalf("target started_at = %v, want %v", got.Nodes[1].StartedAt, startedAt)
	}

	wantTarget := wf.Nodes[1]
	wantTarget.Status = StatusRunning
	wantTarget.StartedAt = startedAt
	if !reflect.DeepEqual(got.Nodes[1], wantTarget) {
		t.Fatalf("target node = %#v, want only status and started_at changed from %#v", got.Nodes[1], wantTarget)
	}
	if wf.Nodes[1].Status != StatusQueued || !wf.Nodes[1].StartedAt.IsZero() {
		t.Fatalf("original workflow was mutated: %#v", wf.Nodes[1])
	}
}

func TestStartReadyNodeRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 18, 12, 30, 0, 0, time.UTC)
	tests := []struct {
		name string
		wf   Workflow
		id   string
		want error
	}{
		{
			name: "unknown node",
			wf: Workflow{
				ID:    "wf-1",
				Nodes: []Node{{ID: "ready", Status: StatusQueued}},
			},
			id:   "missing",
			want: ErrUnknownNode,
		},
		{
			name: "non queued node",
			wf: Workflow{
				ID:    "wf-1",
				Nodes: []Node{{ID: "running", Status: StatusRunning}},
			},
			id:   "running",
			want: ErrNodeNotQueued,
		},
		{
			name: "queued but blocked",
			wf: Workflow{
				ID: "wf-1",
				Nodes: []Node{
					{ID: "context", Status: StatusRunning},
					{ID: "edit", Status: StatusQueued},
				},
				Edges: []Edge{{From: "context", To: "edit"}},
			},
			id:   "edit",
			want: ErrNodeBlocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := StartReadyNode(tt.wf, tt.id, now)
			if !errors.Is(err, tt.want) {
				t.Fatalf("StartReadyNode error = %v, want %v", err, tt.want)
			}
			if !reflect.DeepEqual(got, Workflow{}) {
				t.Fatalf("StartReadyNode workflow = %#v, want zero value on error", got)
			}
		})
	}
}

func TestCompleteRunningNodeMovesNodeToCompleted(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 5, 18, 12, 30, 0, 0, time.UTC)
	endedAt := time.Date(2026, 5, 18, 12, 45, 0, 0, time.UTC)
	wf := Workflow{
		ID:     "wf-1",
		Status: StatusRunning,
		Nodes: []Node{
			{ID: "edit", Status: StatusRunning, StartedAt: startedAt, Outputs: map[string]string{"old": "kept"}},
			{ID: "verify", Status: StatusQueued},
		},
		Edges: []Edge{{From: "edit", To: "verify"}},
	}

	got, err := CompleteRunningNode(
		wf,
		"edit",
		endedAt,
		map[string]string{"summary": "patched workflow runtime"},
		[]Artifact{{Kind: ArtifactTest, Path: "internal/workflow/workflow_test.go"}},
	)
	if err != nil {
		t.Fatalf("CompleteRunningNode returned error: %v", err)
	}

	if got.Status != wf.Status {
		t.Fatalf("workflow status = %q, want preserved %q", got.Status, wf.Status)
	}
	if got.Nodes[0].Status != StatusCompleted {
		t.Fatalf("node status = %q, want %q", got.Nodes[0].Status, StatusCompleted)
	}
	if !got.Nodes[0].EndedAt.Equal(endedAt) {
		t.Fatalf("ended_at = %v, want %v", got.Nodes[0].EndedAt, endedAt)
	}
	if got.Nodes[0].Error != "" {
		t.Fatalf("node error = %q, want empty", got.Nodes[0].Error)
	}
	if got.Nodes[0].Outputs["summary"] != "patched workflow runtime" || got.Nodes[0].Outputs["old"] != "kept" {
		t.Fatalf("outputs = %#v, want merged old and summary outputs", got.Nodes[0].Outputs)
	}
	if len(got.Nodes[0].Artifacts) != 1 || got.Nodes[0].Artifacts[0].Kind != ArtifactTest {
		t.Fatalf("artifacts = %#v, want appended test artifact", got.Nodes[0].Artifacts)
	}
	if wf.Nodes[0].Status != StatusRunning || !wf.Nodes[0].EndedAt.IsZero() {
		t.Fatalf("original workflow was mutated: %#v", wf.Nodes[0])
	}
}

func TestCompleteRunningNodeMarksWorkflowCompletedWhenNoWorkRemains(t *testing.T) {
	t.Parallel()

	endedAt := time.Date(2026, 5, 18, 13, 0, 0, 0, time.UTC)
	wf := Workflow{
		ID:     "wf-final",
		Status: StatusRunning,
		Nodes: []Node{
			{ID: "context", Status: StatusCompleted},
			{ID: "summary", Status: StatusRunning},
		},
		Edges: []Edge{{From: "context", To: "summary"}},
	}

	got, err := CompleteRunningNode(wf, "summary", endedAt, nil, nil)
	if err != nil {
		t.Fatalf("CompleteRunningNode returned error: %v", err)
	}

	if got.Status != StatusCompleted {
		t.Fatalf("workflow status = %q, want %q", got.Status, StatusCompleted)
	}
	if got.Nodes[1].Status != StatusCompleted {
		t.Fatalf("node status = %q, want %q", got.Nodes[1].Status, StatusCompleted)
	}
}

func TestFailRunningNodeMovesNodeToFailed(t *testing.T) {
	t.Parallel()

	endedAt := time.Date(2026, 5, 18, 12, 50, 0, 0, time.UTC)
	wf := Workflow{
		ID:     "wf-1",
		Status: StatusRunning,
		Nodes:  []Node{{ID: "test", Status: StatusRunning}},
	}

	got, err := FailRunningNode(wf, "test", endedAt, "go test failed")
	if err != nil {
		t.Fatalf("FailRunningNode returned error: %v", err)
	}

	if got.Nodes[0].Status != StatusFailed {
		t.Fatalf("node status = %q, want %q", got.Nodes[0].Status, StatusFailed)
	}
	if !got.Nodes[0].EndedAt.Equal(endedAt) {
		t.Fatalf("ended_at = %v, want %v", got.Nodes[0].EndedAt, endedAt)
	}
	if got.Nodes[0].Error != "go test failed" {
		t.Fatalf("node error = %q, want go test failed", got.Nodes[0].Error)
	}
	if wf.Nodes[0].Status != StatusRunning || wf.Nodes[0].Error != "" {
		t.Fatalf("original workflow was mutated: %#v", wf.Nodes[0])
	}
}

func TestRetryNodeMovesFailedNodeToQueued(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 5, 18, 12, 30, 0, 0, time.UTC)
	endedAt := time.Date(2026, 5, 18, 12, 50, 0, 0, time.UTC)
	createdAt := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 18, 12, 51, 0, 0, time.UTC)
	retriedAt := time.Date(2026, 5, 18, 13, 0, 0, 0, time.UTC)
	wf := Workflow{
		ID:        "wf-retry",
		Goal:      "retry failed verification",
		Status:    StatusFailed,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Nodes: []Node{
			{
				ID:     "edit",
				Type:   NodeAgent,
				Status: StatusCompleted,
				Outputs: map[string]string{
					"summary": "patch applied",
				},
				Artifacts: []Artifact{{Kind: ArtifactDiff, Path: "internal/workflow/workflow.go"}},
			},
			{
				ID:         " verify ",
				Type:       NodeVerification,
				Status:     StatusFailed,
				Client:     "codex",
				Agent:      "worker-1",
				Inputs:     map[string]string{"command": "go test ./internal/workflow"},
				Outputs:    map[string]string{"stdout": "old output is kept"},
				Artifacts:  []Artifact{{Kind: ArtifactTest, Path: "internal/workflow/workflow_test.go"}},
				StartedAt:  startedAt,
				EndedAt:    endedAt,
				Error:      "test failed",
				RetryCount: 2,
			},
			{
				ID:     "summary",
				Type:   NodeSummary,
				Status: StatusQueued,
			},
		},
		Edges: []Edge{
			{From: "edit", To: "verify"},
			{From: "verify", To: "summary"},
		},
	}

	got, err := RetryNode(wf, "verify", retriedAt)
	if err != nil {
		t.Fatalf("RetryNode returned error: %v", err)
	}

	if got.Status != StatusQueued {
		t.Fatalf("workflow status = %q, want %q", got.Status, StatusQueued)
	}
	if !got.UpdatedAt.Equal(retriedAt) {
		t.Fatalf("workflow updated_at = %v, want %v", got.UpdatedAt, retriedAt)
	}
	if got.ID != wf.ID || got.Goal != wf.Goal || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("workflow fields changed: got %#v, want preserved from %#v", got, wf)
	}
	if !reflect.DeepEqual(got.Edges, wf.Edges) {
		t.Fatalf("edges = %#v, want %#v", got.Edges, wf.Edges)
	}
	if !reflect.DeepEqual(got.Nodes[0], wf.Nodes[0]) || !reflect.DeepEqual(got.Nodes[2], wf.Nodes[2]) {
		t.Fatalf("non-target nodes changed: got %#v, want %#v", got.Nodes, wf.Nodes)
	}

	wantTarget := wf.Nodes[1]
	wantTarget.Status = StatusQueued
	wantTarget.StartedAt = time.Time{}
	wantTarget.EndedAt = time.Time{}
	wantTarget.Error = ""
	wantTarget.RetryCount = 3
	if !reflect.DeepEqual(got.Nodes[1], wantTarget) {
		t.Fatalf("target node = %#v, want %#v", got.Nodes[1], wantTarget)
	}
	if wf.Status != StatusFailed || !wf.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("original workflow was mutated: %#v", wf)
	}
	if wf.Nodes[1].Status != StatusFailed || wf.Nodes[1].Error == "" || wf.Nodes[1].RetryCount != 2 {
		t.Fatalf("original target node was mutated: %#v", wf.Nodes[1])
	}
}

func TestRetryNodeMovesCanceledNodeToQueuedAndRunningWorkflowWhenActiveWorkExists(t *testing.T) {
	t.Parallel()

	retriedAt := time.Date(2026, 5, 18, 13, 5, 0, 0, time.UTC)
	wf := Workflow{
		ID:     "wf-retry-active",
		Status: StatusCanceled,
		Nodes: []Node{
			{ID: "test", Status: StatusCanceled, EndedAt: retriedAt.Add(-time.Minute), Error: "stopped"},
			{ID: "approval", Status: StatusWaitingApproval},
		},
	}

	got, err := RetryNode(wf, "test", retriedAt)
	if err != nil {
		t.Fatalf("RetryNode returned error: %v", err)
	}

	if got.Status != StatusRunning {
		t.Fatalf("workflow status = %q, want %q while active work remains", got.Status, StatusRunning)
	}
	if got.Nodes[0].Status != StatusQueued {
		t.Fatalf("retried node status = %q, want %q", got.Nodes[0].Status, StatusQueued)
	}
	if got.Nodes[0].RetryCount != 1 {
		t.Fatalf("retry_count = %d, want 1", got.Nodes[0].RetryCount)
	}
}

func TestRetryNodeInvalidatesDependentsForPartialReexecution(t *testing.T) {
	t.Parallel()

	retriedAt := time.Date(2026, 5, 19, 9, 35, 0, 0, time.UTC)
	endedAt := time.Date(2026, 5, 19, 9, 30, 0, 0, time.UTC)
	wf := Workflow{
		ID:     "wf-retry-chain",
		Status: StatusFailed,
		Nodes: []Node{
			{ID: "context", Status: StatusCompleted},
			{ID: "edit", Status: StatusFailed, EndedAt: endedAt, Error: "patch failed"},
			{ID: "test", Status: StatusCompleted, EndedAt: endedAt, Outputs: map[string]string{"result": "old"}},
			{ID: "release", Status: StatusQueued},
			{ID: "summary", Status: StatusSkipped, EndedAt: endedAt},
		},
		Edges: []Edge{
			{From: "context", To: "edit"},
			{From: "edit", To: "test"},
			{From: "test", To: "release"},
		},
	}

	got, err := RetryNode(wf, "edit", retriedAt)
	if err != nil {
		t.Fatalf("RetryNode returned error: %v", err)
	}

	for _, node := range got.Nodes {
		switch node.ID {
		case "edit", "test", "release":
			if node.Status != StatusQueued {
				t.Fatalf("%s status = %q, want queued for partial re-execution", node.ID, node.Status)
			}
			if !node.EndedAt.IsZero() || node.Error != "" {
				t.Fatalf("%s execution state not cleared: %#v", node.ID, node)
			}
		case "context":
			if node.Status != StatusCompleted {
				t.Fatalf("context status = %q, want completed", node.Status)
			}
		case "summary":
			if node.Status != StatusSkipped {
				t.Fatalf("summary status = %q, want skipped preserved", node.Status)
			}
		}
	}
	if got.Nodes[2].Outputs["result"] != "old" {
		t.Fatalf("dependent outputs = %#v, want preserved artifacts/outputs", got.Nodes[2].Outputs)
	}
}

func TestRetryNodeRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	retriedAt := time.Date(2026, 5, 18, 13, 10, 0, 0, time.UTC)
	tests := []struct {
		name string
		wf   Workflow
		id   string
		want error
	}{
		{
			name: "invalid workflow",
			wf:   Workflow{Nodes: []Node{{ID: "failed", Status: StatusFailed}}},
			id:   "failed",
			want: ErrMissingWorkflowID,
		},
		{
			name: "unknown node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "failed", Status: StatusFailed}}},
			id:   "missing",
			want: ErrUnknownNode,
		},
		{
			name: "queued node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "queued", Status: StatusQueued}}},
			id:   "queued",
			want: ErrNodeNotRetryable,
		},
		{
			name: "running node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "running", Status: StatusRunning}}},
			id:   "running",
			want: ErrNodeNotRetryable,
		},
		{
			name: "completed node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "done", Status: StatusCompleted}}},
			id:   "done",
			want: ErrNodeNotRetryable,
		},
		{
			name: "skipped node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "skipped", Status: StatusSkipped}}},
			id:   "skipped",
			want: ErrNodeNotRetryable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RetryNode(tt.wf, tt.id, retriedAt)
			if !errors.Is(err, tt.want) {
				t.Fatalf("RetryNode error = %v, want %v", err, tt.want)
			}
			if !reflect.DeepEqual(got, Workflow{}) {
				t.Fatalf("RetryNode workflow = %#v, want zero value on error", got)
			}
		})
	}
}

func TestFinishRunningNodeRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 18, 12, 55, 0, 0, time.UTC)
	tests := []struct {
		name string
		wf   Workflow
		id   string
		want error
	}{
		{
			name: "unknown node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "running", Status: StatusRunning}}},
			id:   "missing",
			want: ErrUnknownNode,
		},
		{
			name: "non running node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "queued", Status: StatusQueued}}},
			id:   "queued",
			want: ErrNodeNotRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completed, completeErr := CompleteRunningNode(tt.wf, tt.id, now, nil, nil)
			if !errors.Is(completeErr, tt.want) {
				t.Fatalf("CompleteRunningNode error = %v, want %v", completeErr, tt.want)
			}
			if !reflect.DeepEqual(completed, Workflow{}) {
				t.Fatalf("CompleteRunningNode workflow = %#v, want zero value on error", completed)
			}
			failed, failErr := FailRunningNode(tt.wf, tt.id, now, "failed")
			if !errors.Is(failErr, tt.want) {
				t.Fatalf("FailRunningNode error = %v, want %v", failErr, tt.want)
			}
			if !reflect.DeepEqual(failed, Workflow{}) {
				t.Fatalf("FailRunningNode workflow = %#v, want zero value on error", failed)
			}
		})
	}
}

func TestCancelWorkflowCancelsActiveNodesAndPreservesTerminalNodes(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 18, 10, 15, 0, 0, time.UTC)
	endedAt := time.Date(2026, 5, 18, 10, 20, 0, 0, time.UTC)
	canceledAt := time.Date(2026, 5, 18, 10, 30, 0, 0, time.UTC)
	wf := Workflow{
		ID:        "wf-cancel",
		Goal:      "stop pending work",
		Status:    StatusRunning,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Nodes: []Node{
			{ID: "queued", Status: StatusQueued},
			{ID: "running", Status: StatusRunning, StartedAt: updatedAt},
			{ID: "approval", Status: StatusWaitingApproval},
			{ID: "resumed", Status: StatusResumed},
			{ID: "verify", Status: StatusVerifying},
			{ID: "done", Status: StatusCompleted, EndedAt: endedAt, Outputs: map[string]string{"summary": "kept"}},
			{ID: "failed", Status: StatusFailed, EndedAt: endedAt, Error: "test failed"},
			{ID: "skipped", Status: StatusSkipped, EndedAt: endedAt},
			{ID: "canceled", Status: StatusCanceled, EndedAt: endedAt, Error: "already stopped"},
		},
		Edges: []Edge{
			{From: "queued", To: "running"},
			{From: "running", To: "approval"},
		},
	}

	got, err := CancelWorkflow(wf, canceledAt, "user requested cancellation")
	if err != nil {
		t.Fatalf("CancelWorkflow returned error: %v", err)
	}

	if got.Status != StatusCanceled {
		t.Fatalf("workflow status = %q, want %q", got.Status, StatusCanceled)
	}
	if !got.UpdatedAt.Equal(canceledAt) {
		t.Fatalf("workflow updated_at = %v, want %v", got.UpdatedAt, canceledAt)
	}
	if got.ID != wf.ID || got.Goal != wf.Goal || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("workflow fields changed: got %#v, want preserved from %#v", got, wf)
	}
	if !reflect.DeepEqual(got.Edges, wf.Edges) {
		t.Fatalf("edges = %#v, want %#v", got.Edges, wf.Edges)
	}

	for i, node := range got.Nodes[:5] {
		if node.Status != StatusCanceled {
			t.Fatalf("active node %d status = %q, want %q", i, node.Status, StatusCanceled)
		}
		if !node.EndedAt.Equal(canceledAt) {
			t.Fatalf("active node %d ended_at = %v, want %v", i, node.EndedAt, canceledAt)
		}
		if node.Error != "user requested cancellation" {
			t.Fatalf("active node %d error = %q, want cancellation reason", i, node.Error)
		}
	}
	if !reflect.DeepEqual(got.Nodes[5:], wf.Nodes[5:]) {
		t.Fatalf("terminal nodes changed: got %#v, want %#v", got.Nodes[5:], wf.Nodes[5:])
	}
	if wf.Status != StatusRunning || !wf.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("original workflow was mutated: %#v", wf)
	}
	if wf.Nodes[0].Status != StatusQueued || !wf.Nodes[0].EndedAt.IsZero() || wf.Nodes[0].Error != "" {
		t.Fatalf("original active node was mutated: %#v", wf.Nodes[0])
	}
}

func TestCancelWorkflowRejectsInvalidWorkflow(t *testing.T) {
	t.Parallel()

	got, err := CancelWorkflow(Workflow{Nodes: []Node{{ID: "queued", Status: StatusQueued}}}, time.Date(2026, 5, 18, 10, 30, 0, 0, time.UTC), "stop")
	if !errors.Is(err, ErrMissingWorkflowID) {
		t.Fatalf("CancelWorkflow error = %v, want %v", err, ErrMissingWorkflowID)
	}
	if !reflect.DeepEqual(got, Workflow{}) {
		t.Fatalf("CancelWorkflow workflow = %#v, want zero value on error", got)
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
			Memory:     MemoryLink{Reads: []string{"repo-context"}, Writes: []string{"approval-decision"}},
			Artifacts:  []Artifact{{Kind: ArtifactApproval, Ref: "approval:42"}},
			RetryCount: 2,
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
	if got.Nodes[0].RetryCount != 2 {
		t.Fatalf("retry_count = %d, want 2", got.Nodes[0].RetryCount)
	}
}

func TestWaitForApprovalNodeMovesRunningNodeToWaitingApproval(t *testing.T) {
	t.Parallel()

	waitedAt := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	wf := Workflow{
		ID:     "wf-approval",
		Status: StatusRunning,
		Nodes: []Node{{
			ID:     "edit",
			Status: StatusRunning,
			Security: SecurityEnvelope{
				Operation: "write",
				Paths:     []string{"nextgen.md"},
				Approval:  ApprovalRequired,
				Risk:      "workspace-write",
			},
		}},
	}

	got, err := WaitForApprovalNode(wf, "edit", waitedAt, "write nextgen.md")
	if err != nil {
		t.Fatalf("WaitForApprovalNode returned error: %v", err)
	}

	if got.Nodes[0].Status != StatusWaitingApproval {
		t.Fatalf("node status = %q, want %q", got.Nodes[0].Status, StatusWaitingApproval)
	}
	if got.Nodes[0].Error != "write nextgen.md" {
		t.Fatalf("node error = %q, want approval reason", got.Nodes[0].Error)
	}
	if wf.Nodes[0].Status != StatusRunning || wf.Nodes[0].Error != "" {
		t.Fatalf("original workflow was mutated: %#v", wf.Nodes[0])
	}
}

func TestWaitForApprovalNodeRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 19, 10, 5, 0, 0, time.UTC)
	tests := []struct {
		name string
		wf   Workflow
		id   string
		want error
	}{
		{
			name: "unknown node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "running", Status: StatusRunning}}},
			id:   "missing",
			want: ErrUnknownNode,
		},
		{
			name: "non running node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "queued", Status: StatusQueued}}},
			id:   "queued",
			want: ErrNodeNotRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := WaitForApprovalNode(tt.wf, tt.id, now, "reason")
			if !errors.Is(err, tt.want) {
				t.Fatalf("WaitForApprovalNode error = %v, want %v", err, tt.want)
			}
			if !reflect.DeepEqual(got, Workflow{}) {
				t.Fatalf("WaitForApprovalNode workflow = %#v, want zero value on error", got)
			}
		})
	}
}

func TestResumeApprovalNodeMovesWaitingNodeToResumed(t *testing.T) {
	t.Parallel()

	resumedAt := time.Date(2026, 5, 19, 10, 10, 0, 0, time.UTC)
	wf := Workflow{
		ID:     "wf-resume",
		Status: StatusRunning,
		Nodes: []Node{{
			ID:     "edit",
			Status: StatusWaitingApproval,
			Error:  "pending approval",
		}},
	}

	got, err := ResumeApprovalNode(wf, "edit", resumedAt)
	if err != nil {
		t.Fatalf("ResumeApprovalNode returned error: %v", err)
	}

	if got.Nodes[0].Status != StatusResumed {
		t.Fatalf("node status = %q, want %q", got.Nodes[0].Status, StatusResumed)
	}
	if got.Nodes[0].Error != "" {
		t.Fatalf("node error = %q, want cleared", got.Nodes[0].Error)
	}
	if wf.Nodes[0].Status != StatusWaitingApproval || wf.Nodes[0].Error != "pending approval" {
		t.Fatalf("original workflow was mutated: %#v", wf.Nodes[0])
	}
}

func TestResumeApprovalNodeRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 19, 10, 15, 0, 0, time.UTC)
	tests := []struct {
		name string
		wf   Workflow
		id   string
		want error
	}{
		{
			name: "unknown node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "waiting", Status: StatusWaitingApproval}}},
			id:   "missing",
			want: ErrUnknownNode,
		},
		{
			name: "not waiting approval",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "running", Status: StatusRunning}}},
			id:   "running",
			want: ErrNodeNotWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResumeApprovalNode(tt.wf, tt.id, now)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ResumeApprovalNode error = %v, want %v", err, tt.want)
			}
			if !reflect.DeepEqual(got, Workflow{}) {
				t.Fatalf("ResumeApprovalNode workflow = %#v, want zero value on error", got)
			}
		})
	}
}

func TestDenyApprovalNodeMovesWaitingNodeToFailed(t *testing.T) {
	t.Parallel()

	deniedAt := time.Date(2026, 5, 19, 10, 20, 0, 0, time.UTC)
	wf := Workflow{
		ID:     "wf-deny",
		Status: StatusRunning,
		Nodes: []Node{{
			ID:     "edit",
			Status: StatusWaitingApproval,
			Security: SecurityEnvelope{
				Approval: ApprovalRequired,
			},
		}},
	}

	got, err := DenyApprovalNode(wf, "edit", deniedAt, "denied by user")
	if err != nil {
		t.Fatalf("DenyApprovalNode returned error: %v", err)
	}

	if got.Nodes[0].Status != StatusFailed {
		t.Fatalf("node status = %q, want %q", got.Nodes[0].Status, StatusFailed)
	}
	if got.Nodes[0].Error != "denied by user" {
		t.Fatalf("node error = %q, want denial reason", got.Nodes[0].Error)
	}
	if got.Nodes[0].Security.Approval != ApprovalDenied {
		t.Fatalf("security approval = %q, want %q", got.Nodes[0].Security.Approval, ApprovalDenied)
	}
}

func TestDenyApprovalNodeRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 19, 10, 25, 0, 0, time.UTC)
	tests := []struct {
		name string
		wf   Workflow
		id   string
		want error
	}{
		{
			name: "unknown node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "waiting", Status: StatusWaitingApproval}}},
			id:   "missing",
			want: ErrUnknownNode,
		},
		{
			name: "not waiting approval",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "running", Status: StatusRunning}}},
			id:   "running",
			want: ErrNodeNotWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DenyApprovalNode(tt.wf, tt.id, now, "reason")
			if !errors.Is(err, tt.want) {
				t.Fatalf("DenyApprovalNode error = %v, want %v", err, tt.want)
			}
			if !reflect.DeepEqual(got, Workflow{}) {
				t.Fatalf("DenyApprovalNode workflow = %#v, want zero value on error", got)
			}
		})
	}
}

func TestVerifyRunningNodeMovesResumedNodeToVerifying(t *testing.T) {
	t.Parallel()

	wf := Workflow{
		ID:     "wf-verify",
		Status: StatusRunning,
		Nodes: []Node{{
			ID:     "test",
			Status: StatusResumed,
		}},
	}

	got, err := VerifyRunningNode(wf, "test")
	if err != nil {
		t.Fatalf("VerifyRunningNode returned error: %v", err)
	}

	if got.Nodes[0].Status != StatusVerifying {
		t.Fatalf("node status = %q, want %q", got.Nodes[0].Status, StatusVerifying)
	}
	if wf.Nodes[0].Status != StatusResumed {
		t.Fatalf("original workflow was mutated: %#v", wf.Nodes[0])
	}
}

func TestVerifyRunningNodeRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		wf   Workflow
		id   string
		want error
	}{
		{
			name: "unknown node",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "running", Status: StatusResumed}}},
			id:   "missing",
			want: ErrUnknownNode,
		},
		{
			name: "not resumed",
			wf:   Workflow{ID: "wf-1", Nodes: []Node{{ID: "running", Status: StatusRunning}}},
			id:   "running",
			want: ErrNodeNotResumed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := VerifyRunningNode(tt.wf, tt.id)
			if !errors.Is(err, tt.want) {
				t.Fatalf("VerifyRunningNode error = %v, want %v", err, tt.want)
			}
			if !reflect.DeepEqual(got, Workflow{}) {
				t.Fatalf("VerifyRunningNode workflow = %#v, want zero value on error", got)
			}
		})
	}
}
