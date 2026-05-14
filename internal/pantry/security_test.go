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

package pantry

import (
	"testing"
	"time"
)

func TestSecurityStore_UpsertAndListActive(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	f := SecurityFinding{
		CVEID:            "CVE-2024-12345",
		PackageName:      "github.com/foo/bar",
		InstalledVersion: "v1.2.0",
		FixedInVersion:   "v1.2.1",
		Severity:         "CRITICAL",
		Ecosystem:        "Go",
		Summary:          "RCE via crafted input",
		ScanSource:       "go.sum",
		Status:           "active",
	}
	if err := ss.UpsertFinding(f); err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}

	active, err := ss.ListActive(nil)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("ListActive len = %d, want 1", len(active))
	}
	if active[0].CVEID != "CVE-2024-12345" {
		t.Errorf("CVEID = %q, want CVE-2024-12345", active[0].CVEID)
	}
}

func TestSecurityStore_UpsertIdempotent(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	f := SecurityFinding{
		CVEID: "CVE-2024-99999", PackageName: "pkg", InstalledVersion: "v1.0",
		Severity: "HIGH", Ecosystem: "Go", Status: "active",
	}
	if err := ss.UpsertFinding(f); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	f.Summary = "updated summary"
	if err := ss.UpsertFinding(f); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	all, err := ss.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("ListAll len = %d, want 1 (no duplicates)", len(all))
	}
	if all[0].Summary != "updated summary" {
		t.Errorf("Summary not updated: %q", all[0].Summary)
	}
	if all[0].Category != "dependency" {
		t.Errorf("Category = %q, want dependency", all[0].Category)
	}
}

func TestSecurityStore_GetByCVE(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	f := SecurityFinding{CVEID: "CVE-2024-11111", PackageName: "p", InstalledVersion: "v1", Severity: "HIGH", Ecosystem: "Go", Status: "active"}
	if err := ss.UpsertFinding(f); err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}

	got, err := ss.GetByCVE("CVE-2024-11111")
	if err != nil {
		t.Fatalf("GetByCVE: %v", err)
	}
	if got.CVEID != "CVE-2024-11111" {
		t.Errorf("CVEID = %q, want CVE-2024-11111", got.CVEID)
	}

	_, err = ss.GetByCVE("CVE-9999-00000")
	if err == nil {
		t.Error("expected error for unknown CVE, got nil")
	}
}

func TestSecurityStore_MarkResolved(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	f := SecurityFinding{CVEID: "CVE-2024-22222", PackageName: "pkg", InstalledVersion: "v1", Severity: "HIGH", Ecosystem: "Go", Status: "active"}
	if err := ss.UpsertFinding(f); err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}

	if err := ss.MarkResolved("CVE-2024-22222", "pkg", "v1", "Go"); err != nil {
		t.Fatalf("MarkResolved: %v", err)
	}

	active, err := ss.ListActive(nil)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	for _, a := range active {
		if a.CVEID == "CVE-2024-22222" {
			t.Error("resolved finding still in ListActive")
		}
	}
}

func TestSecurityStore_ListActive_SeverityFilter(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	for _, sev := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
		f := SecurityFinding{CVEID: "CVE-" + sev, PackageName: "pkg" + sev, InstalledVersion: "v1", Severity: sev, Ecosystem: "Go", Status: "active"}
		if err := ss.UpsertFinding(f); err != nil {
			t.Fatalf("UpsertFinding %s: %v", sev, err)
		}
	}

	active, err := ss.ListActive([]string{"CRITICAL", "HIGH"})
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 2 {
		t.Errorf("ListActive len = %d, want 2 (CRITICAL+HIGH only)", len(active))
	}
	for _, a := range active {
		if a.Severity != "CRITICAL" && a.Severity != "HIGH" {
			t.Errorf("unexpected severity %q in CRITICAL+HIGH filter", a.Severity)
		}
	}
}

