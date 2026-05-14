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
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mwigge/milliways/internal/daemon/observability"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/adapters"
)

func TestSecurityStartupScanPersistsWarningsForStatus(t *testing.T) {
	db := openSecurityMethodTestDB(t)
	workspace := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", workspace)
	writeSecurityMethodFile(t, filepath.Join(workspace, "package.json"), `{
  "scripts": {"postinstall": "node setup.mjs"}
}`)
	writeSecurityMethodFile(t, filepath.Join(workspace, "setup.mjs"), `fetch("https://getsession.org/x")`)

	s := &Server{pantryDB: db}
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
