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

package daemon

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
)

// securityIntegTestHarness starts a real Server and returns a connected
// send/readResp pair. Callers must defer cleanup().
func securityIntegTestHarness(t *testing.T) (srv *Server, stateDir string, send func(string, any, any), readResp func() (map[string]any, error), cleanup func()) {
	t.Helper()
	stateDir, err := os.MkdirTemp("", "milliways-sec-integ-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	sock := filepath.Join(stateDir, "sock")

	srv, err = NewServer(sock)
	if err != nil {
		os.RemoveAll(stateDir)
		t.Fatalf("NewServer: %v", err)
	}
	go srv.Serve()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		srv.Close()
		os.RemoveAll(stateDir)
		t.Fatalf("dial: %v", err)
	}

	enc := json.NewEncoder(conn)
	reader := bufio.NewReader(conn)

	send = func(method string, params, id any) {
		t.Helper()
		req := map[string]any{
			"jsonrpc": "2.0",
			"method":  method,
		}
		if params != nil {
			req["params"] = params
		}
		if id != nil {
			req["id"] = id
		}
		if err := enc.Encode(req); err != nil {
			t.Fatalf("encode %s: %v", method, err)
		}
	}

	readResp = func() (map[string]any, error) {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var resp map[string]any
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, err
		}
		return resp, nil
	}

	cleanup = func() {
		conn.Close()
		srv.Close()
		os.RemoveAll(stateDir)
	}
	return
}

