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

func TestRunWorkflowRejectsMissingShowID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runWorkflow([]string{"show"}, &stdout, &stderr, "unused.sock"); rc != 2 {
		t.Fatalf("runWorkflow show without id rc=%d stdout=%s stderr=%s", rc, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "workflow show requires <id>") {
		t.Fatalf("stderr missing show id message:\n%s", stderr.String())
	}
}
