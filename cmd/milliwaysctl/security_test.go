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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type securityRPCCall struct {
	Method string
	Params map[string]any
}

func startSecurityRPCTestServer(t *testing.T, results map[string]any) (string, <-chan securityRPCCall) {
	t.Helper()
	sock := shortSecurityTestSocket(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	calls := make(chan securityRPCCall, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSecurityRPCTestConn(conn, results, calls)
		}
	}()
	return sock, calls
}

func shortSecurityTestSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mwctl-sec-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

func handleSecurityRPCTestConn(conn net.Conn, results map[string]any, calls chan<- securityRPCCall) {
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
		calls <- securityRPCCall{Method: req.Method, Params: params}

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

func joinSecurityTestStrings(values []any) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if s, ok := value.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ",")
}

func TestRunSecurityHelpIncludesSecureSurface(t *testing.T) {
	var stdout bytes.Buffer
	if rc := runSecurity([]string{"help"}, &stdout, &bytes.Buffer{}); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	for _, want := range []string{"startup-scan", "warnings", "audit", "mode", "client <name>", "command-check", "shims status", "harden npm", "quarantine", "rules list|update", "output-plan", "cra-scaffold"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help missing %q; got:\n%s", want, stdout.String())
		}
	}
}

func TestRunSecurityShimsInstallAndStatus(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "shims")
	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"shims", "install", "--dir", dir}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("install rc=%d stderr=%s", rc, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "git")); err != nil {
		t.Fatalf("expected git shim: %v", err)
	}
	if !strings.Contains(stdout.String(), "[security] shims:") || !strings.Contains(stdout.String(), "dir: "+dir) {
		t.Fatalf("install output missing summary:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	rc = runSecurity([]string{"shims", "status", "--dir", dir}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("status rc=%d stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "installed:") || !strings.Contains(stdout.String(), "missing optional real tools:") {
		t.Fatalf("status output missing readiness details:\n%s", stdout.String())
	}
}

func TestRunSecurityShimsStatusJSON(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "shims")
	if rc := runSecurity([]string{"shims", "install", "--dir", dir}, &bytes.Buffer{}, &bytes.Buffer{}); rc != 0 {
		t.Fatalf("install rc=%d", rc)
	}
	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"shims", "status", "--dir", dir, "--json"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("status rc=%d stderr=%s", rc, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("status JSON decode: %v\n%s", err, stdout.String())
	}
	if result["dir"] != dir {
		t.Fatalf("dir = %#v, want %q", result["dir"], dir)
	}
	if got, ok := result["installed"].(float64); !ok || got == 0 {
		t.Fatalf("installed = %#v, want positive count", result["installed"])
	}
}

func TestRunSecurityCRAScaffoldCreatesMissingEvidence(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"cra-scaffold", "--workspace", workspace}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("expected rc=0, got %d; stderr:\n%s", rc, stderr.String())
	}
	for _, rel := range []string{"SECURITY.md", "SUPPORT.md", "docs/update-policy.md", "docs/cra-technical-file.md"} {
		if _, err := os.Stat(filepath.Join(workspace, rel)); err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
		if !strings.Contains(stdout.String(), rel) {
			t.Fatalf("stdout missing %s:\n%s", rel, stdout.String())
		}
	}
	if !strings.Contains(stdout.String(), "4 created") {
		t.Fatalf("stdout missing created summary:\n%s", stdout.String())
	}
}

func TestRunSecurityCRAScaffoldDryRun(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"cra-scaffold", "--workspace", workspace, "--dry-run"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("expected rc=0, got %d; stderr:\n%s", rc, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(workspace, "SECURITY.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not write SECURITY.md, stat err=%v", err)
	}
	if !strings.Contains(stdout.String(), "would create SECURITY.md") {
		t.Fatalf("stdout missing dry-run action:\n%s", stdout.String())
	}
}

func TestRunSecurityClientCallsRPC(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.client_profile": map[string]any{"client": "claude", "warnings": []any{"x", "y"}},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"client", "claude"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d; stdout:\n%s", rc, stdout.String())
	}
	call := <-calls
	if call.Method != "security.client_profile" {
		t.Fatalf("expected security.client_profile, got %q", call.Method)
	}
	if client, _ := call.Params["client"].(string); client != "claude" {
		t.Fatalf("expected client=claude params, got %#v", call.Params)
	}
	if !strings.Contains(stdout.String(), "2 warning(s)") {
		t.Errorf("client output should summarize warnings; got:\n%s", stdout.String())
	}
}