func TestSecurityStore_InsertAcceptedRisk(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	f := SecurityFinding{CVEID: "CVE-2024-33333", PackageName: "pkg", InstalledVersion: "v1", Severity: "HIGH", Ecosystem: "Go", Status: "active"}
	if err := ss.UpsertFinding(f); err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}

	expires := time.Now().UTC().Add(30 * 24 * time.Hour)
	if err := ss.InsertAcceptedRisk("CVE-2024-33333", "pkg", "false positive", expires); err != nil {
		t.Fatalf("InsertAcceptedRisk: %v", err)
	}

	risks, err := ss.ListAcceptedRisks()
	if err != nil {
		t.Fatalf("ListAcceptedRisks: %v", err)
	}
	if len(risks) != 1 {
		t.Fatalf("ListAcceptedRisks len = %d, want 1", len(risks))
	}
	if risks[0].CVEID != "CVE-2024-33333" {
		t.Errorf("CVEID = %q, want CVE-2024-33333", risks[0].CVEID)
	}
}

func TestSecurityStore_ListActive_ExcludesAccepted(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	f := SecurityFinding{CVEID: "CVE-2024-44444", PackageName: "pkg", InstalledVersion: "v1", Severity: "HIGH", Ecosystem: "Go", Status: "active"}
	if err := ss.UpsertFinding(f); err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}
	expires := time.Now().UTC().Add(30 * 24 * time.Hour)
	if err := ss.InsertAcceptedRisk("CVE-2024-44444", "pkg", "known issue", expires); err != nil {
		t.Fatalf("InsertAcceptedRisk: %v", err)
	}

	active, err := ss.ListActive(nil)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	for _, a := range active {
		if a.CVEID == "CVE-2024-44444" {
			t.Error("accepted-risk finding still appears in ListActive")
		}
	}
}

func TestSecurityStore_ScanRunLifecycle(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	id, err := ss.InsertScanRun(SecurityScanRun{
		Kind:      "startup",
		Workspace: "/repo",
		ToolName:  "milliways",
	})
	if err != nil {
		t.Fatalf("InsertScanRun: %v", err)
	}
	if id == 0 {
		t.Fatal("InsertScanRun returned id 0")
	}
	if err := ss.CompleteScanRun(id, "completed", 3, 2, 1, ""); err != nil {
		t.Fatalf("CompleteScanRun: %v", err)
	}

	status, err := ss.SecurityStatus("/repo")
	if err != nil {
		t.Fatalf("SecurityStatus: %v", err)
	}
	if status.LastStartupScan == nil {
		t.Fatal("LastStartupScan is nil")
	}
	if status.LastStartupScan.ID != id {
		t.Fatalf("LastStartupScan.ID = %d, want %d", status.LastStartupScan.ID, id)
	}
	if status.LastStartupScan.FindingsTotal != 3 {
		t.Fatalf("FindingsTotal = %d, want 3", status.LastStartupScan.FindingsTotal)
	}
}

func TestSecurityStore_UpsertAndListRulePacks(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	pack := SecurityRulePack{
		Workspace:               "/repo",
		Name:                    "workspace-ioc",
		Version:                 "1.0.0",
		Source:                  "workspace",
		ManifestSource:          "workspace",
		Checksum:                "sha256:abc",
		MinimumMilliWaysVersion: "0.0.0",
		RulesFile:               "rules.yaml",
		RulesCount:              2,
		Root:                    "/repo/.milliways/security/rules/ioc",
		ManifestPath:            "/repo/.milliways/security/rules/ioc/manifest.yaml",
		RulesPath:               "/repo/.milliways/security/rules/ioc/rules.yaml",
	}
	if err := ss.UpsertRulePack(pack); err != nil {
		t.Fatalf("UpsertRulePack: %v", err)
	}
	pack.RulesCount = 3
	if err := ss.UpsertRulePack(pack); err != nil {
		t.Fatalf("second UpsertRulePack: %v", err)
	}

	packs, err := ss.ListRulePacks("/repo")
	if err != nil {
		t.Fatalf("ListRulePacks: %v", err)
	}
	if len(packs) != 1 {
		t.Fatalf("ListRulePacks len = %d, want 1", len(packs))
	}
	if packs[0].RulesCount != 3 {
		t.Fatalf("RulesCount = %d, want 3", packs[0].RulesCount)
	}
	if packs[0].Status != "loaded" {
		t.Fatalf("Status = %q, want loaded", packs[0].Status)
	}
	if packs[0].FirstSeen.IsZero() || packs[0].LastSeen.IsZero() {
		t.Fatalf("timestamps not persisted: %#v", packs[0])
	}
}

