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

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/mwigge/milliways/internal/daemon/observability"
	"github.com/mwigge/milliways/internal/workflow"
)

func TestWorkflowListRPCReturnsStoredSummaries(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-a",
		Goal:   "first graph",
		Status: workflow.StatusQueued,
		Nodes:  []workflow.Node{{ID: "context", Status: workflow.StatusQueued}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.list",
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Workflows []workflow.Summary `json:"workflows"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.list response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.list error = %+v", resp.Error)
	}
	if len(resp.Result.Workflows) != 1 || resp.Result.Workflows[0].ID != "wf-a" {
		t.Fatalf("workflows = %#v, want wf-a summary", resp.Result.Workflows)
	}
	if resp.Result.Workflows[0].Goal != "first graph" || resp.Result.Workflows[0].Nodes != 1 {
		t.Fatalf("summary = %#v, want goal and node count", resp.Result.Workflows[0])
	}
}

func TestWorkflowGetRPCReturnsStoredGraph(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-a",
		Goal:   "approval graph",
		Status: workflow.StatusWaitingApproval,
		Nodes: []workflow.Node{{
			ID:     "approval",
			Type:   workflow.NodeApproval,
			Status: workflow.StatusWaitingApproval,
			Security: workflow.SecurityEnvelope{
				Operation: "write",
				Approval:  workflow.ApprovalRequired,
				Risk:      "workspace-write",
			},
		}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.get",
		Params:  json.RawMessage(`{"id":"wf-a"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Workflow workflow.Workflow `json:"workflow"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.get response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.get error = %+v", resp.Error)
	}
	if resp.Result.Workflow.ID != "wf-a" || resp.Result.Workflow.Nodes[0].Security.Approval != workflow.ApprovalRequired {
		t.Fatalf("workflow = %#v, want stored graph", resp.Result.Workflow)
	}
}

func TestWorkflowGetRPCRejectsMissingID(t *testing.T) {
	srv := &Server{spans: observability.NewRing(10), workflowStore: workflow.NewFileStore(t.TempDir())}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.get",
		Params:  json.RawMessage(`{}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.get response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("workflow.get error = %+v, want invalid params", resp.Error)
	}
}

func TestWorkflowReadyRPCReturnsQueuedNodesWithCompletedDependencies(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-ready",
		Status: workflow.StatusRunning,
		Nodes: []workflow.Node{
			{ID: "context", Status: workflow.StatusCompleted},
			{ID: "edit", Type: workflow.NodeToolCall, Status: workflow.StatusQueued},
			{ID: "test", Type: workflow.NodeVerification, Status: workflow.StatusQueued},
		},
		Edges: []workflow.Edge{
			{From: "context", To: "edit"},
			{From: "edit", To: "test"},
		},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.ready",
		Params:  json.RawMessage(`{"id":"wf-ready"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Nodes []workflow.Node `json:"nodes"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.ready response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.ready error = %+v", resp.Error)
	}
	if len(resp.Result.Nodes) != 1 || resp.Result.Nodes[0].ID != "edit" {
		t.Fatalf("ready nodes = %#v, want edit", resp.Result.Nodes)
	}
}

func TestWorkflowCancelRPCCancelsAndPersistsWorkflow(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-cancel",
		Status: workflow.StatusRunning,
		Nodes: []workflow.Node{
			{ID: "edit", Type: workflow.NodeToolCall, Status: workflow.StatusRunning},
			{ID: "verify", Type: workflow.NodeVerification, Status: workflow.StatusQueued},
		},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.cancel",
		Params:  json.RawMessage(`{"id":" wf-cancel ","reason":" user stopped it "}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Workflow workflow.Workflow `json:"workflow"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.cancel response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.cancel error = %+v", resp.Error)
	}
	if resp.Result.Workflow.ID != "wf-cancel" || resp.Result.Workflow.Status != workflow.StatusCanceled {
		t.Fatalf("workflow = %#v, want canceled wf-cancel", resp.Result.Workflow)
	}

	stored, err := store.Load(context.Background(), "wf-cancel")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Status != workflow.StatusCanceled {
		t.Fatalf("stored workflow status = %q, want canceled", stored.Status)
	}
	if stored.Nodes[0].Status != workflow.StatusCanceled || stored.Nodes[1].Status != workflow.StatusCanceled {
		t.Fatalf("stored node statuses = %q, %q, want canceled", stored.Nodes[0].Status, stored.Nodes[1].Status)
	}
	if stored.Nodes[0].Error != "user stopped it" || stored.Nodes[1].Error != "user stopped it" {
		t.Fatalf("stored cancellation reasons = %q, %q, want trimmed reason", stored.Nodes[0].Error, stored.Nodes[1].Error)
	}
	if stored.UpdatedAt.IsZero() {
		t.Fatalf("stored updated_at is zero, want cancellation timestamp")
	}
}

func TestWorkflowCancelRPCRejectsMissingID(t *testing.T) {
	srv := &Server{spans: observability.NewRing(10), workflowStore: workflow.NewFileStore(t.TempDir())}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.cancel",
		Params:  json.RawMessage(`{"id":"   ","reason":"not used"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.cancel response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("workflow.cancel error = %+v, want invalid params", resp.Error)
	}
}

func TestWorkflowNodeStartRPCStartsAndPersistsNode(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-start",
		Status: workflow.StatusQueued,
		Nodes: []workflow.Node{
			{ID: "context", Status: workflow.StatusCompleted},
			{ID: "edit", Type: workflow.NodeToolCall, Status: workflow.StatusQueued},
		},
		Edges: []workflow.Edge{{From: "context", To: "edit"}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.start",
		Params:  json.RawMessage(`{"id":"wf-start","node_id":"edit"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Workflow   workflow.Workflow `json:"workflow"`
			Node       workflow.Node     `json:"node"`
			ReadyNodes []workflow.Node   `json:"ready_nodes"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.node.start response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.node.start error = %+v", resp.Error)
	}
	if resp.Result.Workflow.ID != "wf-start" || resp.Result.Node.ID != "edit" {
		t.Fatalf("result = %#v, want workflow wf-start and node edit", resp.Result)
	}
	if resp.Result.Workflow.Status != workflow.StatusRunning || resp.Result.Node.Status != workflow.StatusRunning {
		t.Fatalf("statuses = workflow %q node %q, want running", resp.Result.Workflow.Status, resp.Result.Node.Status)
	}

	stored, err := store.Load(context.Background(), "wf-start")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Status != workflow.StatusRunning || stored.Nodes[1].Status != workflow.StatusRunning {
		t.Fatalf("stored statuses = workflow %q node %q, want running", stored.Status, stored.Nodes[1].Status)
	}
}

func TestWorkflowNodeStartRPCRejectsMissingNodeID(t *testing.T) {
	srv := &Server{spans: observability.NewRing(10), workflowStore: workflow.NewFileStore(t.TempDir())}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.start",
		Params:  json.RawMessage(`{"id":"wf-start"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.node.start response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("workflow.node.start error = %+v, want invalid params", resp.Error)
	}
}

func TestWorkflowNodeStartRPCReportsTransitionErrorsAsInvalidParams(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-blocked",
		Status: workflow.StatusQueued,
		Nodes: []workflow.Node{
			{ID: "context", Status: workflow.StatusQueued},
			{ID: "edit", Type: workflow.NodeToolCall, Status: workflow.StatusQueued},
		},
		Edges: []workflow.Edge{{From: "context", To: "edit"}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.start",
		Params:  json.RawMessage(`{"id":"wf-blocked","node_id":"edit"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.node.start response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("workflow.node.start error = %+v, want invalid params", resp.Error)
	}

	stored, err := store.Load(context.Background(), "wf-blocked")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Status != workflow.StatusQueued || stored.Nodes[1].Status != workflow.StatusQueued {
		t.Fatalf("stored statuses = workflow %q node %q, want queued", stored.Status, stored.Nodes[1].Status)
	}
}

func TestWorkflowNodeCompleteRPCCompletesAndPersistsNode(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-complete",
		Status: workflow.StatusRunning,
		Nodes: []workflow.Node{
			{ID: "edit", Type: workflow.NodeToolCall, Status: workflow.StatusRunning},
			{ID: "verify", Type: workflow.NodeVerification, Status: workflow.StatusQueued},
		},
		Edges: []workflow.Edge{{From: "edit", To: "verify"}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.complete",
		Params:  json.RawMessage(`{"id":"wf-complete","node_id":"edit","outputs":{"summary":"done"}}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Workflow   workflow.Workflow `json:"workflow"`
			Node       workflow.Node     `json:"node"`
			ReadyNodes []workflow.Node   `json:"ready_nodes"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.node.complete response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.node.complete error = %+v", resp.Error)
	}
	if resp.Result.Node.Status != workflow.StatusCompleted || resp.Result.Node.Outputs["summary"] != "done" {
		t.Fatalf("node = %#v, want completed with summary output", resp.Result.Node)
	}
	if len(resp.Result.ReadyNodes) != 1 || resp.Result.ReadyNodes[0].ID != "verify" {
		t.Fatalf("ready_nodes = %#v, want verify unlocked in complete response", resp.Result.ReadyNodes)
	}

	stored, err := store.Load(context.Background(), "wf-complete")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Nodes[0].Status != workflow.StatusCompleted || stored.Nodes[0].Outputs["summary"] != "done" {
		t.Fatalf("stored node = %#v, want completed with summary output", stored.Nodes[0])
	}
	ready, err := workflow.ReadyNodes(stored)
	if err != nil {
		t.Fatalf("ReadyNodes: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "verify" {
		t.Fatalf("ready nodes = %#v, want verify unlocked", ready)
	}
}

func TestWorkflowNodeCompleteRPCMarksWorkflowCompletedWhenNoWorkRemains(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-final",
		Status: workflow.StatusRunning,
		Nodes: []workflow.Node{
			{ID: "context", Type: workflow.NodeContext, Status: workflow.StatusCompleted},
			{ID: "summary", Type: workflow.NodeSummary, Status: workflow.StatusRunning},
		},
		Edges: []workflow.Edge{{From: "context", To: "summary"}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.complete",
		Params:  json.RawMessage(`{"id":"wf-final","node_id":"summary"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Workflow   workflow.Workflow `json:"workflow"`
			Node       workflow.Node     `json:"node"`
			ReadyNodes []workflow.Node   `json:"ready_nodes"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.node.complete response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.node.complete error = %+v", resp.Error)
	}
	if resp.Result.Workflow.Status != workflow.StatusCompleted || resp.Result.Node.Status != workflow.StatusCompleted {
		t.Fatalf("result statuses = workflow %q node %q, want completed", resp.Result.Workflow.Status, resp.Result.Node.Status)
	}
	if len(resp.Result.ReadyNodes) != 0 {
		t.Fatalf("ready_nodes = %#v, want none", resp.Result.ReadyNodes)
	}

	stored, err := store.Load(context.Background(), "wf-final")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Status != workflow.StatusCompleted {
		t.Fatalf("stored workflow status = %q, want completed", stored.Status)
	}
}

func TestWorkflowNodeFailRPCFailsAndPersistsNode(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-fail",
		Status: workflow.StatusRunning,
		Nodes:  []workflow.Node{{ID: "test", Type: workflow.NodeVerification, Status: workflow.StatusRunning}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.fail",
		Params:  json.RawMessage(`{"id":"wf-fail","node_id":"test","error":"go test failed"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Workflow workflow.Workflow `json:"workflow"`
			Node     workflow.Node     `json:"node"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.node.fail response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.node.fail error = %+v", resp.Error)
	}
	if resp.Result.Workflow.Status != workflow.StatusFailed || resp.Result.Node.Status != workflow.StatusFailed || resp.Result.Node.Error != "go test failed" {
		t.Fatalf("result = %#v, want failed workflow and node with error", resp.Result)
	}

	stored, err := store.Load(context.Background(), "wf-fail")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Status != workflow.StatusFailed || stored.Nodes[0].Status != workflow.StatusFailed || stored.Nodes[0].Error != "go test failed" {
		t.Fatalf("stored = %#v, want failed workflow and node with error", stored)
	}
}

func TestWorkflowNodeRetryRPCRetriesAndPersistsNode(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-retry",
		Status: workflow.StatusFailed,
		Nodes: []workflow.Node{{
			ID:         "test",
			Type:       workflow.NodeVerification,
			Status:     workflow.StatusFailed,
			Error:      "go test failed",
			RetryCount: 2,
		}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.retry",
		Params:  json.RawMessage(`{"id":" wf-retry ","node_id":" test "}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Workflow workflow.Workflow `json:"workflow"`
			Node     workflow.Node     `json:"node"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.node.retry response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.node.retry error = %+v", resp.Error)
	}
	if resp.Result.Workflow.ID != "wf-retry" || resp.Result.Node.ID != "test" {
		t.Fatalf("result = %#v, want workflow wf-retry and node test", resp.Result)
	}
	if resp.Result.Node.Status != workflow.StatusQueued || resp.Result.Node.RetryCount != 3 || resp.Result.Node.Error != "" {
		t.Fatalf("node = %#v, want queued with retry_count 3 and cleared error", resp.Result.Node)
	}

	stored, err := store.Load(context.Background(), "wf-retry")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Nodes[0].Status != workflow.StatusQueued || stored.Nodes[0].RetryCount != 3 || stored.Nodes[0].Error != "" {
		t.Fatalf("stored node = %#v, want queued with retry_count 3 and cleared error", stored.Nodes[0])
	}
	if stored.UpdatedAt.IsZero() {
		t.Fatalf("stored updated_at is zero, want retry timestamp")
	}
}

func TestWorkflowNodeRetryRPCRejectsMissingNodeID(t *testing.T) {
	srv := &Server{spans: observability.NewRing(10), workflowStore: workflow.NewFileStore(t.TempDir())}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.retry",
		Params:  json.RawMessage(`{"id":"wf-retry"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.node.retry response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("workflow.node.retry error = %+v, want invalid params", resp.Error)
	}
}

func TestWorkflowNodeRetryRPCReportsTransitionErrorsAsInvalidParams(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-retry-invalid",
		Status: workflow.StatusRunning,
		Nodes:  []workflow.Node{{ID: "test", Type: workflow.NodeVerification, Status: workflow.StatusQueued}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.retry",
		Params:  json.RawMessage(`{"id":"wf-retry-invalid","node_id":"test"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.node.retry response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("workflow.node.retry error = %+v, want invalid params", resp.Error)
	}

	stored, err := store.Load(context.Background(), "wf-retry-invalid")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Nodes[0].Status != workflow.StatusQueued {
		t.Fatalf("stored node status = %q, want queued", stored.Nodes[0].Status)
	}
}

func TestWorkflowNodeCompleteRPCRejectsMissingNodeID(t *testing.T) {
	srv := &Server{spans: observability.NewRing(10), workflowStore: workflow.NewFileStore(t.TempDir())}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.complete",
		Params:  json.RawMessage(`{"id":"wf-complete"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.node.complete response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("workflow.node.complete error = %+v, want invalid params", resp.Error)
	}
}