func TestRunSecurityCommandCheckCallsRPC(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.command_check": map[string]any{
			"decision":        "block",
			"reason":          "command changes dependencies",
			"risk_categories": []any{"package-install"},
		},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"command-check", "--mode", "strict", "--client", "codex", "--", "npm", "install", "left-pad"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d; stdout:\n%s", rc, stdout.String())
	}
	call := <-calls
	if call.Method != "security.command_check" {
		t.Fatalf("expected security.command_check, got %q", call.Method)
	}
	if command, _ := call.Params["command"].(string); command != "npm install left-pad" {
		t.Fatalf("expected joined command params, got %#v", call.Params)
	}
	argv, _ := call.Params["argv"].([]any)
	if got := joinSecurityTestStrings(argv); got != "npm,install,left-pad" {
		t.Fatalf("argv = %#v, want npm,install,left-pad", call.Params["argv"])
	}
	if mode, _ := call.Params["mode"].(string); mode != "strict" {
		t.Fatalf("expected mode=strict params, got %#v", call.Params)
	}
	if client, _ := call.Params["client"].(string); client != "codex" {
		t.Fatalf("expected client=codex params, got %#v", call.Params)
	}
	if !strings.Contains(stdout.String(), "decision: block") || !strings.Contains(stdout.String(), "package-install") {
		t.Errorf("command-check output should summarize decision and risks; got:\n%s", stdout.String())
	}
}

func TestRunSecurityCommandCheckQuotesCommandTextAndPreservesArgv(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.command_check": map[string]any{"decision": "allow", "reason": "ok"},
	})

	var stdout bytes.Buffer
	rc := runSecurity([]string{"command-check", "--", "bash", "-lc", "printf '%s\n' \"$HOME\""}, &stdout, &bytes.Buffer{}, sock)
	if rc != 0 {
		t.Fatalf("expected rc=0, got %d; stdout:\n%s", rc, stdout.String())
	}
	call := <-calls
	if command, _ := call.Params["command"].(string); command != "bash -lc 'printf '\\''%s\n'\\'' \"$HOME\"'" {
		t.Fatalf("command = %q; params=%#v", command, call.Params)
	}
	argv, _ := call.Params["argv"].([]any)
	if got := joinSecurityTestStrings(argv); got != "bash,-lc,printf '%s\n' \"$HOME\"" {
		t.Fatalf("argv = %#v", call.Params["argv"])
	}
}

func TestRunSecurityAuditCallsRPC(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.policy_audit": map[string]any{
			"events": []any{
				map[string]any{
					"created_at": "2026-05-14T10:11:12Z",
					"decision":   "block",
					"mode":       "strict",
					"client":     "codex",
					"session_id": "session-1",
					"command":    "npm install left-pad",
				},
			},
		},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"audit", "--workspace", "/repo", "--session", "session-1", "--client", "codex", "--limit", "5"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d; stdout:\n%s", rc, stdout.String())
	}
	call := <-calls
	if call.Method != "security.policy_audit" {
		t.Fatalf("expected security.policy_audit, got %q", call.Method)
	}
	if call.Params["workspace"] != "/repo" || call.Params["session_id"] != "session-1" || call.Params["client"] != "codex" {
		t.Fatalf("unexpected audit params: %#v", call.Params)
	}
	if !strings.Contains(stdout.String(), "1 policy decision(s)") || !strings.Contains(stdout.String(), "codex/session-1") || !strings.Contains(stdout.String(), "npm install left-pad") {
		t.Errorf("audit output should summarize event; got:\n%s", stdout.String())
	}
}

