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
