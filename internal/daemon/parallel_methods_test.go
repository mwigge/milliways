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
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// parallelTestHarness starts a real Server and returns a connected encoder/scanner pair.
// Callers must defer srv.Close() and conn.Close().
func parallelTestHarness(t *testing.T) (srv *Server, send func(method string, params, id any), readResp func() (map[string]any, error), cleanup func()) {
	t.Helper()
	stateDir, err := os.MkdirTemp("", "milliways-parallel-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	sock := filepath.Join(stateDir, "sock")

	srv, err = NewServer(sock)
	if err != nil {
		os.RemoveAll(stateDir)
		t.Fatalf("NewServer: %v", err)
	}
	go srv.Serve()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		srv.Close()
		os.RemoveAll(stateDir)
		t.Fatalf("dial: %v", err)
	}

	enc := json.NewEncoder(conn)
	reader := bufio.NewReader(conn)

	send = func(method string, params, id any) {
		t.Helper()
		req := map[string]any{
			"jsonrpc": "2.0",
			"method":  method,
		}
		if params != nil {
			req["params"] = params
		}
		if id != nil {
			req["id"] = id
		}
		if err := enc.Encode(req); err != nil {
			t.Fatalf("encode %s: %v", method, err)
		}
	}

	readResp = func() (map[string]any, error) {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var resp map[string]any
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, err
		}
		return resp, nil
	}

	cleanup = func() {
		conn.Close()
		srv.Close()
		os.RemoveAll(stateDir)
	}
	return
}