func TestRunSecurityShimExecBrokersAndRunsAllowedCommand(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "git")
	if err := os.WriteFile(real, []byte("#!/bin/sh\necho real:$1\n"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.policy_decide": map[string]any{"decision": "allow", "reason": "ok"},
	})
	t.Setenv("MILLIWAYS_SECURITY_SHIM_COMMAND", "git")
	t.Setenv("MILLIWAYS_SECURITY_SHIM_CATEGORY", "vcs")
	t.Setenv("MILLIWAYS_CLIENT_ID", "codex")
	t.Setenv("MILLIWAYS_SESSION_ID", "sess-1")
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", dir)

	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"shim-exec", "--", real, "status"}, &stdout, &stderr, sock)
	if rc != 0 {
		t.Fatalf("rc = %d, stderr=%s", rc, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "real:status" {
		t.Fatalf("stdout = %q, want real:status", got)
	}
	call := <-calls
	if call.Method != "security.policy_decide" {
		t.Fatalf("method = %q, want security.policy_decide", call.Method)
	}
	if got, _ := call.Params["command"].(string); got != "git status" {
		t.Fatalf("command = %q, want git status; params=%#v", got, call.Params)
	}
	if got, _ := call.Params["client"].(string); got != "codex" {
		t.Fatalf("client = %q, want codex", got)
	}
}

func TestRunSecurityShimExecBlocksDeniedCommand(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "curl")
	if err := os.WriteFile(real, []byte("#!/bin/sh\necho should-not-run\n"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}
	sock, _ := startSecurityRPCTestServer(t, map[string]any{
		"security.policy_decide": map[string]any{"decision": "block", "reason": "network blocked"},
	})
	t.Setenv("MILLIWAYS_SECURITY_SHIM_COMMAND", "curl")

	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"shim-exec", "--", real, "https://example.com"}, &stdout, &stderr, sock)
	if rc != 126 {
		t.Fatalf("rc = %d, want 126; stderr=%s", rc, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "network blocked") {
		t.Fatalf("stderr missing block reason: %s", stderr.String())
	}
}

func TestRunSecurityShimExecMarksNonInteractiveBroker(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "curl")
	if err := os.WriteFile(real, []byte("#!/bin/sh\necho should-not-run\n"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}
	stdinPath := filepath.Join(dir, "stdin")
	if err := os.WriteFile(stdinPath, nil, 0o644); err != nil {
		t.Fatalf("write stdin file: %v", err)
	}
	stdinFile, err := os.Open(stdinPath)
	if err != nil {
		t.Fatalf("open stdin file: %v", err)
	}
	defer stdinFile.Close()
	oldStdin := os.Stdin
	os.Stdin = stdinFile
	t.Cleanup(func() { os.Stdin = oldStdin })

	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.policy_decide": map[string]any{"decision": "block", "reason": "confirmation required but broker is non-interactive"},
	})
	t.Setenv("MILLIWAYS_SECURITY_SHIM_COMMAND", "curl")

	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"shim-exec", "--", real, "https://example.com"}, &stdout, &stderr, sock)
	if rc != 126 {
		t.Fatalf("rc = %d, want 126; stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if got, _ := call.Params["broker_interactive"].(bool); got {
		t.Fatalf("broker_interactive = true, want false in non-interactive test process; params=%#v", call.Params)
	}
	if got, _ := call.Params["enforcement_level"].(string); got != "brokered" {
		t.Fatalf("enforcement_level = %q, want brokered", got)
	}
	if !strings.Contains(stderr.String(), "confirmation required but broker is non-interactive") {
		t.Fatalf("stderr missing non-interactive reason: %s", stderr.String())
	}
}

func TestRunSecurityShimExecBlocksUnavailablePolicyByDefault(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "git")
	if err := os.WriteFile(real, []byte("#!/bin/sh\necho should-not-run\n"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}
	t.Setenv("MILLIWAYS_SECURITY_SHIM_COMMAND", "git")

	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"shim-exec", "--", real, "status"}, &stdout, &stderr, filepath.Join(dir, "missing.sock"))
	if rc != 126 {
		t.Fatalf("rc = %d, want 126; stderr=%s", rc, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "policy unavailable; blocked by default") {
		t.Fatalf("stderr missing fail-closed reason: %s", stderr.String())
	}
}

