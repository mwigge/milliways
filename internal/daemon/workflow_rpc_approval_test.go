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

func TestWorkflowNodeWaitApprovalRPCMovesNodeToWaitingApproval(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-wait",
		Status: workflow.StatusRunning,
		Nodes: []workflow.Node{{
			ID:     "edit",
			Type:   workflow.NodeToolCall,
			Status: workflow.StatusRunning,
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
		Method:  "workflow.node.wait_approval",
		Params:  json.RawMessage(`{"id":"wf-wait","node_id":"edit","reason":"write nextgen.md"}`),
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
		t.Fatalf("decode workflow.node.wait_approval response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.node.wait_approval error = %+v", resp.Error)
	}
	if resp.Result.Node.Status != workflow.StatusWaitingApproval {
		t.Fatalf("node status = %q, want waiting_approval", resp.Result.Node.Status)
	}
	if resp.Result.Node.Error != "write nextgen.md" {
		t.Fatalf("node error = %q, want approval reason", resp.Result.Node.Error)
	}

	stored, err := store.Load(context.Background(), "wf-wait")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Nodes[0].Status != workflow.StatusWaitingApproval {
		t.Fatalf("stored node status = %q, want waiting_approval", stored.Nodes[0].Status)
	}
}

func TestWorkflowNodeWaitApprovalRPCRejectsNonRunningNode(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-wait-invalid",
		Status: workflow.StatusRunning,
		Nodes:  []workflow.Node{{ID: "edit", Status: workflow.StatusQueued}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.wait_approval",
		Params:  json.RawMessage(`{"id":"wf-wait-invalid","node_id":"edit"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("error = %+v, want invalid params", resp.Error)
	}
}

func TestWorkflowNodeResumeRPCMovesNodeToResumed(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-resume",
		Status: workflow.StatusRunning,
		Nodes: []workflow.Node{{
			ID:     "edit",
			Type:   workflow.NodeToolCall,
			Status: workflow.StatusWaitingApproval,
			Error:  "pending approval",
		}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.resume",
		Params:  json.RawMessage(`{"id":"wf-resume","node_id":"edit"}`),
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
		t.Fatalf("decode workflow.node.resume response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.node.resume error = %+v", resp.Error)
	}
	if resp.Result.Node.Status != workflow.StatusResumed {
		t.Fatalf("node status = %q, want resumed", resp.Result.Node.Status)
	}
	if resp.Result.Node.Error != "" {
		t.Fatalf("node error = %q, want cleared", resp.Result.Node.Error)
	}

	stored, err := store.Load(context.Background(), "wf-resume")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Nodes[0].Status != workflow.StatusResumed {
		t.Fatalf("stored node status = %q, want resumed", stored.Nodes[0].Status)
	}
}

func TestWorkflowNodeResumeRPCRejectsNonWaitingNode(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-resume-invalid",
		Status: workflow.StatusRunning,
		Nodes:  []workflow.Node{{ID: "edit", Status: workflow.StatusRunning}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.resume",
		Params:  json.RawMessage(`{"id":"wf-resume-invalid","node_id":"edit"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("error = %+v, want invalid params", resp.Error)
	}
}

func TestWorkflowNodeDenyRPCMovesNodeToFailed(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-deny",
		Status: workflow.StatusRunning,
		Nodes: []workflow.Node{{
			ID:     "edit",
			Type:   workflow.NodeToolCall,
			Status: workflow.StatusWaitingApproval,
			Security: workflow.SecurityEnvelope{
				Approval: workflow.ApprovalRequired,
			},
		}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.deny",
		Params:  json.RawMessage(`{"id":"wf-deny","node_id":"edit","reason":"user denied"}`),
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
		t.Fatalf("decode workflow.node.deny response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.node.deny error = %+v", resp.Error)
	}
	if resp.Result.Node.Status != workflow.StatusFailed {
		t.Fatalf("node status = %q, want failed", resp.Result.Node.Status)
	}
	if resp.Result.Node.Error != "user denied" {
		t.Fatalf("node error = %q, want denial reason", resp.Result.Node.Error)
	}
	if resp.Result.Node.Security.Approval != workflow.ApprovalDenied {
		t.Fatalf("security approval = %q, want denied", resp.Result.Node.Security.Approval)
	}

	stored, err := store.Load(context.Background(), "wf-deny")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Nodes[0].Status != workflow.StatusFailed {
		t.Fatalf("stored node status = %q, want failed", stored.Nodes[0].Status)
	}
}

func TestWorkflowNodeDenyRPCRejectsNonWaitingNode(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-deny-invalid",
		Status: workflow.StatusRunning,
		Nodes:  []workflow.Node{{ID: "edit", Status: workflow.StatusRunning}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.deny",
		Params:  json.RawMessage(`{"id":"wf-deny-invalid","node_id":"edit","reason":"no"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("error = %+v, want invalid params", resp.Error)
	}
}

func TestWorkflowNodeDenyRPCRequiresReason(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-deny-no-reason",
		Status: workflow.StatusRunning,
		Nodes:  []workflow.Node{{ID: "edit", Status: workflow.StatusWaitingApproval}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.deny",
		Params:  json.RawMessage(`{"id":"wf-deny-no-reason","node_id":"edit"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("error = %+v, want invalid params for missing reason", resp.Error)
	}
}
