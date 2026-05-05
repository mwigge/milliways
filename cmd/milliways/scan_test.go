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
	"bytes"
	"strings"
	"testing"
)

// TestHandleScan_NilClient_PrintsError verifies that /scan with no daemon
// connection writes an error message and does not panic.
func TestHandleScan_NilClient_PrintsError(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	loop := &chatLoop{
		client: nil,
		out:    &stdout,
		errw:   &stderr,
	}
	loop.handleScan("")

	if stdout.Len() != 0 {
		t.Errorf("expected no stdout output, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[scan]") {
		t.Errorf("expected [scan] error prefix in stderr, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "not connected") {
		t.Errorf("expected 'not connected' in stderr, got %q", stderr.String())
	}
}

// TestRenderScanResult_NoFindings verifies the no-findings output.
func TestRenderScanResult_NoFindings(t *testing.T) {
	t.Parallel()

	result := map[string]any{
		"scanned_at": "2026-05-05T10:00:00Z",
		"lockfiles":  []any{},
		"findings":   []any{},
	}
	got := renderScanResult(result)
	if !strings.Contains(got, "[scan]") {
		t.Errorf("expected [scan] prefix, got %q", got)
	}
	if !strings.Contains(got, "no findings") {
		t.Errorf("expected 'no findings' in output, got %q", got)
	}
}

// TestRenderScanResult_WithFindings verifies findings are rendered and
// CRITICAL appears before HIGH in the output.
func TestRenderScanResult_WithFindings(t *testing.T) {
	t.Parallel()

	result := map[string]any{
		"scanned_at": "2026-05-05T10:00:00Z",
		"lockfiles":  []any{"go.sum"},
		"findings": []any{
			map[string]any{
				"cve_id":   "CVE-2024-55555",
				"package":  "openssl",
				"version":  "3.0.1",
				"fixed_in": "3.0.2",
				"severity": "HIGH",
				"summary":  "buffer overflow",
			},
			map[string]any{
				"cve_id":   "CVE-2024-12345",
				"package":  "github.com/foo/bar",
				"version":  "v1.2.0",
				"fixed_in": "v1.2.1",
				"severity": "CRITICAL",
				"summary":  "remote code execution",
			},
		},
	}
	got := renderScanResult(result)

	criticalIdx := strings.Index(got, "CRITICAL")
	highIdx := strings.Index(got, "HIGH")
	if criticalIdx == -1 {
		t.Fatalf("CRITICAL not found in output: %q", got)
	}
	if highIdx == -1 {
		t.Fatalf("HIGH not found in output: %q", got)
	}
	if criticalIdx > highIdx {
		t.Errorf("CRITICAL (%d) should appear before HIGH (%d) in output:\n%s", criticalIdx, highIdx, got)
	}
	if !strings.Contains(got, "CVE-2024-12345") {
		t.Errorf("expected CVE-2024-12345 in output, got %q", got)
	}
	if !strings.Contains(got, "CVE-2024-55555") {
		t.Errorf("expected CVE-2024-55555 in output, got %q", got)
	}
	if !strings.Contains(got, "milliwaysctl security accept") {
		t.Errorf("expected milliwaysctl command hint in output, got %q", got)
	}
}

// TestRenderScanResult_SeverityOrdering verifies CRITICAL → HIGH → MEDIUM → LOW ordering.
func TestRenderScanResult_SeverityOrdering(t *testing.T) {
	t.Parallel()

	result := map[string]any{
		"scanned_at": "2026-05-05T10:00:00Z",
		"lockfiles":  []any{"go.sum", "Cargo.lock"},
		"findings": []any{
			map[string]any{
				"cve_id":   "CVE-LOW",
				"package":  "pkgA",
				"version":  "1.0",
				"fixed_in": "1.1",
				"severity": "LOW",
				"summary":  "minor issue",
			},
			map[string]any{
				"cve_id":   "CVE-MEDIUM",
				"package":  "pkgB",
				"version":  "2.0",
				"fixed_in": "",
				"severity": "MEDIUM",
				"summary":  "moderate issue",
			},
			map[string]any{
				"cve_id":   "CVE-HIGH",
				"package":  "pkgC",
				"version":  "3.0",
				"fixed_in": "3.1",
				"severity": "HIGH",
				"summary":  "serious issue",
			},
			map[string]any{
				"cve_id":   "CVE-CRITICAL",
				"package":  "pkgD",
				"version":  "4.0",
				"fixed_in": "4.1",
				"severity": "CRITICAL",
				"summary":  "critical issue",
			},
		},
	}

	got := renderScanResult(result)

	idxCritical := strings.Index(got, "CVE-CRITICAL")
	idxHigh := strings.Index(got, "CVE-HIGH")
	idxMedium := strings.Index(got, "CVE-MEDIUM")
	idxLow := strings.Index(got, "CVE-LOW")

	if idxCritical == -1 || idxHigh == -1 || idxMedium == -1 || idxLow == -1 {
		t.Fatalf("not all severity levels found in output:\n%s", got)
	}
	if !(idxCritical < idxHigh && idxHigh < idxMedium && idxMedium < idxLow) {
		t.Errorf("severity order wrong: CRITICAL=%d HIGH=%d MEDIUM=%d LOW=%d\noutput:\n%s",
			idxCritical, idxHigh, idxMedium, idxLow, got)
	}

	// No fix available case.
	if !strings.Contains(got, "no fix") {
		t.Errorf("expected 'no fix' for empty fixed_in, got %q", got)
	}
}
