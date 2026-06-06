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
	"errors"
	"testing"
	"time"

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

func TestWorkflowTemplatesRPCReturnsBuiltIns(t *testing.T) {
	srv := &Server{spans: observability.NewRing(10), workflowStore: workflow.NewFileStore(t.TempDir())}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.templates",
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Templates []workflow.TemplateSummary `json:"templates"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.templates response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.templates error = %+v", resp.Error)
	}
	if len(resp.Result.Templates) == 0 || resp.Result.Templates[0].Name == "" {
		t.Fatalf("templates = %#v, want built-ins", resp.Result.Templates)
	}
}

func TestWorkflowCreateRPCCreatesAndPersistsTemplateGraph(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.create",
		Params:  json.RawMessage(`{"template":"tdd-bug-fix","id":"wf-created","goal":"fix bug"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Workflow workflow.Workflow `json:"workflow"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.create response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.create error = %+v", resp.Error)
	}
	if resp.Result.Workflow.ID != "wf-created" || resp.Result.Workflow.Goal != "fix bug" || len(resp.Result.Workflow.Nodes) == 0 {
		t.Fatalf("workflow = %#v, want created graph", resp.Result.Workflow)
	}
	stored, err := store.Load(context.Background(), "wf-created")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Nodes[0].Inputs["template"] != "tdd-bug-fix" {
		t.Fatalf("stored node inputs = %#v, want template marker", stored.Nodes[0].Inputs)
	}
}

func TestWorkflowCreateRPCRejectsUnknownTemplate(t *testing.T) {
	srv := &Server{spans: observability.NewRing(10), workflowStore: workflow.NewFileStore(t.TempDir())}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.create",
		Params:  json.RawMessage(`{"template":"unknown","id":"wf-created"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.create response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("workflow.create error = %+v, want invalid params", resp.Error)
	}
}

func TestWorkflowGetRPCReturnsStoredGraph(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	loggedAt := time.Date(2026, 5, 19, 10, 30, 0, 0, time.UTC)
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
			ToolCalls: []workflow.ToolCall{{
				Tool:   "write_file",
				Args:   map[string]string{"path": "nextgen.md"},
				Result: "patched",
			}},
			Logs: []workflow.LogRecord{{
				Time:    loggedAt,
				Level:   "info",
				Message: "waiting for write approval",
			}},
			Mutations: []workflow.FileMutation{{
				Op:    "write",
				Path:  "nextgen.md",
				Lines: 12,
			}},
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
	node := resp.Result.Workflow.Nodes[0]
	if len(node.ToolCalls) != 1 || node.ToolCalls[0].Tool != "write_file" || node.ToolCalls[0].Args["path"] != "nextgen.md" || node.ToolCalls[0].Result != "patched" {
		t.Fatalf("tool_calls = %#v, want stored write_file call", node.ToolCalls)
	}
	if len(node.Logs) != 1 || !node.Logs[0].Time.Equal(loggedAt) || node.Logs[0].Level != "info" || node.Logs[0].Message != "waiting for write approval" {
		t.Fatalf("logs = %#v, want stored log record", node.Logs)
	}
	if len(node.Mutations) != 1 || node.Mutations[0].Op != "write" || node.Mutations[0].Path != "nextgen.md" || node.Mutations[0].Lines != 12 {
		t.Fatalf("mutations = %#v, want stored file mutation", node.Mutations)
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

func TestWorkflowExportRPCReturnsStoredGraph(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-export",
		Goal:   "move graph",
		Status: workflow.StatusQueued,
		Nodes:  []workflow.Node{{ID: "context", Status: workflow.StatusQueued}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.export",
		Params:  json.RawMessage(`{"id":"wf-export"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Workflow workflow.Workflow `json:"workflow"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.export response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.export error = %+v", resp.Error)
	}
	if resp.Result.Workflow.ID != "wf-export" || resp.Result.Workflow.Goal != "move graph" {
		t.Fatalf("workflow = %#v, want exported graph", resp.Result.Workflow)
	}
}

func TestWorkflowImportRPCValidatesAndPersistsGraph(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.import",
		Params:  json.RawMessage(`{"workflow":{"id":"wf-import","goal":"resume elsewhere","status":"queued","nodes":[{"id":"context","status":"queued"}]}}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Workflow workflow.Workflow `json:"workflow"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.import response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.import error = %+v", resp.Error)
	}
	if resp.Result.Workflow.ID != "wf-import" || resp.Result.Workflow.Goal != "resume elsewhere" {
		t.Fatalf("workflow = %#v, want imported graph", resp.Result.Workflow)
	}
	stored, err := store.Load(context.Background(), "wf-import")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.ID != "wf-import" || len(stored.Nodes) != 1 {
		t.Fatalf("stored workflow = %#v, want imported graph", stored)
	}
}

func TestWorkflowImportRPCRejectsInvalidGraph(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.import",
		Params:  json.RawMessage(`{"workflow":{"id":"wf-bad","status":"queued","nodes":[{"id":"context","status":"queued"}],"edges":[{"from":"missing","to":"context"}]}}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.import response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("workflow.import error = %+v, want invalid params", resp.Error)
	}
	if _, err := store.Load(context.Background(), "wf-bad"); err == nil {
		t.Fatalf("Load wf-bad succeeded, want no persisted invalid graph")
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

func TestWorkflowNodeDelegateRPCStartsBackgroundDelegateAndCompletesNode(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-delegate",
		Status: workflow.StatusQueued,
		Nodes: []workflow.Node{{
			ID:     "implement",
			Type:   workflow.NodeAgent,
			Status: workflow.StatusQueued,
			Agent:  "codex",
			Inputs: map[string]string{
				"dir":    "/repo",
				"prompt": "implement import/export",
			},
		}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	gotArgs := make(chan []string, 1)
	srv := &Server{
		spans:         observability.NewRing(10),
		workflowStore: store,
		workflowDelegateRunner: func(_ context.Context, agent, dir, prompt string) (string, error) {
			gotArgs <- []string{agent, dir, prompt}
			return "delegate done", nil
		},
	}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.delegate",
		Params:  json.RawMessage(`{"id":"wf-delegate","node_id":"implement"}`),
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
		t.Fatalf("decode workflow.node.delegate response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.node.delegate error = %+v", resp.Error)
	}
	if resp.Result.Node.Status != workflow.StatusRunning || resp.Result.Workflow.Status != workflow.StatusRunning {
		t.Fatalf("response workflow/node = %#v/%#v, want running", resp.Result.Workflow, resp.Result.Node)
	}
	srv.bgWG.Wait()
	args := <-gotArgs
	if args[0] != "codex" || args[1] != "/repo" || args[2] != "implement import/export" {
		t.Fatalf("delegate args = %#v", args)
	}

	stored, err := store.Load(context.Background(), "wf-delegate")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Status != workflow.StatusCompleted || stored.Nodes[0].Status != workflow.StatusCompleted {
		t.Fatalf("stored workflow/node = %#v/%#v, want completed", stored, stored.Nodes[0])
	}
	if stored.Nodes[0].Outputs["delegate_output"] != "delegate done" {
		t.Fatalf("delegate_output = %q, want delegate done", stored.Nodes[0].Outputs["delegate_output"])
	}
}

func TestWorkflowNodeDelegateRPCPersistsDelegateFailure(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-delegate-fail",
		Status: workflow.StatusQueued,
		Nodes: []workflow.Node{{
			ID:     "implement",
			Type:   workflow.NodeAgent,
			Status: workflow.StatusQueued,
			Agent:  "codex",
			Inputs: map[string]string{"prompt": "implement"},
		}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{
		spans:         observability.NewRing(10),
		workflowStore: store,
		workflowDelegateRunner: func(context.Context, string, string, string) (string, error) {
			return "", errors.New("delegate failed")
		},
	}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.delegate",
		Params:  json.RawMessage(`{"id":"wf-delegate-fail","node_id":"implement"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode workflow.node.delegate response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("workflow.node.delegate error = %+v", resp.Error)
	}
	srv.bgWG.Wait()

	stored, err := store.Load(context.Background(), "wf-delegate-fail")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Status != workflow.StatusFailed || stored.Nodes[0].Status != workflow.StatusFailed {
		t.Fatalf("stored workflow/node = %#v/%#v, want failed", stored, stored.Nodes[0])
	}
	if stored.Nodes[0].Error != "delegate failed" {
		t.Fatalf("node error = %q, want delegate failed", stored.Nodes[0].Error)
	}
}

func TestWorkflowNodeDelegateRPCRejectsMissingAgentOrPrompt(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-delegate-invalid",
		Status: workflow.StatusQueued,
		Nodes:  []workflow.Node{{ID: "implement", Status: workflow.StatusQueued}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	srv := &Server{spans: observability.NewRing(10), workflowStore: store}

	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "workflow.node.delegate",
		Params:  json.RawMessage(`{"id":"wf-delegate-invalid","node_id":"implement"}`),
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

func TestWorkflowNodeCompleteRPCCompletesAndPersistsNode(t *testing.T) {
	store := workflow.NewFileStore(t.TempDir())
	loggedAt := time.Date(2026, 5, 19, 11, 0, 0, 0, time.UTC)
	if err := store.Save(context.Background(), workflow.Workflow{
		ID:     "wf-complete",
		Status: workflow.StatusRunning,
		Nodes: []workflow.Node{
			{
				ID:      "edit",
				Type:    workflow.NodeToolCall,
				Status:  workflow.StatusRunning,
				Outputs: map[string]string{"draft": "kept"},
				Artifacts: []workflow.Artifact{{
					Kind: workflow.ArtifactLog,
					Path: "logs/edit.log",
				}},
				ToolCalls: []workflow.ToolCall{{
					Tool:     "edit_file",
					Args:     map[string]string{"path": "internal/daemon/workflow_methods_test.go"},
					Duration: "120ms",
				}},
				Logs: []workflow.LogRecord{{
					Time:    loggedAt,
					Level:   "info",
					Message: "applied patch",
				}},
				Mutations: []workflow.FileMutation{{
					Op:    "edit",
					Path:  "internal/daemon/workflow_methods_test.go",
					Lines: 8,
				}},
			},
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
		Params:  json.RawMessage(`{"id":"wf-complete","node_id":"edit","outputs":{"summary":"done"},"artifacts":[{"kind":"diff","path":"internal/daemon/workflow_methods_test.go"}]}`),
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
	if resp.Result.Node.Status != workflow.StatusCompleted || resp.Result.Node.Outputs["summary"] != "done" || resp.Result.Node.Outputs["draft"] != "kept" {
		t.Fatalf("node = %#v, want completed with merged outputs", resp.Result.Node)
	}
	if len(resp.Result.Node.Artifacts) != 2 || resp.Result.Node.Artifacts[0].Path != "logs/edit.log" || resp.Result.Node.Artifacts[1].Kind != workflow.ArtifactDiff {
		t.Fatalf("artifacts = %#v, want pre-existing log and appended diff", resp.Result.Node.Artifacts)
	}
	if len(resp.Result.Node.ToolCalls) != 1 || resp.Result.Node.ToolCalls[0].Tool != "edit_file" || resp.Result.Node.ToolCalls[0].Args["path"] != "internal/daemon/workflow_methods_test.go" {
		t.Fatalf("tool_calls = %#v, want pre-existing tool call preserved", resp.Result.Node.ToolCalls)
	}
	if len(resp.Result.Node.Logs) != 1 || !resp.Result.Node.Logs[0].Time.Equal(loggedAt) || resp.Result.Node.Logs[0].Message != "applied patch" {
		t.Fatalf("logs = %#v, want pre-existing log preserved", resp.Result.Node.Logs)
	}
	if len(resp.Result.Node.Mutations) != 1 || resp.Result.Node.Mutations[0].Path != "internal/daemon/workflow_methods_test.go" || resp.Result.Node.Mutations[0].Lines != 8 {
		t.Fatalf("mutations = %#v, want pre-existing mutation preserved", resp.Result.Node.Mutations)
	}
	if len(resp.Result.ReadyNodes) != 1 || resp.Result.ReadyNodes[0].ID != "verify" {
		t.Fatalf("ready_nodes = %#v, want verify unlocked in complete response", resp.Result.ReadyNodes)
	}

	stored, err := store.Load(context.Background(), "wf-complete")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Nodes[0].Status != workflow.StatusCompleted || stored.Nodes[0].Outputs["summary"] != "done" || stored.Nodes[0].Outputs["draft"] != "kept" {
		t.Fatalf("stored node = %#v, want completed with merged outputs", stored.Nodes[0])
	}
	if len(stored.Nodes[0].Artifacts) != 2 || stored.Nodes[0].Artifacts[0].Path != "logs/edit.log" || stored.Nodes[0].Artifacts[1].Kind != workflow.ArtifactDiff {
		t.Fatalf("stored artifacts = %#v, want pre-existing log and appended diff", stored.Nodes[0].Artifacts)
	}
	if len(stored.Nodes[0].ToolCalls) != 1 || stored.Nodes[0].ToolCalls[0].Tool != "edit_file" {
		t.Fatalf("stored tool_calls = %#v, want pre-existing tool call preserved", stored.Nodes[0].ToolCalls)
	}
	if len(stored.Nodes[0].Logs) != 1 || !stored.Nodes[0].Logs[0].Time.Equal(loggedAt) {
		t.Fatalf("stored logs = %#v, want pre-existing log preserved", stored.Nodes[0].Logs)
	}
	if len(stored.Nodes[0].Mutations) != 1 || stored.Nodes[0].Mutations[0].Path != "internal/daemon/workflow_methods_test.go" {
		t.Fatalf("stored mutations = %#v, want pre-existing mutation preserved", stored.Nodes[0].Mutations)
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
