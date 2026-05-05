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

package security_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/security"
)

// openTestDB opens a real pantry DB in a temp dir.
func openTestDB(t *testing.T) *pantry.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := pantry.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRunner_UpsertFindings(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := db.Security()
	runner := security.NewRunner(store, t.TempDir())

	result := security.ScanResult{
		Findings: []security.Finding{
			{
				CVEID:            "CVE-2024-0001",
				PackageName:      "pkg-a",
				InstalledVersion: "1.0.0",
				FixedInVersion:   "1.0.1",
				Severity:         "HIGH",
				Ecosystem:        "Go",
				Summary:          "test vuln a",
				ScanSource:       "go.sum",
			},
			{
				CVEID:            "CVE-2024-0002",
				PackageName:      "pkg-b",
				InstalledVersion: "2.0.0",
				FixedInVersion:   "",
				Severity:         "MEDIUM",
				Ecosystem:        "Go",
				Summary:          "test vuln b",
				ScanSource:       "go.sum",
			},
		},
		ScannedAt: time.Now(),
		LockFiles: []string{"go.sum"},
	}

	if err := runner.UpsertFindings(result); err != nil {
		t.Fatalf("UpsertFindings: %v", err)
	}

	active, err := store.ListActive(nil)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active findings, got %d", len(active))
	}
}

func TestRunner_UpsertFindings_MarkResolved(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := db.Security()
	runner := security.NewRunner(store, t.TempDir())

	// First pass: 2 findings for go.sum.
	first := security.ScanResult{
		Findings: []security.Finding{
			{
				CVEID:      "CVE-2024-1001",
				PackageName: "pkg-x",
				Severity:   "HIGH",
				ScanSource: "go.sum",
			},
			{
				CVEID:      "CVE-2024-1002",
				PackageName: "pkg-y",
				Severity:   "CRITICAL",
				ScanSource: "go.sum",
			},
		},
		ScannedAt: time.Now(),
		LockFiles: []string{"go.sum"},
	}
	if err := runner.UpsertFindings(first); err != nil {
		t.Fatalf("UpsertFindings first pass: %v", err)
	}

	active, err := store.ListActive(nil)
	if err != nil {
		t.Fatalf("ListActive after first pass: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active findings after first pass, got %d", len(active))
	}

	// Second pass: only 1 finding (different CVE) for go.sum. CVE-2024-1001 should be resolved.
	second := security.ScanResult{
		Findings: []security.Finding{
			{
				CVEID:      "CVE-2024-1002",
				PackageName: "pkg-y",
				Severity:   "CRITICAL",
				ScanSource: "go.sum",
			},
		},
		ScannedAt: time.Now(),
		LockFiles: []string{"go.sum"},
	}
	if err := runner.UpsertFindings(second); err != nil {
		t.Fatalf("UpsertFindings second pass: %v", err)
	}

	active, err = store.ListActive(nil)
	if err != nil {
		t.Fatalf("ListActive after second pass: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active finding after second pass, got %d: %+v", len(active), active)
	}
	if active[0].CVEID != "CVE-2024-1002" {
		t.Errorf("expected remaining active finding to be CVE-2024-1002, got %q", active[0].CVEID)
	}
}

func TestRunner_MtimeDetection(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockfile := filepath.Join(dir, "go.sum")
	if err := os.WriteFile(lockfile, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := openTestDB(t)
	store := db.Security()

	var scanCalled int
	mockScan := func(_ context.Context, _ []string) (security.ScanResult, error) {
		scanCalled++
		return security.ScanResult{
			Findings:  nil,
			ScannedAt: time.Now(),
			LockFiles: []string{lockfile},
		}, nil
	}

	runner := security.NewRunnerWithScanFunc(store, dir, mockScan)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// scanNow should trigger the mock.
	if _, err := runner.ScanNow(ctx); err != nil {
		t.Fatalf("ScanNow: %v", err)
	}

	if scanCalled == 0 {
		t.Fatal("expected scan function to be called at least once")
	}
}

func TestRunner_Ready(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := openTestDB(t)
	store := db.Security()

	mockScan := func(_ context.Context, _ []string) (security.ScanResult, error) {
		return security.ScanResult{ScannedAt: time.Now()}, nil
	}

	runner := security.NewRunnerWithScanFunc(store, dir, mockScan)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runner.Start(ctx)

	select {
	case <-runner.Ready():
		// ok
	case <-ctx.Done():
		t.Fatal("timeout waiting for runner to become ready")
	}
}