func TestCodeGraphInjectTimeoutFromEnv(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want time.Duration
	}{
		{name: "default", want: 750 * time.Millisecond},
		{name: "invalid", env: "nope", want: 750 * time.Millisecond},
		{name: "negative", env: "-1s", want: 750 * time.Millisecond},
		{name: "custom", env: "25ms", want: 25 * time.Millisecond},
		{name: "capped", env: "10s", want: 5 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MILLIWAYS_CODEGRAPH_TIMEOUT", tc.env)
			if got := codeGraphInjectTimeout(); got != tc.want {
				t.Fatalf("codeGraphInjectTimeout() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestParallelDispatch_EchoAgent verifies that parallel.dispatch with _echo
// provider returns a group_id and at least one slot.
func TestParallelDispatch_EchoAgent(t *testing.T) {
	t.Parallel()

	_, send, readResp, cleanup := parallelTestHarness(t)
	defer cleanup()

	send("parallel.dispatch", map[string]any{
		"prompt":    "hello parallel",
		"providers": []string{"_echo"},
	}, 1)

	resp, err := readResp()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("parallel.dispatch error: %v", errObj)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got: %v", resp)
	}

	groupID, ok := result["group_id"].(string)
	if !ok || groupID == "" {
		t.Errorf("expected non-empty group_id, got: %v", result["group_id"])
	}

	slots, ok := result["slots"].([]any)
	if !ok {
		t.Fatalf("expected slots array, got: %v", result["slots"])
	}
	if len(slots) == 0 {
		t.Fatal("expected at least one slot")
	}

	slot, ok := slots[0].(map[string]any)
	if !ok {
		t.Fatalf("expected slot map, got: %v", slots[0])
	}
	if _, ok := slot["handle"].(float64); !ok {
		t.Errorf("expected numeric handle in slot, got: %v", slot)
	}
	if provider, ok := slot["provider"].(string); !ok || provider != "_echo" {
		t.Errorf("expected provider=_echo, got: %v", slot["provider"])
	}

	t.Logf("parallel.dispatch group_id=%s slots=%d", groupID, len(slots))
}

// TestParallelDispatch_EmptyPrompt verifies that parallel.dispatch rejects
// an empty prompt.
func TestParallelDispatch_EmptyPrompt(t *testing.T) {
	t.Parallel()

	_, send, readResp, cleanup := parallelTestHarness(t)
	defer cleanup()

	send("parallel.dispatch", map[string]any{
		"prompt":    "",
		"providers": []string{"_echo"},
	}, 1)

	resp, err := readResp()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if _, ok := resp["error"].(map[string]any); !ok {
		t.Error("expected error response for empty prompt")
	}
}

// TestParallelDispatch_AllProvidersFail verifies that when all providers fail
// (unknown provider), a JSON-RPC error is returned.
func TestParallelDispatch_AllProvidersFail(t *testing.T) {
	t.Parallel()

	_, send, readResp, cleanup := parallelTestHarness(t)
	defer cleanup()

	send("parallel.dispatch", map[string]any{
		"prompt":    "hello",
		"providers": []string{"does-not-exist"},
	}, 1)

	resp, err := readResp()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if _, ok := resp["error"].(map[string]any); !ok {
		t.Error("expected error response when all providers fail")
	}
}

// TestGroupStatus_ReturnsStructure verifies that group.status returns the
// correct structure after a parallel.dispatch.
func TestGroupStatus_ReturnsStructure(t *testing.T) {
	t.Parallel()

	_, send, readResp, cleanup := parallelTestHarness(t)
	defer cleanup()

	// First dispatch a group.
	send("parallel.dispatch", map[string]any{
		"prompt":    "status test prompt",
		"providers": []string{"_echo"},
	}, 1)

	dispatchResp, err := readResp()
	if err != nil {
		t.Fatalf("read dispatch response: %v", err)
	}
	if errObj, ok := dispatchResp["error"].(map[string]any); ok {
		t.Fatalf("parallel.dispatch error: %v", errObj)
	}
	result, ok := dispatchResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result map: %v", dispatchResp)
	}
	groupID, _ := result["group_id"].(string)

	// Now call group.status.
	send("group.status", map[string]any{"group_id": groupID}, 2)

	statusResp, err := readResp()
	if err != nil {
		t.Fatalf("read status response: %v", err)
	}
	if errObj, ok := statusResp["error"].(map[string]any); ok {
		t.Fatalf("group.status error: %v", errObj)
	}

	statusResult, ok := statusResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result map: %v", statusResp)
	}

	if id, ok := statusResult["group_id"].(string); !ok || id != groupID {
		t.Errorf("expected group_id=%s, got: %v", groupID, statusResult["group_id"])
	}
	if _, ok := statusResult["prompt"].(string); !ok {
		t.Error("expected prompt field in group.status result")
	}
	if _, ok := statusResult["status"].(string); !ok {
		t.Error("expected status field in group.status result")
	}
	if _, ok := statusResult["created_at"].(string); !ok {
		t.Error("expected created_at field in group.status result")
	}
	slots, ok := statusResult["slots"].([]any)
	if !ok {
		t.Fatalf("expected slots array in group.status result")
	}
	if len(slots) == 0 {
		t.Error("expected at least one slot in group.status result")
	}

	t.Logf("group.status: group_id=%s status=%v slots=%d", groupID, statusResult["status"], len(slots))
}

// TestGroupStatus_UnknownGroup verifies that group.status returns an error
// for an unknown group_id.
func TestGroupStatus_UnknownGroup(t *testing.T) {
	t.Parallel()

	_, send, readResp, cleanup := parallelTestHarness(t)
	defer cleanup()

	send("group.status", map[string]any{"group_id": "no-such-group"}, 1)

	resp, err := readResp()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if _, ok := resp["error"].(map[string]any); !ok {
		t.Error("expected error for unknown group_id")
	}
}

// TestGroupList_ReturnsList verifies that group.list returns a groups array
// that includes groups created via parallel.dispatch.
func TestGroupList_ReturnsList(t *testing.T) {
	t.Parallel()

	_, send, readResp, cleanup := parallelTestHarness(t)
	defer cleanup()

	// Dispatch a group.
	send("parallel.dispatch", map[string]any{
		"prompt":    "list test",
		"providers": []string{"_echo"},
	}, 1)
	dispatchResp, err := readResp()
	if err != nil {
		t.Fatalf("read dispatch: %v", err)
	}
	if errObj, ok := dispatchResp["error"].(map[string]any); ok {
		t.Fatalf("dispatch error: %v", errObj)
	}

	// Now list groups.
	send("group.list", map[string]any{}, 2)
	listResp, err := readResp()
	if err != nil {
		t.Fatalf("read list: %v", err)
	}
	if errObj, ok := listResp["error"].(map[string]any); ok {
		t.Fatalf("group.list error: %v", errObj)
	}

	listResult, ok := listResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result map: %v", listResp)
	}
	groups, ok := listResult["groups"].([]any)
	if !ok {
		t.Fatalf("expected groups array: %v", listResult)
	}
	if len(groups) == 0 {
		t.Error("expected at least one group after dispatch")
	}

	// Verify group summary shape.
	g, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("expected group summary map: %v", groups[0])
	}
	for _, field := range []string{"group_id", "prompt", "status", "created_at"} {
		if _, ok := g[field]; !ok {
			t.Errorf("group summary missing field %q", field)
		}
	}
	t.Logf("group.list returned %d groups", len(groups))
}

// TestConsensusAggregate_Stub verifies that consensus.aggregate returns a
// result with a summary field (stub implementation).
func TestConsensusAggregate_Stub(t *testing.T) {
	t.Parallel()

	_, send, readResp, cleanup := parallelTestHarness(t)
	defer cleanup()

	// Dispatch a group first.
	send("parallel.dispatch", map[string]any{
		"prompt":    "consensus test",
		"providers": []string{"_echo"},
	}, 1)
	dispatchResp, err := readResp()
	if err != nil {
		t.Fatalf("read dispatch: %v", err)
	}
	result, _ := dispatchResp["result"].(map[string]any)
	groupID, _ := result["group_id"].(string)

	// Call consensus.aggregate.
	send("consensus.aggregate", map[string]any{"group_id": groupID}, 2)
	consensusResp, err := readResp()
	if err != nil {
		t.Fatalf("read consensus: %v", err)
	}
	if errObj, ok := consensusResp["error"].(map[string]any); ok {
		t.Fatalf("consensus.aggregate error: %v", errObj)
	}

	consensusResult, ok := consensusResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result map: %v", consensusResp)
	}
	if _, ok := consensusResult["summary"].(string); !ok {
		t.Error("expected summary string in consensus.aggregate result")
	}
	t.Logf("consensus.aggregate summary: %v", consensusResult["summary"])
}
