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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/daemon/observability"
	"github.com/mwigge/milliways/internal/daemon/runners"
	"github.com/mwigge/milliways/internal/history"
	"github.com/mwigge/milliways/internal/pantry"
)

func TestPingReportsBuildVersion(t *testing.T) {
	oldVersion := Version
	Version = "v-test"
	t.Cleanup(func() { Version = oldVersion })

	var buf bytes.Buffer
	srv := &Server{spans: observability.NewRing(10)}
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "ping",
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result PingResult `json:"result"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode ping response: %v", err)
	}
	if resp.Result.Version != "v-test" {
		t.Fatalf("ping version = %q, want v-test", resp.Result.Version)
	}
}

func TestBuildStatusIncludesClientEnforcement(t *testing.T) {
	runners.SetBrokerPathProvider(nil)
	t.Cleanup(func() { runners.SetBrokerPathProvider(nil) })

	srv := &Server{spans: observability.NewRing(10)}
	status := srv.buildStatus()

	if got := status.ClientEnforcement["claude"].Level; got != runners.EnforcementPreflightOnly {
		t.Fatalf("claude enforcement = %q, want %q", got, runners.EnforcementPreflightOnly)
	}
	if got := status.ClientEnforcement["codex"].Level; got != runners.EnforcementPreflightOnly {
		t.Fatalf("codex enforcement = %q, want %q", got, runners.EnforcementPreflightOnly)
	}
	if got := status.ClientEnforcement["minimax"].Level; got != runners.EnforcementFull {
		t.Fatalf("minimax enforcement = %q, want %q", got, runners.EnforcementFull)
	}
	if got := status.ClientEnforcement["local"].Level; got != runners.EnforcementFull {
		t.Fatalf("local enforcement = %q, want %q", got, runners.EnforcementFull)
	}
}

func TestCapabilitiesGetReportsClientToolContracts(t *testing.T) {
	runners.SetBrokerPathProvider(nil)
	t.Cleanup(func() { runners.SetBrokerPathProvider(nil) })

	srv := &Server{spans: observability.NewRing(10)}
	var buf bytes.Buffer
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "capabilities.get",
		ID:      json.RawMessage(`1`),
	})

	var resp struct {
		Result struct {
			Clients map[string]runners.EnforcementMetadata `json:"clients"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode capabilities.get response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("capabilities.get error = %+v", resp.Error)
	}
	if got := resp.Result.Clients["minimax"].Capabilities.Contract.Write; got != runners.CapabilityRunnerControlled {
		t.Fatalf("minimax write capability = %q, want %q", got, runners.CapabilityRunnerControlled)
	}
	if got := resp.Result.Clients["codex"].Capabilities.Contract.Read; got != runners.CapabilityNative {
		t.Fatalf("codex read capability = %q, want %q without broker", got, runners.CapabilityNative)
	}
	if got := resp.Result.Clients["codex"].Capabilities.Contract.Bash; got != runners.CapabilityPreflightOnly {
		t.Fatalf("codex bash capability = %q, want %q without broker", got, runners.CapabilityPreflightOnly)
	}
	if got := resp.Result.Clients["codex"].Capabilities.Contract.Approvals; got != runners.CapabilityPreflightOnly {
		t.Fatalf("codex approval capability = %q, want %q without broker", got, runners.CapabilityPreflightOnly)
	}
	if got := resp.Result.Clients["codex"].Capabilities.Contract.Artifacts; got != runners.CapabilityUnsupported {
		t.Fatalf("codex artifact capability = %q, want %q without broker", got, runners.CapabilityUnsupported)
	}
}

