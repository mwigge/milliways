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
	sock := filepath.Join(t.TempDir(), "mw.sock")
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
	for _, want := range []string{"startup-scan", "warnings", "mode", "client <name>", "command-check", "harden npm", "quarantine", "rules list|update", "output-plan", "cra-scaffold"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help missing %q; got:\n%s", want, stdout.String())
		}
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
	for _, want := range []string{"mode: warn", "workspace: /repo/service", "posture: WARN", "warnings: 2  blocks: 1", "last startup scan", "last dependency scan", "scanners: installed osv-scanner (osv-scanner 2.0.0), semgrep; missing gitleaks, govulncheck"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("status missing %q; got:\n%s", want, stdout.String())
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
				"reporting_deadline":     "2026-09-11",
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
		"design evidence: partial",
		"Article 14 reporting: 2026-09-11",
		"WARN  cra-vulnerability-handling",
		"missing: vulnerability_reporting_process",
		"next: Document vulnerability reporting in SECURITY.md",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("security cra missing %q; got:\n%s", want, stdout.String())
		}
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
