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

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunWorkflowListRendersSummaries(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"workflow.list": map[string]any{
			"workflows": []any{
				map[string]any{"id": "wf-a", "goal": "ship graph", "status": "queued", "nodes": 3},
			},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"list"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow list rc=%d stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if call.Method != "workflow.list" {
		t.Fatalf("method = %q, want workflow.list", call.Method)
	}
	for _, want := range []string{"ID", "STATUS", "NODES", "GOAL", "wf-a", "queued", "3", "ship graph"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workflow list output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkflowShowCallsGetWithID(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"workflow.get": map[string]any{
			"workflow": map[string]any{
				"id":     "wf-a",
				"goal":   "approval graph",
				"status": "waiting_approval",
				"nodes": []any{
					map[string]any{"id": "approval", "type": "approval", "status": "waiting_approval"},
				},
			},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"show", "wf-a"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow show rc=%d stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if call.Method != "workflow.get" || call.Params["id"] != "wf-a" {
		t.Fatalf("call = %#v, want workflow.get id wf-a", call)
	}
	for _, want := range []string{"workflow wf-a", "status: waiting_approval", "goal: approval graph", "nodes: 1", "approval"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workflow show output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkflowListJSONRendersRawShape(t *testing.T) {
	sock, _ := startSecurityRPCTestServer(t, map[string]any{
		"workflow.list": map[string]any{"workflows": []any{}},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"list", "--json"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow list --json rc=%d stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"workflows"`) {
		t.Fatalf("json output missing workflows:\n%s", stdout.String())
	}
}

func TestRunWorkflowReadyRendersReadyNodes(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"workflow.ready": map[string]any{
			"nodes": []any{
				map[string]any{"id": "edit", "type": "tool_call", "status": "queued", "client": "codex"},
			},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"ready", "wf-a"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow ready rc=%d stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if call.Method != "workflow.ready" || call.Params["id"] != "wf-a" {
		t.Fatalf("call = %#v, want workflow.ready id wf-a", call)
	}
	for _, want := range []string{"READY", "edit", "tool_call", "queued", "codex"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workflow ready output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkflowStartCallsNodeStartAndRendersNode(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"workflow.node.start": map[string]any{
			"node": map[string]any{"id": "edit", "type": "tool_call", "status": "running", "client": "codex"},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"start", "wf-a", "edit"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow start rc=%d stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if call.Method != "workflow.node.start" || call.Params["id"] != "wf-a" || call.Params["node_id"] != "edit" {
		t.Fatalf("call = %#v, want workflow.node.start id wf-a node_id edit", call)
	}
	for _, want := range []string{"started", "edit", "running", "tool_call", "codex"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workflow start output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkflowStartJSONRendersRawShape(t *testing.T) {
	sock, _ := startSecurityRPCTestServer(t, map[string]any{
		"workflow.node.start": map[string]any{
			"node": map[string]any{"id": "edit", "type": "tool_call", "status": "running", "client": "codex"},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"start", "wf-a", "edit", "--json"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow start --json rc=%d stderr=%s", rc, stderr.String())
	}
	for _, want := range []string{`"node"`, `"id": "edit"`, `"status": "running"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workflow start json output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkflowRetryCallsNodeRetryAndRendersNode(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"workflow.node.retry": map[string]any{
			"node": map[string]any{"id": "test", "status": "queued", "retry": 2},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"retry", "wf-a", "test"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow retry rc=%d stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if call.Method != "workflow.node.retry" || call.Params["id"] != "wf-a" || call.Params["node_id"] != "test" {
		t.Fatalf("call = %#v, want workflow.node.retry id wf-a node_id test", call)
	}
	want := "retried test status=queued retry=2"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("workflow retry output missing %q:\n%s", want, stdout.String())
	}
}

func TestRunWorkflowRetryJSONRendersRawShape(t *testing.T) {
	sock, _ := startSecurityRPCTestServer(t, map[string]any{
		"workflow.node.retry": map[string]any{
			"node": map[string]any{"id": "test", "status": "queued", "retry": 2},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"retry", "wf-a", "test", "--json"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow retry --json rc=%d stderr=%s", rc, stderr.String())
	}
	for _, want := range []string{`"node"`, `"id": "test"`, `"status": "queued"`, `"retry": 2`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workflow retry json output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkflowCompleteCallsNodeCompleteAndRendersNode(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"workflow.node.complete": map[string]any{
			"node":        map[string]any{"id": "edit", "type": "tool_call", "status": "completed", "client": "codex"},
			"ready_nodes": []any{map[string]any{"id": "verify", "type": "verification", "status": "queued", "client": "codex"}},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"complete", "wf-a", "edit"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow complete rc=%d stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if call.Method != "workflow.node.complete" || call.Params["id"] != "wf-a" || call.Params["node_id"] != "edit" {
		t.Fatalf("call = %#v, want workflow.node.complete id wf-a node_id edit", call)
	}
	for _, want := range []string{"completed", "edit", "tool_call", "codex", "next=verify"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workflow complete output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkflowFailCallsNodeFailAndRendersNode(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"workflow.node.fail": map[string]any{
			"node": map[string]any{"id": "test", "type": "verification", "status": "failed", "client": "codex", "error": "go test failed"},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"fail", "wf-a", "test", "--error", "go test failed"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow fail rc=%d stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if call.Method != "workflow.node.fail" || call.Params["id"] != "wf-a" || call.Params["node_id"] != "test" || call.Params["error"] != "go test failed" {
		t.Fatalf("call = %#v, want workflow.node.fail id wf-a node_id test error", call)
	}
	for _, want := range []string{"failed", "test", "verification", "go test failed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workflow fail output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkflowCancelCallsCancelWithReasonAndRendersWorkflow(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"workflow.cancel": map[string]any{
			"workflow": map[string]any{
				"id":     "wf-a",
				"status": "canceled",
				"nodes": []any{
					map[string]any{"id": "edit", "status": "canceled"},
					map[string]any{"id": "test", "status": "canceled"},
				},
			},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"cancel", " wf-a ", "--reason", "  user requested  "}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow cancel rc=%d stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if call.Method != "workflow.cancel" || call.Params["id"] != "wf-a" || call.Params["reason"] != "user requested" {
		t.Fatalf("call = %#v, want workflow.cancel id wf-a reason", call)
	}
	want := "canceled wf-a status=canceled nodes=2"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("workflow cancel output missing %q:\n%s", want, stdout.String())
	}
}

func TestRunWorkflowCancelJSONRendersRawShapeWithoutReason(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"workflow.cancel": map[string]any{
			"workflow": map[string]any{
				"id":     "wf-a",
				"status": "canceled",
				"nodes":  []any{},
			},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"cancel", "wf-a", "--json"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow cancel --json rc=%d stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if call.Method != "workflow.cancel" || call.Params["id"] != "wf-a" {
		t.Fatalf("call = %#v, want workflow.cancel id wf-a", call)
	}
	if _, ok := call.Params["reason"]; ok {
		t.Fatalf("call params included empty reason: %#v", call.Params)
	}
	for _, want := range []string{`"workflow"`, `"id": "wf-a"`, `"status": "canceled"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workflow cancel json output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkflowCompleteJSONRendersRawShape(t *testing.T) {
	sock, _ := startSecurityRPCTestServer(t, map[string]any{
		"workflow.node.complete": map[string]any{
			"node": map[string]any{"id": "edit", "type": "tool_call", "status": "completed", "client": "codex"},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"complete", "wf-a", "edit", "--json"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runWorkflow complete --json rc=%d stderr=%s", rc, stderr.String())
	}
	for _, want := range []string{`"node"`, `"id": "edit"`, `"status": "completed"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workflow complete json output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkflowRejectsMissingStartIDs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"start", "wf-a"}, &stdout, &stderr, "unused.sock"); rc != 2 {
		t.Fatalf("runWorkflow start without node id rc=%d stdout=%s stderr=%s", rc, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "workflow start requires <workflow-id> <node-id>") {
		t.Fatalf("stderr missing start id message:\n%s", stderr.String())
	}
}

func TestRunWorkflowRejectsMissingRetryIDs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"retry", "wf-a"}, &stdout, &stderr, "unused.sock"); rc != 2 {
		t.Fatalf("runWorkflow retry without node id rc=%d stdout=%s stderr=%s", rc, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "workflow retry requires <workflow-id> <node-id>") {
		t.Fatalf("stderr missing retry id message:\n%s", stderr.String())
	}
}

func TestRunWorkflowRejectsMissingCompleteIDs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"complete", "wf-a"}, &stdout, &stderr, "unused.sock"); rc != 2 {
		t.Fatalf("runWorkflow complete without node id rc=%d stdout=%s stderr=%s", rc, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "workflow complete requires <workflow-id> <node-id>") {
		t.Fatalf("stderr missing complete id message:\n%s", stderr.String())
	}
}

func TestRunWorkflowRejectsMissingFailError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"fail", "wf-a", "test"}, &stdout, &stderr, "unused.sock"); rc != 2 {
		t.Fatalf("runWorkflow fail without error rc=%d stdout=%s stderr=%s", rc, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "workflow fail requires --error") {
		t.Fatalf("stderr missing fail error message:\n%s", stderr.String())
	}
}

func TestRunWorkflowRejectsMissingShowID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"show"}, &stdout, &stderr, "unused.sock"); rc != 2 {
		t.Fatalf("runWorkflow show without id rc=%d stdout=%s stderr=%s", rc, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "workflow show requires <id>") {
		t.Fatalf("stderr missing show id message:\n%s", stderr.String())
	}
}
