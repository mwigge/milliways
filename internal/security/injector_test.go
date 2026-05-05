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

package security

import (
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
)

func TestBuildContextBlock_Empty(t *testing.T) {
	t.Parallel()

	got := BuildContextBlock(nil, DefaultTokenCap)
	if got != "" {
		t.Errorf("BuildContextBlock(nil) = %q, want empty string", got)
	}

	got = BuildContextBlock([]pantry.SecurityFinding{}, DefaultTokenCap)
	if got != "" {
		t.Errorf("BuildContextBlock([]) = %q, want empty string", got)
	}
}

func TestBuildContextBlock_OneCritical(t *testing.T) {
	t.Parallel()

	findings := []pantry.SecurityFinding{
		{
			CVEID:       "CVE-2024-12345",
			Severity:    "CRITICAL",
			PackageName:     "github.com/foo/bar",
			InstalledVersion:     "v1.2.0",
			FixedInVersion:     "v1.2.1",
			Summary:     "Arbitrary code execution via crafted input in Bar.Parse()",
			Status:      "active",
			FirstSeen:   time.Now(),
		},
	}

	got := BuildContextBlock(findings, DefaultTokenCap)

	if got == "" {
		t.Fatal("BuildContextBlock returned empty string for 1 finding")
	}
	if !strings.Contains(got, "[security context") {
		t.Errorf("missing [security context header, got:\n%s", got)
	}
	if !strings.Contains(got, "CVE-2024-12345") {
		t.Errorf("missing CVE ID, got:\n%s", got)
	}
	if !strings.Contains(got, "github.com/foo/bar") {
		t.Errorf("missing package name, got:\n%s", got)
	}
	if !strings.Contains(got, "CRITICAL") {
		t.Errorf("missing severity CRITICAL, got:\n%s", got)
	}
}

func TestBuildContextBlock_Truncation(t *testing.T) {
	t.Parallel()

	// Build enough findings to force truncation. Each finding's summary is
	// 400 chars (~100 tokens). We use a cap of 200 tokens so the second
	// finding (header + finding1 + finding2 + footer) would breach the cap.
	truncCap := 200
	var findings []pantry.SecurityFinding
	for i := range 10 {
		findings = append(findings, pantry.SecurityFinding{
			CVEID:    "CVE-2024-00001",
			Severity: "CRITICAL",
			PackageName:  "github.com/foo/bar",
			InstalledVersion:  "v1.0.0",
			FixedInVersion:  "v1.0.1",
			Summary:  strings.Repeat("x", 400), // ~100 tokens per finding
			Status:   "active",
			FirstSeen: time.Now(),
		})
		_ = i
	}

	got := BuildContextBlock(findings, truncCap)

	// The output must contain a truncation notice.
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation notice in output, got:\n%s", got)
	}
	// The full un-truncated block would be much larger than the cap.
	// Verify fewer than all findings are included by checking the note says < 10.
	if strings.Contains(got, "showing 10 of 10") {
		t.Errorf("expected fewer than all findings shown, got:\n%s", got)
	}
}

func TestBuildContextBlock_SeverityOrdering(t *testing.T) {
	t.Parallel()

	// Input: HIGH then CRITICAL — but BuildContextBlock receives pre-sorted input
	// so we test that when CRITICAL appears first in input, it appears first in output.
	findings := []pantry.SecurityFinding{
		{
			CVEID:    "CVE-2024-CRIT",
			Severity: "CRITICAL",
			PackageName:  "github.com/critical/pkg",
			InstalledVersion:  "v1.0.0",
			Status:   "active",
			FirstSeen: time.Now(),
		},
		{
			CVEID:    "CVE-2024-HIGH",
			Severity: "HIGH",
			PackageName:  "github.com/high/pkg",
			InstalledVersion:  "v2.0.0",
			Status:   "active",
			FirstSeen: time.Now(),
		},
	}

	got := BuildContextBlock(findings, DefaultTokenCap)

	critIdx := strings.Index(got, "CVE-2024-CRIT")
	highIdx := strings.Index(got, "CVE-2024-HIGH")

	if critIdx < 0 {
		t.Fatalf("CRITICAL CVE not found in output:\n%s", got)
	}
	if highIdx < 0 {
		t.Fatalf("HIGH CVE not found in output:\n%s", got)
	}
	if critIdx > highIdx {
		t.Errorf("CRITICAL should appear before HIGH; CRITICAL at %d, HIGH at %d", critIdx, highIdx)
	}
}

func TestBuildContextBlock_MixedSeverities(t *testing.T) {
	t.Parallel()

	// Pre-filtered to CRITICAL+HIGH (ListActive already excludes others),
	// both should appear.
	findings := []pantry.SecurityFinding{
		{
			CVEID:    "CVE-2024-CRIT1",
			Severity: "CRITICAL",
			PackageName:  "github.com/pkg/critical",
			InstalledVersion:  "v1.0.0",
			Status:   "active",
			FirstSeen: time.Now(),
		},
		{
			CVEID:    "CVE-2024-HIGH1",
			Severity: "HIGH",
			PackageName:  "github.com/pkg/high",
			InstalledVersion:  "v2.0.0",
			FixedInVersion:  "v2.0.1",
			Summary:  "Path traversal in ReadFile()",
			Status:   "active",
			FirstSeen: time.Now(),
		},
	}

	got := BuildContextBlock(findings, DefaultTokenCap)

	if !strings.Contains(got, "CVE-2024-CRIT1") {
		t.Errorf("CRITICAL finding missing from output:\n%s", got)
	}
	if !strings.Contains(got, "CVE-2024-HIGH1") {
		t.Errorf("HIGH finding missing from output:\n%s", got)
	}
	if !strings.Contains(got, "CRITICAL") {
		t.Errorf("CRITICAL severity label missing:\n%s", got)
	}
	if !strings.Contains(got, "HIGH") {
		t.Errorf("HIGH severity label missing:\n%s", got)
	}
}

func TestBuildContextBlock_NoFixAvailable(t *testing.T) {
	t.Parallel()

	findings := []pantry.SecurityFinding{
		{
			CVEID:    "CVE-2024-NOFIX",
			Severity: "HIGH",
			PackageName:  "github.com/baz/qux",
			InstalledVersion:  "v0.9.1",
			FixedInVersion:  "",
			Summary:  "Path traversal in Qux.ReadFile()",
			Status:   "active",
			FirstSeen: time.Now(),
		},
	}

	got := BuildContextBlock(findings, DefaultTokenCap)

	if !strings.Contains(got, "no fix available") {
		t.Errorf("expected 'no fix available' when FixedIn is empty, got:\n%s", got)
	}
}