func TestRunSecurityShimExecFailsOpenWhenExplicit(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "git")
	if err := os.WriteFile(real, []byte("#!/bin/sh\necho real:$1\n"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}
	t.Setenv("MILLIWAYS_SECURITY_SHIM_COMMAND", "git")
	t.Setenv("MILLIWAYS_SHIM_FAIL_OPEN", "1")

	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"shim-exec", "--", real, "status"}, &stdout, &stderr, filepath.Join(dir, "missing.sock"))
	if rc != 0 {
		t.Fatalf("rc = %d, want 0; stderr=%s", rc, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "real:status" {
		t.Fatalf("stdout = %q, want real:status", got)
	}
	if !strings.Contains(stderr.String(), "MILLIWAYS_SHIM_FAIL_OPEN=1") {
		t.Fatalf("stderr missing fail-open reason: %s", stderr.String())
	}
}

func TestRunSecurityShimExecDoesNotPreserveShimEnvToRealProcess(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "env.out")
	real := filepath.Join(dir, "git")
	if err := os.WriteFile(real, []byte(`#!/bin/sh
{
	printf 'active=%s\n' "$MILLIWAYS_SECURITY_SHIM_ACTIVE"
	printf 'command=%s\n' "$MILLIWAYS_SECURITY_SHIM_COMMAND"
	printf 'category=%s\n' "$MILLIWAYS_SECURITY_SHIM_CATEGORY"
	printf 'shimdir=%s\n' "$MILLIWAYS_SECURITY_SHIM_DIR"
	printf 'resolved=%s\n' "$MILLIWAYS_SECURITY_SHIM_RESOLVED"
	printf 'original=%s\n' "$MILLIWAYS_SECURITY_SHIM_ORIGINAL_PATH"
	printf 'broker=%s\n' "$MILLIWAYS_SECURITY_SHIM_BROKER"
} > "$MW_TEST_OUT"
`), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}
	sock, _ := startSecurityRPCTestServer(t, map[string]any{
		"security.policy_decide": map[string]any{"decision": "allow", "reason": "ok"},
	})
	t.Setenv("MW_TEST_OUT", out)
	t.Setenv("MILLIWAYS_SECURITY_SHIM_ACTIVE", "1")
	t.Setenv("MILLIWAYS_SECURITY_SHIM_COMMAND", "git")
	t.Setenv("MILLIWAYS_SECURITY_SHIM_CATEGORY", "vcs")
	t.Setenv("MILLIWAYS_SECURITY_SHIM_DIR", filepath.Join(dir, "shims"))
	t.Setenv("MILLIWAYS_SECURITY_SHIM_RESOLVED", real)
	t.Setenv("MILLIWAYS_SECURITY_SHIM_ORIGINAL_PATH", os.Getenv("PATH"))
	t.Setenv("MILLIWAYS_SECURITY_SHIM_BROKER", "evilctl")

	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"shim-exec", "--", real, "status"}, &stdout, &stderr, sock)
	if rc != 0 {
		t.Fatalf("rc = %d, stderr=%s", rc, stderr.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read real env output: %v", err)
	}
	for _, want := range []string{
		"active=\n",
		"command=\n",
		"category=\n",
		"shimdir=\n",
		"resolved=\n",
		"original=\n",
		"broker=\n",
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("real env output missing %q:\n%s", want, string(got))
		}
	}
}

func TestRunSecurityShimExecDerivesWorkspaceFromCWDWhenEnvSpoofed(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	spoofed := filepath.Join(root, "spoofed")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.MkdirAll(spoofed, 0o755); err != nil {
		t.Fatalf("mkdir spoofed: %v", err)
	}
	real := filepath.Join(root, "git")
	if err := os.WriteFile(real, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir workspace: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.policy_decide": map[string]any{"decision": "allow", "reason": "ok"},
	})
	t.Setenv("MILLIWAYS_SECURITY_SHIM_COMMAND", "git")
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", spoofed)

	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"shim-exec", "--", real, "status"}, &stdout, &stderr, sock)
	if rc != 0 {
		t.Fatalf("rc = %d, stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if got, _ := call.Params["workspace"].(string); got != workspace {
		t.Fatalf("workspace = %q, want cwd-derived %q; params=%#v", got, workspace, call.Params)
	}
}

func TestRunSecurityShimExecRejectsMismatchedResolvedBinary(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "sh")
	if err := os.WriteFile(real, []byte("#!/bin/sh\necho should-not-run\n"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}
	t.Setenv("MILLIWAYS_SECURITY_SHIM_COMMAND", "git")

	var stdout, stderr bytes.Buffer
	rc := runSecurity([]string{"shim-exec", "--", real, "status"}, &stdout, &stderr, filepath.Join(dir, "missing.sock"))
	if rc != 126 {
		t.Fatalf("rc = %d, want 126; stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), "does not match shim command") {
		t.Fatalf("stderr missing mismatch reason: %s", stderr.String())
	}
}

func TestRunSecurityScanUsesExistingRPC(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.scan": map[string]any{"findings": []any{}},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"scan"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d; stdout:\n%s", rc, stdout.String())
	}
	call := <-calls
	if call.Method != "security.scan" {
		t.Fatalf("expected security.scan, got %q", call.Method)
	}
	if !strings.Contains(stdout.String(), "0 finding(s)") {
		t.Errorf("scan output should summarize findings; got:\n%s", stdout.String())
	}
}