func TestApprovalRPCStubsWireShape(t *testing.T) {
	srv := &Server{spans: observability.NewRing(10)}

	var listBuf bytes.Buffer
	srv.dispatch(json.NewEncoder(&listBuf), &Request{
		JSONRPC: "2.0",
		Method:  "approval.list",
		ID:      json.RawMessage(`1`),
	})
	var listResp struct {
		Result struct {
			Approvals []any  `json:"approvals"`
			Storage   string `json:"storage"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(listBuf.Bytes(), &listResp); err != nil {
		t.Fatalf("decode approval.list response: %v", err)
	}
	if listResp.Error != nil {
		t.Fatalf("approval.list error = %+v", listResp.Error)
	}
	if listResp.Result.Approvals == nil {
		t.Fatal("approval.list approvals is nil, want empty array")
	}
	if len(listResp.Result.Approvals) != 0 {
		t.Fatalf("approval.list approvals len = %d, want 0", len(listResp.Result.Approvals))
	}
	if listResp.Result.Storage != "stub" {
		t.Fatalf("approval.list storage = %q, want stub", listResp.Result.Storage)
	}

	var respondBuf bytes.Buffer
	params := json.RawMessage(`{"id":"appr-1","decision":"deny","reason":"test"}`)
	srv.dispatch(json.NewEncoder(&respondBuf), &Request{
		JSONRPC: "2.0",
		Method:  "approval.respond",
		Params:  params,
		ID:      json.RawMessage(`2`),
	})
	var respondResp struct {
		Result struct {
			OK       bool   `json:"ok"`
			Accepted bool   `json:"accepted"`
			ID       string `json:"id"`
			Decision string `json:"decision"`
			Storage  string `json:"storage"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respondBuf.Bytes(), &respondResp); err != nil {
		t.Fatalf("decode approval.respond response: %v", err)
	}
	if respondResp.Error != nil {
		t.Fatalf("approval.respond error = %+v", respondResp.Error)
	}
	if !respondResp.Result.OK || !respondResp.Result.Accepted {
		t.Fatalf("approval.respond result = %+v, want ok accepted", respondResp.Result)
	}
	if respondResp.Result.ID != "appr-1" || respondResp.Result.Decision != "deny" {
		t.Fatalf("approval.respond echoed id/decision = %+v", respondResp.Result)
	}
	if respondResp.Result.Storage != "stub" {
		t.Fatalf("approval.respond storage = %q, want stub", respondResp.Result.Storage)
	}
}

func TestApprovalRespondRejectsMissingID(t *testing.T) {
	var buf bytes.Buffer
	srv := &Server{spans: observability.NewRing(10)}
	srv.dispatch(json.NewEncoder(&buf), &Request{
		JSONRPC: "2.0",
		Method:  "approval.respond",
		Params:  json.RawMessage(`{"decision":"approve"}`),
		ID:      json.RawMessage(`1`),
	})

	var resp Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode approval.respond response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("approval.respond error = %+v, want invalid params", resp.Error)
	}
}