// openTestPantry opens the pantry DB at the same path the server uses.
// The path convention is: filepath.Dir(sock)/milliways.db.
func openTestPantry(t *testing.T, stateDir string) *pantry.DB {
	t.Helper()
	dbPath := filepath.Join(stateDir, "milliways.db")
	db, err := pantry.Open(dbPath)
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestSecurityList_ReturnsInsertedFinding verifies that a finding inserted
// directly into the pantry DB is returned by the security.list RPC.
func TestSecurityList_ReturnsInsertedFinding(t *testing.T) {
	t.Parallel()

	_, stateDir, send, readResp, cleanup := securityIntegTestHarness(t)
	defer cleanup()

	db := openTestPantry(t, stateDir)

	// Insert a finding directly into the DB.
	err := db.Security().UpsertFinding(pantry.SecurityFinding{
		CVEID:            "CVE-2024-9001",
		PackageName:      "example.com/test",
		InstalledVersion: "1.0.0",
		FixedInVersion:   "1.0.1",
		Severity:         "HIGH",
		Ecosystem:        "Go",
		Summary:          "Test vulnerability",
		ScanSource:       "test",
		Status:           "active",
		FirstSeen:        time.Now().Add(-24 * time.Hour),
		LastSeen:         time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}

	// Call security.list RPC.
	send("security.list", map[string]any{"include_accepted": false}, 1)
	resp, err := readResp()
	if err != nil {
		t.Fatalf("readResp: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("security.list error: %v", errObj)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result not map: %v", resp)
	}
	findings, ok := result["findings"].([]any)
	if !ok {
		t.Fatalf("findings not array: %v", result)
	}
	if len(findings) == 0 {
		t.Fatal("expected at least 1 finding, got 0")
	}

	found := false
	for _, f := range findings {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		if fm["cve_id"] == "CVE-2024-9001" {
			found = true
			if fm["severity"] != "HIGH" {
				t.Errorf("severity = %v, want HIGH", fm["severity"])
			}
			if fm["package_name"] != "example.com/test" {
				t.Errorf("package_name = %v, want example.com/test", fm["package_name"])
			}
		}
	}
	if !found {
		t.Errorf("CVE-2024-9001 not found in findings: %v", findings)
	}
}

// TestSecurityAccept_ExcludesFromList verifies that after accepting a risk,
// security.list (without include_accepted) omits the accepted finding.
func TestSecurityAccept_ExcludesFromList(t *testing.T) {
	t.Parallel()

	_, stateDir, send, readResp, cleanup := securityIntegTestHarness(t)
	defer cleanup()

	db := openTestPantry(t, stateDir)

	// Insert a finding.
	err := db.Security().UpsertFinding(pantry.SecurityFinding{
		CVEID:            "CVE-2024-9002",
		PackageName:      "example.com/accept-test",
		InstalledVersion: "2.0.0",
		FixedInVersion:   "2.0.1",
		Severity:         "MEDIUM",
		Ecosystem:        "Go",
		Summary:          "Accepted risk test",
		ScanSource:       "test",
		Status:           "active",
		FirstSeen:        time.Now().Add(-1 * time.Hour),
		LastSeen:         time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}

	// Verify it appears in the list.
	send("security.list", map[string]any{"include_accepted": false}, 1)
	resp, err := readResp()
	if err != nil {
		t.Fatalf("readResp list before accept: %v", err)
	}
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.list error before accept: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	findingsBeforeArr := result["findings"].([]any)
	foundBefore := false
	for _, f := range findingsBeforeArr {
		if fm, ok := f.(map[string]any); ok && fm["cve_id"] == "CVE-2024-9002" {
			foundBefore = true
		}
	}
	if !foundBefore {
		t.Fatal("CVE-2024-9002 should appear in list before accept")
	}

	// Accept the risk via RPC.
	send("security.accept", map[string]any{
		"cve_id":       "CVE-2024-9002",
		"package_name": "example.com/accept-test",
		"reason":       "integration test acceptance",
		"expires_at":   time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339),
	}, 2)
	resp, err = readResp()
	if err != nil {
		t.Fatalf("readResp accept: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("security.accept error: %v", errObj)
	}

	// Verify it's gone from the list.
	send("security.list", map[string]any{"include_accepted": false}, 3)
	resp, err = readResp()
	if err != nil {
		t.Fatalf("readResp list after accept: %v", err)
	}
	if _, ok := resp["error"]; ok {
		t.Fatalf("security.list error after accept: %v", resp["error"])
	}
	result = resp["result"].(map[string]any)
	var findingsAfterArr []any
	if fa, ok := result["findings"]; ok && fa != nil {
		findingsAfterArr, _ = fa.([]any)
	}
	for _, f := range findingsAfterArr {
		if fm, ok := f.(map[string]any); ok && fm["cve_id"] == "CVE-2024-9002" {
			t.Fatal("CVE-2024-9002 should be excluded after accept (without include_accepted)")
		}
	}
}

// TestSecurityShow_ReturnsFinding verifies security.show returns the correct CVE details.
func TestSecurityShow_ReturnsFinding(t *testing.T) {
	t.Parallel()

	_, stateDir, send, readResp, cleanup := securityIntegTestHarness(t)
	defer cleanup()

	db := openTestPantry(t, stateDir)

	err := db.Security().UpsertFinding(pantry.SecurityFinding{
		CVEID:            "CVE-2024-9003",
		PackageName:      "example.com/show-test",
		InstalledVersion: "3.0.0",
		FixedInVersion:   "3.0.2",
		Severity:         "CRITICAL",
		Ecosystem:        "Go",
		Summary:          "Show test vulnerability",
		ScanSource:       "test",
		Status:           "active",
		FirstSeen:        time.Now().Add(-2 * time.Hour),
		LastSeen:         time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}

	send("security.show", map[string]any{"cve_id": "CVE-2024-9003"}, 1)
	resp, err := readResp()
	if err != nil {
		t.Fatalf("readResp: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("security.show error: %v", errObj)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result not map: %v", resp)
	}
	finding, ok := result["finding"].(map[string]any)
	if !ok {
		t.Fatalf("finding not map: %v", result)
	}
	if finding["cve_id"] != "CVE-2024-9003" {
		t.Errorf("cve_id = %v, want CVE-2024-9003", finding["cve_id"])
	}
	if finding["severity"] != "CRITICAL" {
		t.Errorf("severity = %v, want CRITICAL", finding["severity"])
	}
}

// TestSecurityShow_UnknownCVE verifies security.show returns an error for missing CVEs.
func TestSecurityShow_UnknownCVE(t *testing.T) {
	t.Parallel()

	_, _, send, readResp, cleanup := securityIntegTestHarness(t)
	defer cleanup()

	send("security.show", map[string]any{"cve_id": "CVE-9999-0000"}, 1)
	resp, err := readResp()
	if err != nil {
		t.Fatalf("readResp: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Fatal("expected error response for unknown CVE")
	}
}

// TestSecurityAccept_ExpiryTooFarServer verifies the daemon rejects accepts with expiry > 365 days.
func TestSecurityAccept_ExpiryTooFarServer(t *testing.T) {
	t.Parallel()

	_, stateDir, send, readResp, cleanup := securityIntegTestHarness(t)
	defer cleanup()

	db := openTestPantry(t, stateDir)

	// Insert a finding first.
	err := db.Security().UpsertFinding(pantry.SecurityFinding{
		CVEID:            "CVE-2024-9004",
		PackageName:      "example.com/expiry-test",
		InstalledVersion: "1.0.0",
		FixedInVersion:   "1.0.1",
		Severity:         "LOW",
		Ecosystem:        "Go",
		Summary:          "Expiry test",
		ScanSource:       "test",
		Status:           "active",
		FirstSeen:        time.Now(),
		LastSeen:         time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}

	tooFar := time.Now().AddDate(1, 0, 2).UTC().Format(time.RFC3339)
	send("security.accept", map[string]any{
		"cve_id":       "CVE-2024-9004",
		"package_name": "example.com/expiry-test",
		"reason":       "testing expiry rejection",
		"expires_at":   tooFar,
	}, 1)
	resp, err := readResp()
	if err != nil {
		t.Fatalf("readResp: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Fatal("expected error for expiry > 365 days")
	}
}

// TestSecurityScan_ReturnsCachedFindings verifies security.scan returns DB state
// when no live scanner is available (the default stub scenario).
func TestSecurityScan_ReturnsCachedFindings(t *testing.T) {
	t.Parallel()

	_, stateDir, send, readResp, cleanup := securityIntegTestHarness(t)
	defer cleanup()

	db := openTestPantry(t, stateDir)

	err := db.Security().UpsertFinding(pantry.SecurityFinding{
		CVEID:            "CVE-2024-9005",
		PackageName:      "example.com/scan-test",
		InstalledVersion: "5.0.0",
		FixedInVersion:   "5.0.1",
		Severity:         "MEDIUM",
		Ecosystem:        "Go",
		Summary:          "Scan test vulnerability",
		ScanSource:       "test",
		Status:           "active",
		FirstSeen:        time.Now(),
		LastSeen:         time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}

	send("security.scan", map[string]any{}, 1)
	resp, err := readResp()
	if err != nil {
		t.Fatalf("readResp: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("security.scan error: %v", errObj)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result not map: %v", resp)
	}
	if _, ok := result["scanned_at"]; !ok {
		t.Error("expected scanned_at in result")
	}
	findings, _ := result["findings"].([]any)
	found := false
	for _, f := range findings {
		fm, ok := f.(map[string]any)
		if ok && fm["cve_id"] == "CVE-2024-9005" {
			found = true
		}
	}
	if !found {
		t.Errorf("CVE-2024-9005 not found in scan result: %v", findings)
	}
}

// TestAgentOpen_SecurityContextSuppressed verifies that security context
// injection is suppressed when security_context: false is passed to agent.open.
// If injection is not wired yet, this test is skipped.
func TestInteg_AgentOpen_ContextSuppressed_Stub(t *testing.T) {
	t.Skip("security injection not wired")
}

// TestAgentOpen_SecurityContextInjected verifies that a CRITICAL finding causes
// a security context priming message in the agent stream.
// Requires Agent B's injection code to be wired.
func TestInteg_AgentOpen_ContextInjected_Stub(t *testing.T) {
	t.Skip("security injection not wired")
}
