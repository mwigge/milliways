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

//go:build integration

package main

// parallel_integration_test.go — end-to-end tests for the
// runParallelStatus and runParallelConsensus functions in milliwaysctl.
//
// Starts a real daemon, dispatches a parallel group via parallel.dispatch,
// then exercises runParallelStatus and runParallelConsensus against the
// real group.
//
// Build / run:
//
//	go test ./cmd/milliwaysctl/... -tags integration -run TestParallelCtl -v

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/daemon"
	"github.com/mwigge/milliways/internal/rpc"
)

// startCtlTestDaemon starts a real server and returns the socket path
// and a cleanup func. Uses os.MkdirTemp with a short prefix to stay
// well within the 104-byte UDS socket path limit on macOS.
func startCtlTestDaemon(t *testing.T) (socketPath string, cleanup func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "mwctl-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	sock := filepath.Join(dir, "s")
	srv, err := daemon.NewServer(sock)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("NewServer: %v", err)
	}
	go srv.Serve() //nolint:errcheck // background; errors surface through test failures
	time.Sleep(60 * time.Millisecond)
	return sock, func() {
		srv.Close()
		os.RemoveAll(dir)
	}
}

// dispatchTestGroup dispatches a parallel group to the _echo provider and
// returns the group_id. It uses the rpc.Client directly so no milliwaysctl
// binary is required.
func dispatchTestGroup(t *testing.T, sock string) string {
	t.Helper()
	c, err := rpc.Dial(sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	var result struct {
		GroupID string `json:"group_id"`
	}
	if err := c.Call("parallel.dispatch", map[string]any{
		"providers": []string{"_echo"},
		"prompt":    "integration test prompt",
	}, &result); err != nil {
		t.Fatalf("parallel.dispatch: %v", err)
	}
	if result.GroupID == "" {
		t.Fatal("parallel.dispatch returned empty group_id")
	}
	return result.GroupID
}

// TestRunParallelListIntegration verifies that runParallelList returns exit 0
// and prints at least one group after a parallel.dispatch.
func TestRunParallelListIntegration(t *testing.T) {
	sock, cleanup := startCtlTestDaemon(t)
	defer cleanup()

	_ = dispatchTestGroup(t, sock)
	// Allow the daemon time to register the group.
	time.Sleep(20 * time.Millisecond)

	var out, errw bytes.Buffer
	code := runParallelList(nil, &out, &errw, sock)
	if code != 0 {
		t.Fatalf("runParallelList returned %d; errw=%q", code, errw.String())
	}
	t.Logf("list output:\n%s", out.String())
	if errw.Len() > 0 {
		t.Errorf("runParallelList wrote to errw: %q", errw.String())
	}
	// Must not print "(no parallel groups)" since we just created one.
	if strings.Contains(out.String(), "(no parallel groups)") {
		t.Error("expected at least one group in list, got none")
	}
}

// TestRunParallelStatusIntegration verifies that runParallelStatus exits 0
// and prints a slot table containing the _echo provider for a real group.
func TestRunParallelStatusIntegration(t *testing.T) {
	sock, cleanup := startCtlTestDaemon(t)
	defer cleanup()

	groupID := dispatchTestGroup(t, sock)
	time.Sleep(20 * time.Millisecond)

	var out, errw bytes.Buffer
	code := runParallelStatus([]string{groupID}, &out, &errw, sock)
	if code != 0 {
		t.Fatalf("runParallelStatus returned %d; errw=%q", code, errw.String())
	}
	outStr := out.String()
	t.Logf("status output:\n%s", outStr)
	if errw.Len() > 0 {
		t.Errorf("runParallelStatus wrote to errw: %q", errw.String())
	}

	// The output must contain the group_id and the provider name.
	if !strings.Contains(outStr, groupID[:8]) {
		t.Errorf("status output does not contain group_id %q; got: %q", groupID, outStr)
	}
	if !strings.Contains(outStr, "_echo") {
		t.Errorf("status output does not contain provider %q; got: %q", "_echo", outStr)
	}
}

// TestRunParallelConsensusIntegration verifies that runParallelConsensus exits
// 0 and returns a non-empty JSON result for a real group.
func TestRunParallelConsensusIntegration(t *testing.T) {
	sock, cleanup := startCtlTestDaemon(t)
	defer cleanup()

	groupID := dispatchTestGroup(t, sock)

	// Give the _echo agent time to process and buffer its response.
	time.Sleep(200 * time.Millisecond)

	var out, errw bytes.Buffer
	code := runParallelConsensus([]string{groupID}, &out, &errw, sock)
	if code != 0 {
		t.Fatalf("runParallelConsensus returned %d; errw=%q", code, errw.String())
	}
	outStr := out.String()
	t.Logf("consensus output:\n%s", outStr)
	if errw.Len() > 0 {
		t.Errorf("runParallelConsensus wrote to errw: %q", errw.String())
	}
	// Output must be non-empty JSON.
	if outStr == "" {
		t.Error("runParallelConsensus produced no output")
	}
	// Must contain the group_id in the response.
	if !strings.Contains(outStr, groupID[:8]) {
		t.Errorf("consensus output does not contain group_id prefix; got: %q", outStr)
	}
	// Must contain findings key.
	if !strings.Contains(outStr, "HIGH") && !strings.Contains(outStr, "no structured") {
		t.Errorf("consensus output missing expected content; got: %q", outStr)
	}
}

// TestRunParallelStatusMissingGroupID verifies that runParallelStatus returns
// exit code 1 when no group_id is provided.
func TestRunParallelStatusMissingGroupID(t *testing.T) {
	sock, cleanup := startCtlTestDaemon(t)
	defer cleanup()

	var out, errw bytes.Buffer
	code := runParallelStatus(nil, &out, &errw, sock)
	if code != 2 {
		t.Errorf("runParallelStatus with no args returned %d, want 2", code)
	}
	if errw.Len() == 0 {
		t.Error("expected error message in errw")
	}
}

// TestRunParallelConsensusUnknownGroup verifies that runParallelConsensus
// returns exit code 1 for an unknown group_id.
func TestRunParallelConsensusUnknownGroup(t *testing.T) {
	sock, cleanup := startCtlTestDaemon(t)
	defer cleanup()

	var out, errw bytes.Buffer
	code := runParallelConsensus([]string{"00000000-0000-0000-0000-000000000000"}, &out, &errw, sock)
	if code != 1 {
		t.Errorf("runParallelConsensus with unknown group returned %d, want 1", code)
	}
	if errw.Len() == 0 {
		t.Error("expected error message in errw for unknown group")
	}
}