func TestSecurityStore_MarkStartupScanCompleted(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	if err := ss.MarkStartupScanCompleted("/repo", "hash-a"); err != nil {
		t.Fatalf("MarkStartupScanCompleted: %v", err)
	}
	status, err := ss.SecurityStatus("/repo")
	if err != nil {
		t.Fatalf("SecurityStatus: %v", err)
	}
	if status.StartupScanCompletedAt.IsZero() {
		t.Fatal("StartupScanCompletedAt is zero")
	}
	if status.StartupScanConfigHash != "hash-a" {
		t.Fatalf("StartupScanConfigHash = %q, want hash-a", status.StartupScanConfigHash)
	}

	if err := ss.SetWorkspaceStatus("/repo", "strict", "codex"); err != nil {
		t.Fatalf("SetWorkspaceStatus: %v", err)
	}
	status, err = ss.SecurityStatus("/repo")
	if err != nil {
		t.Fatalf("SecurityStatus after SetWorkspaceStatus: %v", err)
	}
	if status.Mode != "strict" || status.ActiveClient != "codex" {
		t.Fatalf("workspace mode/client = %q/%q, want strict/codex", status.Mode, status.ActiveClient)
	}
	if status.StartupScanConfigHash != "hash-a" {
		t.Fatalf("StartupScanConfigHash after SetWorkspaceStatus = %q, want hash-a", status.StartupScanConfigHash)
	}
}

