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
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/rpc"
)

type chatSecurityRPCCall struct {
	Method string
	Params map[string]any
}

func startChatSecurityRPCTestServer(t *testing.T, results map[string]any) (string, <-chan chatSecurityRPCCall) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "mw.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	calls := make(chan chatSecurityRPCCall, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleChatSecurityRPCTestConn(conn, results, calls)
		}
	}()
	return sock, calls
}

func handleChatSecurityRPCTestConn(conn net.Conn, results map[string]any, calls chan<- chatSecurityRPCCall) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	enc := json.NewEncoder(conn)
	for scanner.Scan() {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			ID     int64           `json:"id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			return
		}
		params := map[string]any{}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params)
		}
		calls <- chatSecurityRPCCall{Method: req.Method, Params: params}

		result, ok := results[req.Method]
		if !ok {
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error": map[string]any{
					"code":    -32601,
					"message": "method not found",
				},
			})
			continue
		}
		_ = enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}
}

func newSecurityTestLoop(t *testing.T, results map[string]any) (*chatLoop, <-chan chatSecurityRPCCall, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	sock, calls := startChatSecurityRPCTestServer(t, results)
	client, err := rpc.Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	var stdout, stderr bytes.Buffer
	return &chatLoop{client: client, out: &stdout, errw: &stderr}, calls, &stdout, &stderr
}

func requireSecurityCall(t *testing.T, calls <-chan chatSecurityRPCCall) chatSecurityRPCCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for security RPC call")
		return chatSecurityRPCCall{}
	}
}

func TestHandleSecurityNilClientPrintsError(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	loop := &chatLoop{out: &stdout, errw: &stderr}
	loop.handleSlash("/security status")

	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[security] not connected") {
		t.Fatalf("expected not-connected error, got %q", stderr.String())
	}
}

func TestHandleSecurityStatusCallsRPC(t *testing.T) {
	t.Parallel()

	loop, calls, stdout, stderr := newSecurityTestLoop(t, map[string]any{
		"security.status": map[string]any{
			"mode":                 "warn",
			"posture":              "warn",
			"warnings":             float64(2),
			"blocks":               float64(1),
			"last_startup_scan_at": "2026-05-14T10:00:00Z",
			"scanners": []any{
				map[string]any{"name": "osv-scanner", "installed": true, "version": "osv-scanner 2.0.0"},
				map[string]any{"name": "gitleaks", "installed": false},
			},
		},
	})

	loop.handleSlash("/security status")
	call := requireSecurityCall(t, calls)
	if call.Method != "security.status" {
		t.Fatalf("method = %q, want security.status", call.Method)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	for _, want := range []string{"mode: warn", "posture: WARN", "warnings: 2", "blocks: 1", "installed osv-scanner", "missing gitleaks", "last startup scan"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("status output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestHandleSecurityClientCallsRPC(t *testing.T) {
	t.Parallel()

	loop, calls, stdout, stderr := newSecurityTestLoop(t, map[string]any{
		"security.client_profile": map[string]any{
			"client":   "codex",
			"warnings": []any{"untrusted workspace", "tool allowlist missing"},
		},
	})

	loop.handleSlash("/security client codex")
	call := requireSecurityCall(t, calls)
	if call.Method != "security.client_profile" {
		t.Fatalf("method = %q, want security.client_profile", call.Method)
	}
	if client, _ := call.Params["client"].(string); client != "codex" {
		t.Fatalf("client param = %#v, want codex", call.Params)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "2 warning(s)") || !strings.Contains(stdout.String(), "untrusted workspace") {
		t.Fatalf("client output did not summarize warnings:\n%s", stdout.String())
	}
}

func TestHandleSecurityCommandCheckCallsRPC(t *testing.T) {
	t.Parallel()

	loop, calls, stdout, stderr := newSecurityTestLoop(t, map[string]any{
		"security.command_check": map[string]any{
			"decision":        "block",
			"reason":          "command changes dependencies",
			"risk_categories": []any{"package-install", "postinstall"},
		},
	})

	loop.handleSlash("/security command-check --mode strict --client codex -- npm install left-pad")
	call := requireSecurityCall(t, calls)
	if call.Method != "security.command_check" {
		t.Fatalf("method = %q, want security.command_check", call.Method)
	}
	if command, _ := call.Params["command"].(string); command != "npm install left-pad" {
		t.Fatalf("command param = %#v, want joined command", call.Params)
	}
	if mode, _ := call.Params["mode"].(string); mode != "strict" {
		t.Fatalf("mode param = %#v, want strict", call.Params)
	}
	if client, _ := call.Params["client"].(string); client != "codex" {
		t.Fatalf("client param = %#v, want codex", call.Params)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	for _, want := range []string{"decision: block", "command changes dependencies", "package-install, postinstall"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("command-check output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestHandleSecurityWarningsCallsRPC(t *testing.T) {
	t.Parallel()

	loop, calls, stdout, stderr := newSecurityTestLoop(t, map[string]any{
		"security.warnings": map[string]any{
			"warnings": []any{
				map[string]any{"severity": "high", "message": "startup script is risky"},
			},
		},
	})

	loop.handleSlash("/security warnings")
	call := requireSecurityCall(t, calls)
	if call.Method != "security.warnings" {
		t.Fatalf("method = %q, want security.warnings", call.Method)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "1 warning(s)") || !strings.Contains(stdout.String(), "HIGH: startup script is risky") {
		t.Fatalf("warnings output did not summarize warning:\n%s", stdout.String())
	}
}
