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