func TestSecurityStore_RecordAndListQuarantineActions(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	record := SecurityQuarantineAction{
		Workspace:        "/repo",
		Kind:             "move-to-quarantine",
		SourcePath:       "/repo/.claude/hooks.js",
		DestinationPath:  "/repo/.milliways/quarantine/hooks.js",
		OriginalHash:     "sha256:original",
		AppliedHash:      "sha256:applied",
		Status:           "applied",
		RollbackHint:     "move the quarantined file back",
		AdditionalFields: map[string]string{"task": "agent-start"},
		AppliedAt:        time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
	}
	if err := ss.RecordQuarantineAction(record); err != nil {
		t.Fatalf("RecordQuarantineAction: %v", err)
	}

	records, err := ss.ListQuarantineActions("/repo")
	if err != nil {
		t.Fatalf("ListQuarantineActions: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	got := records[0]
	if got.Kind != record.Kind || got.SourcePath != record.SourcePath || got.Status != "applied" {
		t.Fatalf("record mismatch: %#v", got)
	}
	if got.OriginalHash != "sha256:original" || got.AppliedHash != "sha256:applied" {
		t.Fatalf("hashes not persisted: %#v", got)
	}
	if got.RollbackHint == "" {
		t.Fatal("rollback hint was not persisted")
	}
	if got.AdditionalFields["task"] != "agent-start" {
		t.Fatalf("additional fields = %#v", got.AdditionalFields)
	}
	if got.AppliedAt.IsZero() {
		t.Fatal("AppliedAt is zero")
	}
}

func TestSecurityStore_UpsertGetAndListClientProfiles(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	profile := SecurityClientProfile{
		Workspace:    "/repo",
		Client:       "claude",
		ConfigHash:   "sha256:a",
		WarningCount: 2,
		BlockCount:   1,
		Status:       "completed",
		ResultJSON:   `{"client":"claude","warnings":[{"id":"a"}]}`,
	}
	if err := ss.UpsertClientProfile(profile); err != nil {
		t.Fatalf("UpsertClientProfile: %v", err)
	}
	profile.WarningCount = 3
	profile.ResultJSON = `{"client":"claude","warnings":[{"id":"a"},{"id":"b"}]}`
	if err := ss.UpsertClientProfile(profile); err != nil {
		t.Fatalf("second UpsertClientProfile: %v", err)
	}

	got, ok, err := ss.GetClientProfile("/repo", "claude", "sha256:a")
	if err != nil {
		t.Fatalf("GetClientProfile: %v", err)
	}
	if !ok {
		t.Fatal("GetClientProfile ok = false, want true")
	}
	if got.WarningCount != 3 || got.BlockCount != 1 || got.Status != "completed" {
		t.Fatalf("profile counts/status mismatch: %#v", got)
	}
	if got.FirstCheckedAt.IsZero() || got.LastCheckedAt.IsZero() {
		t.Fatalf("timestamps not persisted: %#v", got)
	}

	if _, ok, err := ss.GetClientProfile("/repo", "claude", "sha256:b"); err != nil {
		t.Fatalf("GetClientProfile stale hash: %v", err)
	} else if ok {
		t.Fatal("GetClientProfile stale hash ok = true, want false")
	}

	if err := ss.UpsertClientProfile(SecurityClientProfile{
		Workspace:    "/repo",
		Client:       "claude",
		ConfigHash:   "sha256:b",
		WarningCount: 0,
		Status:       "completed",
		ResultJSON:   `{"client":"claude","warnings":[]}`,
	}); err != nil {
		t.Fatalf("UpsertClientProfile second hash: %v", err)
	}
	profiles, err := ss.ListClientProfiles("/repo", "claude")
	if err != nil {
		t.Fatalf("ListClientProfiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("ListClientProfiles len = %d, want 2", len(profiles))
	}
}

func TestSecurityStore_UpsertWarningIdempotent(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	w := SecurityWarning{
		Workspace:   "/repo",
		Category:    "ioc",
		Severity:    "WARN",
		Source:      "package.json",
		Message:     "suspicious package script",
		Remediation: "review script",
	}
	if err := ss.UpsertWarning(w); err != nil {
		t.Fatalf("UpsertWarning: %v", err)
	}
	w.Severity = "BLOCK"
	w.Remediation = "remove script"
	if err := ss.UpsertWarning(w); err != nil {
		t.Fatalf("second UpsertWarning: %v", err)
	}

	warnings, err := ss.ListActiveWarnings("/repo")
	if err != nil {
		t.Fatalf("ListActiveWarnings: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings len = %d, want 1", len(warnings))
	}
	if warnings[0].Severity != "BLOCK" {
		t.Fatalf("Severity = %q, want BLOCK", warnings[0].Severity)
	}
	if warnings[0].Remediation != "remove script" {
		t.Fatalf("Remediation = %q, want remove script", warnings[0].Remediation)
	}
}

func TestSecurityStore_SecurityStatusAggregatesFindingsAndWarnings(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ss := db.Security()

	if err := ss.SetWorkspaceStatus("/repo", "strict", "codex"); err != nil {
		t.Fatalf("SetWorkspaceStatus: %v", err)
	}
	if err := ss.UpsertFinding(SecurityFinding{
		CVEID:            "CVE-2026-11111",
		PackageName:      "pkg",
		InstalledVersion: "v1",
		Severity:         "HIGH",
		Ecosystem:        "Go",
	}); err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}
	if err := ss.UpsertWarning(SecurityWarning{
		Workspace: "/repo",
		Category:  "client-profile",
		Severity:  "BLOCK",
		Source:    "codex",
		Message:   "unsafe approval mode",
	}); err != nil {
		t.Fatalf("UpsertWarning: %v", err)
	}

	status, err := ss.SecurityStatus("/repo")
	if err != nil {
		t.Fatalf("SecurityStatus: %v", err)
	}
	if status.Mode != "strict" {
		t.Fatalf("Mode = %q, want strict", status.Mode)
	}
	if status.ActiveClient != "codex" {
		t.Fatalf("ActiveClient = %q, want codex", status.ActiveClient)
	}
	if status.Posture != "block" {
		t.Fatalf("Posture = %q, want block", status.Posture)
	}
	if status.CountsByCategory["dependency"] != 1 {
		t.Fatalf("dependency count = %d, want 1", status.CountsByCategory["dependency"])
	}
	if status.CountsByCategory["client-profile"] != 1 {
		t.Fatalf("client-profile count = %d, want 1", status.CountsByCategory["client-profile"])
	}
	if status.CountsBySeverity["HIGH"] != 1 {
		t.Fatalf("HIGH count = %d, want 1", status.CountsBySeverity["HIGH"])
	}
	if status.CountsBySeverity["BLOCK"] != 1 {
		t.Fatalf("BLOCK count = %d, want 1", status.CountsBySeverity["BLOCK"])
	}
}