func TestApprovalRPCUsesPantryStorage(t *testing.T) {
	db, err := pantry.Open(filepath.Join(t.TempDir(), "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	id, err := db.Coding().RecordToolApproval(pantry.ToolApproval{
		Workspace:  "/repo",
		SessionID:  "sess-1",
		Client:     "minimax",
		ToolCallID: "call-1",
		ToolName:   "Write",
		Operation:  "write",
		Path:       "/repo/main.go",
		BeforeHash: "sha256:old",
		Preview:    "package main\n",
		Decision:   "pending",
	})
	if err != nil {
		t.Fatalf("RecordToolApproval: %v", err)
	}

	srv := &Server{spans: observability.NewRing(10), pantryDB: db}

	var listBuf bytes.Buffer
	srv.dispatch(json.NewEncoder(&listBuf), &Request{
		JSONRPC: "2.0",
		Method:  "approval.list",
		ID:      json.RawMessage(`1`),
	})
	var listResp struct {
		Result struct {
			Approvals []struct {
				ID       string `json:"id"`
				AgentID  string `json:"agent_id"`
				Kind     string `json:"kind"`
				Path     string `json:"path"`
				Decision string `json:"decision"`
			} `json:"approvals"`
			Storage string `json:"storage"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(listBuf.Bytes(), &listResp); err != nil {
		t.Fatalf("decode approval.list response: %v", err)
	}
	if listResp.Error != nil {
		t.Fatalf("approval.list error = %+v", listResp.Error)
	}
	if listResp.Result.Storage != "pantry" {
		t.Fatalf("approval.list storage = %q, want pantry", listResp.Result.Storage)
	}
	if len(listResp.Result.Approvals) != 1 {
		t.Fatalf("approval.list approvals len = %d, want 1", len(listResp.Result.Approvals))
	}
	got := listResp.Result.Approvals[0]
	if got.ID != "1" || got.AgentID != "minimax" || got.Kind != "write" || got.Path != "/repo/main.go" || got.Decision != "pending" {
		t.Fatalf("approval.list approval = %+v", got)
	}

	var respondBuf bytes.Buffer
	params := json.RawMessage(`{"id":"` + strconv.FormatInt(id, 10) + `","decision":"approve","reason":"reviewed"}`)
	srv.dispatch(json.NewEncoder(&respondBuf), &Request{
		JSONRPC: "2.0",
		Method:  "approval.respond",
		Params:  params,
		ID:      json.RawMessage(`2`),
	})
	var respondResp struct {
		Result struct {
			OK       bool   `json:"ok"`
			Decision string `json:"decision"`
			Storage  string `json:"storage"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respondBuf.Bytes(), &respondResp); err != nil {
		t.Fatalf("decode approval.respond response: %v", err)
	}
	if respondResp.Error != nil {
		t.Fatalf("approval.respond error = %+v", respondResp.Error)
	}
	if !respondResp.Result.OK || respondResp.Result.Decision != "approve" || respondResp.Result.Storage != "pantry" {
		t.Fatalf("approval.respond result = %+v", respondResp.Result)
	}

	approvals, err := db.Coding().ListToolApprovals("", "", 10)
	if err != nil {
		t.Fatalf("ListToolApprovals: %v", err)
	}
	if len(approvals) != 1 || approvals[0].Decision != "approve" || approvals[0].Reason != "reviewed" {
		t.Fatalf("updated approvals = %+v", approvals)
	}
}

func TestCodingRPCUsesPantryStorage(t *testing.T) {
	db, err := pantry.Open(filepath.Join(t.TempDir(), "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	changeID, err := db.Coding().InsertChangeSet(pantry.CodingChangeSet{
		Workspace: "/repo",
		SessionID: "sess-1",
		Client:    "minimax",
		Operation: "edit",
		Status:    "applied",
		Reason:    "tool result",
	})
	if err != nil {
		t.Fatalf("InsertChangeSet: %v", err)
	}
	if _, err := db.Coding().InsertFileChange(pantry.CodingFileChange{
		ChangeSetID: changeID,
		Workspace:   "/repo",
		SessionID:   "sess-1",
		Client:      "minimax",
		Operation:   "edit",
		Path:        "/repo/main.go",
		Diff:        "--- a/main.go\n+++ b/main.go\n@@\n-old\n+new\n",
	}); err != nil {
		t.Fatalf("InsertFileChange: %v", err)
	}

	srv := &Server{spans: observability.NewRing(10), pantryDB: db}

	var changesBuf bytes.Buffer
	srv.dispatch(json.NewEncoder(&changesBuf), &Request{
		JSONRPC: "2.0",
		Method:  "coding.changes",
		ID:      json.RawMessage(`1`),
	})
	var changesResp struct {
		Result struct {
			Changes []struct {
				ID        int64    `json:"id"`
				Client    string   `json:"client"`
				Operation string   `json:"operation"`
				Status    string   `json:"status"`
				Paths     []string `json:"paths"`
			} `json:"changes"`
			Storage string `json:"storage"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(changesBuf.Bytes(), &changesResp); err != nil {
		t.Fatalf("decode coding.changes response: %v", err)
	}
	if changesResp.Error != nil {
		t.Fatalf("coding.changes error = %+v", changesResp.Error)
	}
	if changesResp.Result.Storage != "pantry" {
		t.Fatalf("coding.changes storage = %q, want pantry", changesResp.Result.Storage)
	}
	if len(changesResp.Result.Changes) != 1 {
		t.Fatalf("coding.changes len = %d, want 1", len(changesResp.Result.Changes))
	}
	change := changesResp.Result.Changes[0]
	if change.ID != changeID || change.Client != "minimax" || change.Operation != "edit" || change.Status != "applied" {
		t.Fatalf("coding.changes change = %+v", change)
	}
	if len(change.Paths) != 1 || change.Paths[0] != "/repo/main.go" {
		t.Fatalf("coding.changes paths = %+v", change.Paths)
	}

	var diffBuf bytes.Buffer
	params := json.RawMessage(`{"id":` + strconv.FormatInt(changeID, 10) + `}`)
	srv.dispatch(json.NewEncoder(&diffBuf), &Request{
		JSONRPC: "2.0",
		Method:  "coding.diff",
		Params:  params,
		ID:      json.RawMessage(`2`),
	})
	var diffResp struct {
		Result struct {
			ID      int64  `json:"id"`
			Diff    string `json:"diff"`
			Storage string `json:"storage"`
		} `json:"result"`
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(diffBuf.Bytes(), &diffResp); err != nil {
		t.Fatalf("decode coding.diff response: %v", err)
	}
	if diffResp.Error != nil {
		t.Fatalf("coding.diff error = %+v", diffResp.Error)
	}
	if diffResp.Result.ID != changeID || diffResp.Result.Storage != "pantry" {
		t.Fatalf("coding.diff result = %+v", diffResp.Result)
	}
	if !strings.Contains(diffResp.Result.Diff, "-old") || !strings.Contains(diffResp.Result.Diff, "+new") {
		t.Fatalf("coding.diff missing diff body:\n%s", diffResp.Result.Diff)
	}
}

func TestApprovalRespondResumesWaitingToolDecision(t *testing.T) {
	t.Setenv("MILLIWAYS_PERMISSION_MODE", "auto")
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	path := filepath.Join(workspace, "old.txt")
	if err := os.WriteFile(path, []byte("delete me"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	db, err := pantry.Open(filepath.Join(t.TempDir(), "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hooks := codingToolHooks(db.Coding(), "minimax", workspace)
	if hooks.Decide == nil {
		t.Fatal("codingToolHooks Decide is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resultCh := make(chan runners.ToolDecisionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := hooks.Decide(ctx, runners.ToolDecisionRequest{
			SessionID: "sess-approval",
			Call: runners.ToolCall{
				ID:   "call-delete",
				Name: "Delete",
				Args: `{"path":"` + filepath.ToSlash(path) + `"}`,
			},
			ToolName: "Delete",
			Args: map[string]any{
				"path": path,
			},
		})
		resultCh <- result
		errCh <- err
	}()

	var approvalID string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		approvals, err := db.Coding().ListToolApprovals("", "", 10)
		if err != nil {
			t.Fatalf("ListToolApprovals: %v", err)
		}
		if len(approvals) > 0 {
			approvalID = strconv.FormatInt(approvals[0].ID, 10)
			if approvals[0].Decision != "pending" {
				t.Fatalf("approval decision before response = %q, want pending", approvals[0].Decision)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if approvalID == "" {
		t.Fatal("waiting tool decision did not record a pending approval")
	}

	select {
	case result := <-resultCh:
		t.Fatalf("tool decision returned before approval: %+v", result)
	default:
	}

	srv := &Server{spans: observability.NewRing(10), pantryDB: db}
	var respondBuf bytes.Buffer
	params := json.RawMessage(`{"id":"` + approvalID + `","decision":"approve","reason":"reviewed live"}`)
	srv.dispatch(json.NewEncoder(&respondBuf), &Request{
		JSONRPC: "2.0",
		Method:  "approval.respond",
		Params:  params,
		ID:      json.RawMessage(`1`),
	})
	var respondResp Response
	if err := json.Unmarshal(respondBuf.Bytes(), &respondResp); err != nil {
		t.Fatalf("decode approval.respond response: %v", err)
	}
	if respondResp.Error != nil {
		t.Fatalf("approval.respond error = %+v", respondResp.Error)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("tool decision error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("tool decision did not resume after approval.respond")
	}
	select {
	case result := <-resultCh:
		if result.Decision != runners.ToolDecisionAllow {
			t.Fatalf("tool decision = %+v, want allow", result)
		}
	case <-ctx.Done():
		t.Fatal("tool decision result missing after approval.respond")
	}
}

func TestApprovalWaitTimeoutCleansWaiter(t *testing.T) {
	t.Setenv("MILLIWAYS_PERMISSION_MODE", "auto")
	t.Setenv("MILLIWAYS_APPROVAL_WAIT_TIMEOUT", "20ms")
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	path := filepath.Join(workspace, "old.txt")
	if err := os.WriteFile(path, []byte("delete me"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	db, err := pantry.Open(filepath.Join(t.TempDir(), "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hooks := codingToolHooks(db.Coding(), "minimax", workspace)
	resultCh := make(chan runners.ToolDecisionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := hooks.Decide(context.Background(), runners.ToolDecisionRequest{
			SessionID: "sess-timeout",
			Call: runners.ToolCall{
				ID:   "call-delete-timeout",
				Name: "Delete",
				Args: `{"path":"` + filepath.ToSlash(path) + `"}`,
			},
			ToolName: "Delete",
			Args: map[string]any{
				"path": path,
			},
		})
		resultCh <- result
		errCh <- err
	}()

	var result runners.ToolDecisionResult
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("tool decision error = %v", err)
		}
		result = <-resultCh
	case <-time.After(500 * time.Millisecond):
		t.Fatal("tool decision did not return after approval wait timeout")
	}
	if result.Decision != runners.ToolDecisionBlock {
		t.Fatalf("tool decision = %+v, want block", result)
	}
	if !strings.Contains(result.Message, "approval wait timed out") {
		t.Fatalf("tool decision message = %q, want timeout", result.Message)
	}

	approvals, err := db.Coding().ListToolApprovals("", "", 10)
	if err != nil {
		t.Fatalf("ListToolApprovals: %v", err)
	}
	if len(approvals) != 1 {
		t.Fatalf("approval count = %d, want 1", len(approvals))
	}
	if approvals[0].Decision != "deny" {
		t.Fatalf("approval decision after timeout = %q, want deny", approvals[0].Decision)
	}
	if !strings.Contains(approvals[0].Reason, "approval wait timed out") {
		t.Fatalf("approval reason after timeout = %q, want timeout", approvals[0].Reason)
	}
	id := approvals[0].ID

	toolApprovalWaiters.Lock()
	waiterCount := len(toolApprovalWaiters.chans[id])
	toolApprovalWaiters.Unlock()
	if waiterCount != 0 {
		t.Fatalf("approval waiter count for %d = %d, want 0", id, waiterCount)
	}
}

func TestCodingToolHookMetadataRedactsMutationPreview(t *testing.T) {
	t.Setenv("MILLIWAYS_PERMISSION_MODE", "default")
	t.Setenv("MILLIWAYS_APPROVAL_WAIT_TIMEOUT", "20ms")
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	path := filepath.Join(workspace, "secret.txt")
	db, err := pantry.Open(filepath.Join(t.TempDir(), "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hooks := codingToolHooks(db.Coding(), "minimax", workspace)
	secret := "super-secret-token"
	result, err := hooks.Decide(context.Background(), runners.ToolDecisionRequest{
		SessionID: "sess-redact",
		Call: runners.ToolCall{
			ID:   "call-write",
			Name: "Write",
			Args: `{"path":"` + filepath.ToSlash(path) + `"}`,
		},
		ToolName: "Write",
		Args: map[string]any{
			"path":    path,
			"content": secret,
		},
	})
	if err != nil {
		t.Fatalf("tool decision error = %v", err)
	}
	if result.Decision != runners.ToolDecisionBlock {
		t.Fatalf("tool decision = %+v, want block after approval timeout", result)
	}
	rawMetadata, err := json.Marshal(result.Metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if strings.Contains(string(rawMetadata), secret) {
		t.Fatalf("tool decision metadata leaked mutation preview: %s", rawMetadata)
	}

	approvals, err := db.Coding().ListToolApprovals("", "", 10)
	if err != nil {
		t.Fatalf("ListToolApprovals: %v", err)
	}
	if len(approvals) != 1 {
		t.Fatalf("approval count = %d, want 1", len(approvals))
	}
	if approvals[0].Preview != secret {
		t.Fatalf("approval preview = %q, want audit preview", approvals[0].Preview)
	}
	if approvals[0].Decision != "deny" {
		t.Fatalf("approval decision after timeout = %q, want deny", approvals[0].Decision)
	}
}

func TestApprovalRespondDenyResumesWaitingToolDecisionAsBlock(t *testing.T) {
	t.Setenv("MILLIWAYS_PERMISSION_MODE", "auto")
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	path := filepath.Join(workspace, "old.txt")
	if err := os.WriteFile(path, []byte("delete me"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	db, err := pantry.Open(filepath.Join(t.TempDir(), "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hooks := codingToolHooks(db.Coding(), "minimax", workspace)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resultCh := make(chan runners.ToolDecisionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := hooks.Decide(ctx, runners.ToolDecisionRequest{
			SessionID: "sess-deny",
			Call: runners.ToolCall{
				ID:   "call-delete-deny",
				Name: "Delete",
				Args: `{"path":"` + filepath.ToSlash(path) + `"}`,
			},
			ToolName: "Delete",
			Args: map[string]any{
				"path": path,
			},
		})
		resultCh <- result
		errCh <- err
	}()

	var approvalID string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		approvals, err := db.Coding().ListToolApprovals("", "", 10)
		if err != nil {
			t.Fatalf("ListToolApprovals: %v", err)
		}
		if len(approvals) > 0 {
			approvalID = strconv.FormatInt(approvals[0].ID, 10)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if approvalID == "" {
		t.Fatal("waiting tool decision did not record a pending approval")
	}

	srv := &Server{spans: observability.NewRing(10), pantryDB: db}
	var respondBuf bytes.Buffer
	params := json.RawMessage(`{"id":"` + approvalID + `","decision":"deny","reason":"not safe"}`)
	srv.dispatch(json.NewEncoder(&respondBuf), &Request{
		JSONRPC: "2.0",
		Method:  "approval.respond",
		Params:  params,
		ID:      json.RawMessage(`1`),
	})
	var respondResp Response
	if err := json.Unmarshal(respondBuf.Bytes(), &respondResp); err != nil {
		t.Fatalf("decode approval.respond response: %v", err)
	}
	if respondResp.Error != nil {
		t.Fatalf("approval.respond error = %+v", respondResp.Error)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("tool decision error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("tool decision did not resume after denial")
	}
	select {
	case result := <-resultCh:
		if result.Decision != runners.ToolDecisionBlock {
			t.Fatalf("tool decision = %+v, want block", result)
		}
		if !strings.Contains(result.Message, "denied by approval response") {
			t.Fatalf("tool decision message = %q, want denial reason", result.Message)
		}
		if result.Metadata["approval_decision"] != "deny" {
			t.Fatalf("approval metadata = %+v, want approval_decision deny", result.Metadata)
		}
	case <-ctx.Done():
		t.Fatal("tool decision result missing after denial")
	}

	approvals, err := db.Coding().ListToolApprovals("", "", 10)
	if err != nil {
		t.Fatalf("ListToolApprovals: %v", err)
	}
	if len(approvals) != 1 || approvals[0].Decision != "deny" || approvals[0].Reason != "not safe" {
		t.Fatalf("approval record after deny = %+v", approvals)
	}
}

func TestToolApprovalWaiterCancellationCleansWaiter(t *testing.T) {
	db, err := pantry.Open(filepath.Join(t.TempDir(), "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	id, err := db.Coding().RecordToolApproval(pantry.ToolApproval{
		Workspace: "/repo",
		SessionID: "sess-cancel",
		Client:    "minimax",
		ToolName:  "Delete",
		Operation: "delete",
		Path:      "/repo/old.txt",
		Decision:  "pending",
	})
	if err != nil {
		t.Fatalf("RecordToolApproval: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := waitForToolApproval(ctx, db.Coding(), id)
		errCh <- err
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		toolApprovalWaiters.Lock()
		waiterCount := len(toolApprovalWaiters.chans[id])
		toolApprovalWaiters.Unlock()
		if waiterCount == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	toolApprovalWaiters.Lock()
	waiterCount := len(toolApprovalWaiters.chans[id])
	toolApprovalWaiters.Unlock()
	if waiterCount != 1 {
		t.Fatalf("waiter count before cancel = %d, want 1", waiterCount)
	}

	cancel()
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("waitForToolApproval err = %v, want canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waitForToolApproval did not return after cancellation")
	}

	toolApprovalWaiters.Lock()
	_, stillRegistered := toolApprovalWaiters.chans[id]
	toolApprovalWaiters.Unlock()
	if stillRegistered {
		t.Fatalf("waiter for approval %d remained registered after cancellation", id)
	}
}

func TestToolApprovalWaiterPrefersApprovalOverConcurrentCancellation(t *testing.T) {
	db, err := pantry.Open(filepath.Join(t.TempDir(), "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for attempt := 0; attempt < 100; attempt++ {
		id, err := db.Coding().RecordToolApproval(pantry.ToolApproval{
			Workspace: "/repo",
			SessionID: "sess-concurrent-cancel",
			Client:    "minimax",
			ToolName:  "Delete",
			Operation: "delete",
			Path:      "/repo/old.txt",
			Decision:  "pending",
		})
		if err != nil {
			t.Fatalf("RecordToolApproval: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		resultCh := make(chan string, 1)
		errCh := make(chan error, 1)
		go func() {
			decision, err := waitForToolApproval(ctx, db.Coding(), id)
			resultCh <- decision
			errCh <- err
		}()

		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			toolApprovalWaiters.Lock()
			waiterCount := len(toolApprovalWaiters.chans[id])
			toolApprovalWaiters.Unlock()
			if waiterCount == 1 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		toolApprovalWaiters.Lock()
		waiterCount := len(toolApprovalWaiters.chans[id])
		toolApprovalWaiters.Unlock()
		if waiterCount != 1 {
			t.Fatalf("attempt %d: waiter count before response = %d, want 1", attempt, waiterCount)
		}

		if err := db.Coding().UpdateToolApprovalDecision(id, "approve", "reviewed"); err != nil {
			t.Fatalf("UpdateToolApprovalDecision: %v", err)
		}
		cancel()
		notifyToolApproval(id, "approve")

		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("attempt %d: waitForToolApproval err = %v, want approval", attempt, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("attempt %d: waitForToolApproval did not return", attempt)
		}
		select {
		case decision := <-resultCh:
			if decision != "approve" {
				t.Fatalf("attempt %d: decision = %q, want approve", attempt, decision)
			}
		case <-time.After(time.Second):
			t.Fatalf("attempt %d: waitForToolApproval decision missing", attempt)
		}
	}
}

// TestHistoryRPC simulates appending history via history.append and reading
// it back via history.get through the internal helpers. Uses a temp dir as
// the server state dir to avoid touching the real runtime state.
func TestHistoryRPC(t *testing.T) {
	dir, err := os.MkdirTemp("", "milliways-state-")
	if err != nil {
		t.Fatalf("tmpdir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	agent := "_test-agent"
	p := map[string]any{"hello": "world"}
	if err := history.AppendAgentHistory(dir, agent, p, 100); err != nil {
		t.Fatalf("append: %v", err)
	}

	// ensure file exists
	fpath := filepath.Join(dir, "history", agent+".ndjson")
	if _, err := os.Stat(fpath); err != nil {
		t.Fatalf("stat: %v", err)
	}

	// read back
	res, err := history.ReadAgentHistory(dir, agent, -1)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(res))
	}
	// verify shape
	if _, ok := res[0]["t"]; !ok {
		t.Fatalf("missing t field")
	}
	if v, ok := res[0]["v"].(map[string]any); !ok || v["hello"] != "world" {
		b, _ := json.MarshalIndent(res, "", "  ")
		t.Fatalf("unexpected payload: %s", b)
	}

	// append a chunk_end event
	ce := map[string]any{"t": "chunk_end", "cost_usd": 0.012}
	if err := history.AppendAgentHistory(dir, agent, ce, 100); err != nil {
		t.Fatalf("append chunk_end: %v", err)
	}
	res2, err := history.ReadAgentHistory(dir, agent, -1)
	if err != nil {
		t.Fatalf("read2: %v", err)
	}
	if len(res2) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res2))
	}
	// trimming: append many entries and verify trimming keeps last N
	for i := 0; i < 150; i++ {
		_ = history.AppendAgentHistory(dir, agent, map[string]any{"i": i}, 100)
	}
	res3, err := history.ReadAgentHistory(dir, agent, -1)
	if err != nil {
		t.Fatalf("read3: %v", err)
	}
	if len(res3) != 100 {
		t.Fatalf("expected trim to 100 lines, got %d", len(res3))
	}

	// check timestamps are present and increasing
	var last int64
	for _, e := range res3 {
		if tval, ok := e["t"].(float64); ok {
			if int64(tval) < last {
				t.Fatalf("non-monotonic time: %d < %d", int64(tval), last)
			}
			last = int64(tval)
		} else {
			t.Fatalf("t not number")
		}
	}

	// small sleep to ensure filesystem timestamps differ for manual inspection
	t.Logf("history file path: %s", fpath)
	t.Log("TestHistoryRPC passed")
}
