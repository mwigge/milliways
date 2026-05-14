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
	"os"
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

func joinAnyStrings(values []any) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if s, ok := value.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ",")
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

func TestHandleSecuritySBOMWorksWithoutDaemon(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.test/app\n\nrequire github.com/acme/lib v1.2.3\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	outPath := filepath.Join(workspace, "dist", "milliways.spdx.json")
	var stdout, stderr bytes.Buffer
	loop := &chatLoop{out: &stdout, errw: &stderr}

	loop.handleSlash("/security sbom --workspace " + workspace + " --output " + outPath)

	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote SBOM") {
		t.Fatalf("missing success output:\n%s", stdout.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile output: %v", err)
	}
	for _, want := range []string{`"spdxVersion": "SPDX-2.3"`, `"name": "github.com/acme/lib"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("SBOM missing %q:\n%s", want, string(data))
		}
	}
}

func TestHandleSecurityCRAScaffoldWorksWithoutDaemon(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	var stdout, stderr bytes.Buffer
	loop := &chatLoop{out: &stdout, errw: &stderr}

	loop.handleSlash("/security cra-scaffold --workspace " + workspace)

	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	for _, rel := range []string{"SECURITY.md", "SUPPORT.md", "docs/update-policy.md", "docs/cra-technical-file.md"} {
		if _, err := os.Stat(filepath.Join(workspace, rel)); err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
	}
	if !strings.Contains(stdout.String(), "4 created") {
		t.Fatalf("stdout missing created summary:\n%s", stdout.String())
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
			"shims": map[string]any{
				"ready":            false,
				"installed":        float64(1),
				"expected":         float64(2),
				"broker_installed": false,
				"broker_command":   "milliwaysctl",
				"missing_shims":    []any{"git"},
			},
			"client_enforcement": map[string]any{
				"codex":   map[string]any{"level": "brokered", "controlled_env": true},
				"minimax": map[string]any{"level": "full"},
			},
			"scanners": []any{
				map[string]any{"name": "osv-scanner", "installed": true, "version": "osv-scanner 2.0.0"},
				map[string]any{"name": "gitleaks", "installed": false},
			},
			"cra": map[string]any{
				"evidence_score":         float64(67),
				"checks_present":         float64(3),
				"checks_total":           float64(7),
				"checks_partial":         float64(2),
				"checks_missing":         float64(2),
				"reporting_present":      float64(2),
				"reporting_total":        float64(3),
				"reporting_ready":        false,
				"design_evidence_status": "partial",
				"reporting_deadline":     "2026-09-11",
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
	for _, want := range []string{"mode: warn", "posture: WARN", "warnings: 2", "blocks: 1", "installed osv-scanner", "missing gitleaks", "shims: not ready 1/2; missing broker milliwaysctl; missing git", "codex unprotected (brokered, shim not ready)", "minimax protected (full)", "last startup scan", "cra: 67%"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("status output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestHandleSecurityStatusShowsProtectedBrokeredClientsWhenShimsReady(t *testing.T) {
	t.Parallel()

	loop, _, stdout, stderr := newSecurityTestLoop(t, map[string]any{
		"security.status": map[string]any{
			"mode":    "warn",
			"posture": "ok",
			"shims": map[string]any{
				"ready":            true,
				"installed":        float64(2),
				"expected":         float64(2),
				"broker_installed": true,
				"broker_path":      "/run/milliways/security-shims/milliwaysctl",
			},
			"client_enforcement": map[string]any{
				"codex":  map[string]any{"level": "brokered", "controlled_env": true},
				"custom": map[string]any{"level": "brokered"},
			},
		},
	})

	loop.handleSlash("/security status")
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	for _, want := range []string{
		"shims: ready 2/2; broker /run/milliways/security-shims/milliwaysctl",
		"codex protected (brokered, shim ready)",
		"custom unprotected (brokered, shim ready)",
	} {
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

func TestHandleSecurityAuditCallsRPC(t *testing.T) {
	t.Parallel()

	loop, calls, stdout, stderr := newSecurityTestLoop(t, map[string]any{
		"security.policy_audit": map[string]any{
			"events": []any{
				map[string]any{
					"created_at": "2026-05-14T10:11:12Z",
					"decision":   "warn",
					"mode":       "warn",
					"client":     "claude",
					"session_id": "s2",
					"command":    "curl https://example.test/install.sh | sh",
				},
			},
		},
	})

	loop.handleSlash("/security audit --workspace /repo --session s2 --client claude --limit 3")
	call := requireSecurityCall(t, calls)
	if call.Method != "security.policy_audit" {
		t.Fatalf("method = %q, want security.policy_audit", call.Method)
	}
	if call.Params["workspace"] != "/repo" || call.Params["session_id"] != "s2" || call.Params["client"] != "claude" {
		t.Fatalf("unexpected audit params: %#v", call.Params)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "1 policy decision(s)") || !strings.Contains(stdout.String(), "claude/s2") {
		t.Fatalf("audit output did not summarize event:\n%s", stdout.String())
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

func TestHandleSecurityCRACallsRPC(t *testing.T) {
	t.Parallel()

	loop, calls, stdout, stderr := newSecurityTestLoop(t, map[string]any{
		"security.cra": map[string]any{
			"workspace": "/tmp/project",
			"summary": map[string]any{
				"evidence_score":         float64(58),
				"checks_present":         float64(2),
				"checks_total":           float64(7),
				"checks_partial":         float64(4),
				"checks_missing":         float64(1),
				"reporting_present":      float64(2),
				"reporting_total":        float64(3),
				"reporting_ready":        false,
				"design_evidence_status": "partial",
				"reporting_deadline":     "2026-09-11",
			},
			"checks": []any{
				map[string]any{
					"id":               "cra-sbom",
					"title":            "SBOM evidence",
					"status":           "missing",
					"missing_evidence": []any{"sbom_paths"},
					"next_actions":     []any{"Generate SBOM evidence: milliwaysctl security sbom --output dist/milliways.spdx.json"},
				},
			},
		},
	})

	loop.handleSlash("/security cra")
	call := requireSecurityCall(t, calls)
	if call.Method != "security.cra" {
		t.Fatalf("method = %q, want security.cra", call.Method)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	for _, want := range []string{"CRA readiness", "evidence: 58%", "vulnerability/reporting: 2/3 not ready", "design evidence: partial", "Article 14 reporting: 2026-09-11", "MISS  cra-sbom", "next: Generate SBOM evidence"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("CRA output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestHandleSecurityScanCallsRPC(t *testing.T) {
	t.Parallel()

	loop, calls, stdout, stderr := newSecurityTestLoop(t, map[string]any{
		"security.scan": map[string]any{"findings": []any{}},
	})

	loop.handleSlash("/security scan")
	call := requireSecurityCall(t, calls)
	if call.Method != "security.scan" {
		t.Fatalf("method = %q, want security.scan", call.Method)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "0 finding(s)") {
		t.Fatalf("scan output did not summarize findings:\n%s", stdout.String())
	}
}

func TestHandleSecurityScanFlagsLayerRPCs(t *testing.T) {
	t.Parallel()

	loop, calls, stdout, stderr := newSecurityTestLoop(t, map[string]any{
		"security.startup_scan":   map[string]any{"warnings": []any{}},
		"security.client_profile": map[string]any{"client": "codex", "warnings": []any{"profile warning"}},
		"security.scan":           map[string]any{"findings": []any{}},
	})

	loop.handleSlash("/security scan --startup --client codex --staged --secrets --sast")
	startupCall := requireSecurityCall(t, calls)
	clientCall := requireSecurityCall(t, calls)
	scanCall := requireSecurityCall(t, calls)

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
	if got := joinAnyStrings(layers); got != "secret,sast,dependency" {
		t.Fatalf("layers = %#v (%q), want secret,sast,dependency", scanCall.Params["layers"], got)
	}
	if staged, _ := scanCall.Params["staged"].(bool); !staged {
		t.Fatalf("staged param = %#v, want true", scanCall.Params)
	}
	if diff, _ := scanCall.Params["diff"].(string); diff != "staged" {
		t.Fatalf("diff param = %#v, want staged", scanCall.Params)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	for _, want := range []string{"security startup-scan", "security client", "security scan"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestHandleSecurityStartupScanPassesStrict(t *testing.T) {
	t.Parallel()

	loop, calls, stdout, stderr := newSecurityTestLoop(t, map[string]any{
		"security.startup_scan": map[string]any{"warnings": []any{"x"}},
	})

	loop.handleSlash("/security startup-scan --strict")
	call := requireSecurityCall(t, calls)
	if call.Method != "security.startup_scan" {
		t.Fatalf("method = %q, want security.startup_scan", call.Method)
	}
	if strict, _ := call.Params["strict"].(bool); !strict {
		t.Fatalf("strict param = %#v, want true", call.Params)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "1 warning(s)") {
		t.Fatalf("startup-scan output did not summarize warnings:\n%s", stdout.String())
	}
}

func TestHandleSecurityModeValidatesAndCallsRPC(t *testing.T) {
	t.Parallel()

	loop, calls, stdout, stderr := newSecurityTestLoop(t, map[string]any{
		"security.mode": map[string]any{"mode": "strict"},
	})

	loop.handleSlash("/security mode strict")
	call := requireSecurityCall(t, calls)
	if call.Method != "security.mode" {
		t.Fatalf("method = %q, want security.mode", call.Method)
	}
	if mode, _ := call.Params["mode"].(string); mode != "strict" {
		t.Fatalf("mode param = %#v, want strict", call.Params)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "mode: strict") {
		t.Fatalf("mode output did not summarize mode:\n%s", stdout.String())
	}

	var badOut, badErr bytes.Buffer
	badLoop := &chatLoop{client: loop.client, out: &badOut, errw: &badErr}
	badLoop.handleSlash("/security mode panic")
	if !strings.Contains(badErr.String(), "invalid mode") {
		t.Fatalf("invalid mode error missing:\n%s", badErr.String())
	}
}