func TestRunSecurityScanJSONKeepsExistingRPCParams(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.scan": map[string]any{"findings": []any{}},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"scan", "--json"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d; stdout:\n%s", rc, stdout.String())
	}
	call := <-calls
	if call.Method != "security.scan" {
		t.Fatalf("expected security.scan, got %q", call.Method)
	}
	if len(call.Params) != 0 {
		t.Fatalf("security scan --json params = %#v, want empty for compatibility", call.Params)
	}
	if !strings.Contains(stdout.String(), `"findings": []`) {
		t.Errorf("scan JSON should render raw scan result; got:\n%s", stdout.String())
	}
}

func TestRunSecurityScanFlagsLayerRPCs(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.startup_scan":   map[string]any{"warnings": []any{}},
		"security.client_profile": map[string]any{"client": "codex", "warnings": []any{"profile warning"}},
		"security.scan":           map[string]any{"findings": []any{}},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"scan", "--startup", "--client", "codex", "--diff", "--secrets", "--sast"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d; stdout:\n%s", rc, stdout.String())
	}
	startupCall := <-calls
	clientCall := <-calls
	scanCall := <-calls
	if startupCall.Method != "security.startup_scan" {
		t.Fatalf("first method = %q, want security.startup_scan", startupCall.Method)
	}
	if clientCall.Method != "security.client_profile" {
		t.Fatalf("second method = %q, want security.client_profile", clientCall.Method)
	}
	if client, _ := clientCall.Params["client"].(string); client != "codex" {
		t.Fatalf("client params = %#v, want codex", clientCall.Params)
	}
	if scanCall.Method != "security.scan" {
		t.Fatalf("third method = %q, want security.scan", scanCall.Method)
	}
	layers, _ := scanCall.Params["layers"].([]any)
	if got := joinSecurityTestStrings(layers); got != "secret,sast,dependency" {
		t.Fatalf("layers = %#v (%q), want secret,sast,dependency", scanCall.Params["layers"], got)
	}
	if staged, _ := scanCall.Params["staged"].(bool); !staged {
		t.Fatalf("staged param = %#v, want true", scanCall.Params)
	}
	if diff, _ := scanCall.Params["diff"].(string); diff != "staged" {
		t.Fatalf("diff param = %#v, want staged", scanCall.Params)
	}
	for _, want := range []string{"security startup-scan", "security client", "security scan"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunSecurityStartupScanPassesStrict(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.startup_scan": map[string]any{"warnings": []any{"x"}},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"startup-scan", "--strict"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d; stdout:\n%s", rc, stdout.String())
	}
	call := <-calls
	if call.Method != "security.startup_scan" {
		t.Fatalf("expected security.startup_scan, got %q", call.Method)
	}
	if strict, _ := call.Params["strict"].(bool); !strict {
		t.Fatalf("expected strict=true params, got %#v", call.Params)
	}
	if !strings.Contains(stdout.String(), "1 warning(s)") {
		t.Errorf("startup-scan output should summarize warnings; got:\n%s", stdout.String())
	}
}

func TestRunSecurityModeValidatesAndCallsRPC(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.mode": map[string]any{"mode": "strict"},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"mode", "strict"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	call := <-calls
	if call.Method != "security.mode" {
		t.Fatalf("expected security.mode, got %q", call.Method)
	}
	if mode, _ := call.Params["mode"].(string); mode != "strict" {
		t.Fatalf("expected mode=strict params, got %#v", call.Params)
	}

	var stderr bytes.Buffer
	if rc := runSecurity([]string{"mode", "panic"}, &bytes.Buffer{}, &stderr, sock); rc == 0 {
		t.Fatalf("expected invalid mode to fail")
	}
	if !strings.Contains(stderr.String(), "invalid mode") {
		t.Errorf("expected invalid mode error, got:\n%s", stderr.String())
	}
}

func TestRunSecurityQuarantineDefaultsToDryRun(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.quarantine": map[string]any{"actions": []any{"move"}},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"quarantine"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	call := <-calls
	if call.Method != "security.quarantine" {
		t.Fatalf("expected security.quarantine, got %q", call.Method)
	}
	if dryRun, _ := call.Params["dry_run"].(bool); !dryRun {
		t.Fatalf("expected dry_run=true params, got %#v", call.Params)
	}
	if apply, _ := call.Params["apply"].(bool); apply {
		t.Fatalf("expected apply=false params, got %#v", call.Params)
	}
	if !strings.Contains(stdout.String(), "1 action(s)") {
		t.Errorf("quarantine output should summarize actions; got:\n%s", stdout.String())
	}
}

func TestRunSecurityOutputPlanClassifiesPathsAsJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rc := runSecurity([]string{
		"output-plan",
		"--json",
		"--generated", "cmd/app/main.go",
		"--generated", "package-lock.json",
		"--staged", ".env.local",
		"--staged", "README.md",
	}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("expected rc=0, got %d; stderr:\n%s", rc, stderr.String())
	}

	var plan struct {
		Requests []struct {
			Kind   string   `json:"kind"`
			Files  []string `json:"files"`
			Reason string   `json:"reason"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("Unmarshal output-plan JSON: %v\n%s", err, stdout.String())
	}

	got := map[string][]string{}
	for _, req := range plan.Requests {
		got[req.Kind] = req.Files
		if req.Reason == "" {
			t.Fatalf("request for %s has empty reason", req.Kind)
		}
	}
	want := map[string][]string{
		"secret":     {".env.local", "cmd/app/main.go", "package-lock.json"},
		"sast":       {"cmd/app/main.go"},
		"dependency": {"package-lock.json"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("output-plan = %#v, want %#v", got, want)
	}
}

func TestRunSecurityOutputPlanRendersNoScans(t *testing.T) {
	var stdout bytes.Buffer
	if rc := runSecurity([]string{"output-plan", "--generated", "docs/guide.md"}, &stdout, &bytes.Buffer{}); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	if !strings.Contains(stdout.String(), "no scans requested") {
		t.Fatalf("expected no-scan summary, got:\n%s", stdout.String())
	}
}

func TestRunSecurityOutputPlanRejectsPositionalPaths(t *testing.T) {
	var stderr bytes.Buffer
	if rc := runSecurity([]string{"output-plan", "cmd/app/main.go"}, &bytes.Buffer{}, &stderr); rc == 0 {
		t.Fatalf("expected positional path to fail")
	}
	if !strings.Contains(stderr.String(), "use --generated or --staged") {
		t.Fatalf("expected flag guidance, got:\n%s", stderr.String())
	}
}

func TestRunSecurityPrecommitPlanReadsGitStagedChanges(t *testing.T) {
	prevGitOutput := securityGitOutput
	t.Cleanup(func() { securityGitOutput = prevGitOutput })

	var gotName string
	var gotArgs []string
	securityGitOutput = func(name string, args ...string) ([]byte, error) {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return []byte("A\x00.env.local\x00M\x00cmd/app/main.go\x00D\x00go.sum\x00R100\x00old.js\x00src/new.ts\x00"), nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rc := runSecurity([]string{"precommit-plan", "--json"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("expected rc=0, got %d; stderr:\n%s", rc, stderr.String())
	}
	if gotName != "git" || !reflect.DeepEqual(gotArgs, []string{"diff", "--cached", "--name-status", "-z"}) {
		t.Fatalf("git command = %s %#v, want git diff --cached --name-status -z", gotName, gotArgs)
	}

	var plan struct {
		Requests []struct {
			Kind  string   `json:"kind"`
			Files []string `json:"files"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("Unmarshal precommit-plan JSON: %v\n%s", err, stdout.String())
	}

	got := map[string][]string{}
	for _, req := range plan.Requests {
		got[req.Kind] = req.Files
	}
	want := map[string][]string{
		"secret": {".env.local", "cmd/app/main.go", "src/new.ts"},
		"sast":   {"cmd/app/main.go", "src/new.ts"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("precommit-plan = %#v, want %#v", got, want)
	}
}

func TestRunSecurityPrecommitPlanStagedFallbackSkipsGit(t *testing.T) {
	prevGitOutput := securityGitOutput
	t.Cleanup(func() { securityGitOutput = prevGitOutput })
	securityGitOutput = func(string, ...string) ([]byte, error) {
		t.Fatal("git should not be called when --staged paths are supplied")
		return nil, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rc := runSecurity([]string{"precommit-plan", "--json", "--staged", "package-lock.json", "--staged", "README.md"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("expected rc=0, got %d; stderr:\n%s", rc, stderr.String())
	}

	var plan struct {
		Requests []struct {
			Kind  string   `json:"kind"`
			Files []string `json:"files"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("Unmarshal precommit-plan JSON: %v\n%s", err, stdout.String())
	}

	got := map[string][]string{}
	for _, req := range plan.Requests {
		got[req.Kind] = req.Files
	}
	want := map[string][]string{
		"secret":     {"package-lock.json"},
		"dependency": {"package-lock.json"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("precommit-plan fallback = %#v, want %#v", got, want)
	}
}

func TestRunSecurityPrecommitPlanReportsGitFailure(t *testing.T) {
	prevGitOutput := securityGitOutput
	t.Cleanup(func() { securityGitOutput = prevGitOutput })
	securityGitOutput = func(string, ...string) ([]byte, error) {
		return nil, fmt.Errorf("not a git repo")
	}

	var stderr bytes.Buffer
	if rc := runSecurity([]string{"precommit-plan"}, &bytes.Buffer{}, &stderr); rc == 0 {
		t.Fatalf("expected git failure to fail")
	}
	if !strings.Contains(stderr.String(), "read staged files") || !strings.Contains(stderr.String(), "--staged") {
		t.Fatalf("expected staged fallback guidance, got:\n%s", stderr.String())
	}
}

func TestRunSecurityOutputPlanRendersSBOMRefreshRecommendation(t *testing.T) {
	var stdout bytes.Buffer

	if rc := runSecurity([]string{"output-plan", "--generated", "package-lock.json"}, &stdout, &bytes.Buffer{}); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	for _, want := range []string{
		"dependency: package-lock.json",
		"recommend: Generated dependency file changed; refresh SBOM evidence",
		"milliwaysctl security sbom --output dist/milliways.spdx.json",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("output-plan missing %q; got:\n%s", want, stdout.String())
		}
	}
}

func TestRunSecurityStatusRendersExtendedFields(t *testing.T) {
	sock, _ := startSecurityRPCTestServer(t, map[string]any{
		"security.status": map[string]any{
			"installed":               false,
			"enabled":                 true,
			"mode":                    "warn",
			"security_workspace":      "/repo/service",
			"posture":                 "warn",
			"warning_count":           2,
			"block_count":             1,
			"last_startup_scan_at":    "2026-05-14T10:00:00Z",
			"last_dependency_scan_at": "2026-05-14T10:02:00Z",
			"shims": map[string]any{
				"ready":            false,
				"installed":        2,
				"expected":         3,
				"broker_installed": false,
				"broker_command":   "milliwaysctl",
				"missing_shims":    []any{"npm"},
			},
			"client_enforcement": map[string]any{
				"codex":   map[string]any{"level": "brokered", "controlled_env": true, "broker_path": "/tmp/security-shims"},
				"claude":  map[string]any{"level": "brokered", "controlled_env": true},
				"minimax": map[string]any{"level": "full"},
				"custom":  map[string]any{"level": "unknown"},
			},
			"rulepacks": map[string]any{
				"count":        1,
				"update_state": "offline-current",
				"offline":      true,
				"packs": []any{
					map[string]any{"name": "workspace-ioc", "version": "1.2.3", "source": "workspace", "status": "loaded"},
				},
			},
			"scanners": []any{
				map[string]any{"name": "osv-scanner", "installed": true, "version": "osv-scanner 2.0.0"},
				map[string]any{"name": "gitleaks", "installed": false},
				map[string]any{"name": "semgrep", "installed": true},
				map[string]any{"name": "govulncheck", "installed": false},
			},
		},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"status"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	for _, want := range []string{
		"mode: warn",
		"workspace: /repo/service",
		"posture: WARN",
		"warnings: 2  blocks: 1",
		"last startup scan",
		"last dependency scan",
		"rulepacks: 1 loaded (offline-current)",
		"workspace-ioc@1.2.3",
		"scanners: installed osv-scanner (osv-scanner 2.0.0), semgrep; missing gitleaks, govulncheck",
		"shims: not ready 2/3; missing broker milliwaysctl; missing npm",
		"claude unprotected (brokered, shim not ready)",
		"codex unprotected (brokered, shim not ready)",
		"custom unprotected (unknown)",
		"minimax protected (full)",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("status missing %q; got:\n%s", want, stdout.String())
		}
	}
}

func TestRunSecurityStatusRendersProtectedBrokeredClientsWhenShimsReady(t *testing.T) {
	sock, _ := startSecurityRPCTestServer(t, map[string]any{
		"security.status": map[string]any{
			"installed": true,
			"enabled":   true,
			"shims": map[string]any{
				"ready":            true,
				"installed":        3,
				"expected":         3,
				"broker_installed": true,
				"broker_path":      "/run/milliways/security-shims/milliwaysctl",
			},
			"client_enforcement": map[string]any{
				"codex":  map[string]any{"level": "brokered", "controlled_env": true},
				"custom": map[string]any{"level": "brokered"},
			},
		},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"status"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	for _, want := range []string{
		"shims: ready 3/3; broker /run/milliways/security-shims/milliwaysctl",
		"codex protected (brokered, shim ready)",
		"custom unprotected (brokered, shim ready)",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("status missing %q; got:\n%s", want, stdout.String())
		}
	}
}

func TestRunSecurityCRARendersReadinessAndGaps(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"security.cra": map[string]any{
			"workspace": "/repo",
			"summary": map[string]any{
				"evidence_score":         67,
				"checks_present":         3,
				"checks_total":           6,
				"checks_partial":         2,
				"checks_missing":         1,
				"reporting_present":      2,
				"reporting_total":        3,
				"reporting_ready":        false,
				"design_evidence_status": "partial",
				"security_warnings":      2,
				"security_blocks":        1,
			},
			"checks": []any{
				map[string]any{
					"id":               "cra-vulnerability-handling",
					"title":            "Vulnerability handling and reporting evidence",
					"status":           "partial",
					"missing_evidence": []any{"vulnerability_reporting_process"},
					"next_actions":     []any{"Document vulnerability reporting in SECURITY.md"},
				},
				map[string]any{
					"id":     "cra-sbom",
					"title":  "SBOM evidence",
					"status": "present",
				},
			},
		},
	})

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"cra"}, &stdout, &bytes.Buffer{}, sock); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	call := <-calls
	if call.Method != "security.cra" {
		t.Fatalf("expected security.cra, got %q", call.Method)
	}
	for _, want := range []string{
		"CRA readiness",
		"workspace: /repo",
		"evidence: 67% (3/6 present, 2 partial, 1 missing)",
		"vulnerability/reporting: 2/3 not ready",
		"security issues: 2 warnings, 1 blocks",
		"design evidence: partial",
		"WARN  cra-vulnerability-handling",
		"missing: vulnerability_reporting_process",
		"next: Document vulnerability reporting in SECURITY.md",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("security cra missing %q; got:\n%s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "Article 14") || strings.Contains(stdout.String(), "2026-09-11") {
		t.Fatalf("security cra should not render Article 14 date metadata:\n%s", stdout.String())
	}
}

func TestRunSecuritySBOMWritesSPDX(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.test/app\n\nrequire github.com/acme/lib v1.2.3\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if rc := runSecurity([]string{"sbom", "--workspace", workspace}, &stdout, &stderr, ""); rc != 0 {
		t.Fatalf("expected rc=0, got %d; stderr:\n%s", rc, stderr.String())
	}
	for _, want := range []string{`"spdxVersion": "SPDX-2.3"`, `"name": "github.com/acme/lib"`, `"versionInfo": "v1.2.3"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("sbom output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunSecuritySBOMWritesOutputFile(t *testing.T) {
	workspace := t.TempDir()
	outPath := filepath.Join(workspace, "dist", "milliways.spdx.json")

	var stdout, stderr bytes.Buffer
	if rc := runSecurity([]string{"sbom", "--workspace", workspace, "--output", outPath}, &stdout, &stderr, ""); rc != 0 {
		t.Fatalf("expected rc=0, got %d; stderr:\n%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote SBOM") {
		t.Fatalf("missing success output:\n%s", stdout.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile output: %v", err)
	}
	if !strings.Contains(string(data), `"spdxVersion": "SPDX-2.3"`) {
		t.Fatalf("output is not SPDX JSON:\n%s", string(data))
	}
}

func TestRunSecurityHardenNPMDryRunDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".npmrc")
	var stdout bytes.Buffer
	if rc := runSecurity([]string{"harden", "npm", "--path", path}, &stdout, &bytes.Buffer{}); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create %s", path)
	}
	if !strings.Contains(stdout.String(), "ignore-scripts=true") {
		t.Errorf("dry-run should preview npm settings; got:\n%s", stdout.String())
	}
}

func TestRunSecurityHardenNPMApplyAppendsMissingSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".npmrc")
	if err := os.WriteFile(path, []byte("audit=false\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout bytes.Buffer
	if rc := runSecurity([]string{"harden", "npm", "--apply", "--path", path}, &stdout, &bytes.Buffer{}); rc != 0 {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	gotBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got := string(gotBytes)
	for _, want := range []string{"audit=false", "ignore-scripts=true", "fund=false", "package-lock=true"} {
		if !strings.Contains(got, want) {
			t.Errorf(".npmrc missing %q; got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "audit=true") {
		t.Errorf("harden should not override an existing audit setting; got:\n%s", got)
	}
}
