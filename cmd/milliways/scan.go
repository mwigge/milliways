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
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// handleScan implements the /scan slash command.
// It calls the daemon's security.scan RPC with a 30-second timeout enforced
// via a context + goroutine, since the underlying RPC client does not accept a
// context directly. If the timeout fires first, an error is written to the
// user and the background goroutine is abandoned.
func (l *chatLoop) handleScan(_ string) {
	if l.client == nil {
		fmt.Fprintln(l.errw, "[scan] not connected to daemon")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type scanResult struct {
		result map[string]any
		err    error
	}
	ch := make(chan scanResult, 1)
	go func() {
		var result map[string]any
		err := l.client.Call("security.scan", map[string]any{}, &result)
		ch <- scanResult{result: result, err: err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			fmt.Fprintln(l.errw, friendlyError("[scan] scan: ", "", res.err))
			return
		}
		fmt.Fprintln(l.out, renderScanResult(res.result))
	case <-ctx.Done():
		fmt.Fprintln(l.errw, "[scan] timed out after 30s — daemon did not respond")
	}
}

// severityOrder maps severity names to sort priority (lower = higher priority).
var severityOrder = map[string]int{
	"CRITICAL": 0,
	"HIGH":     1,
	"MEDIUM":   2,
	"LOW":      3,
}

// severityRank returns the sort rank for a severity string.
// Unknown severities sort after LOW.
func severityRank(s string) int {
	if r, ok := severityOrder[strings.ToUpper(s)]; ok {
		return r
	}
	return 99
}

// renderScanResult formats a security.scan RPC result for display.
//
// Output format with findings:
//
//	[scan] go.sum · Cargo.lock scanned — 3 findings
//	CRITICAL  CVE-2024-12345  github.com/foo/bar@v1.2.0  → fix: v1.2.1
//	HIGH      CVE-2024-55555  openssl@3.0.1              → fix: 3.0.2
//	MEDIUM    CVE-2024-99999  libc@2.35                  → no fix
//	─────────────────────────────────────────────────────────────────
//	Run: milliwaysctl security accept <cve-id> --reason "..." --expires 2026-08-01
//
// Output format when no findings:
//
//	[scan] scanned — no findings ✓
func renderScanResult(result map[string]any) string {
	findings := extractFindings(result)
	lockfiles := extractLockfiles(result)

	if len(findings) == 0 {
		return "[scan] scanned — no findings ✓"
	}

	// Sort findings: CRITICAL → HIGH → MEDIUM → LOW.
	sort.Slice(findings, func(i, j int) bool {
		ri := severityRank(findings[i].severity)
		rj := severityRank(findings[j].severity)
		if ri != rj {
			return ri < rj
		}
		return findings[i].cveID < findings[j].cveID
	})

	var sb strings.Builder

	// Header line.
	if len(lockfiles) > 0 {
		fmt.Fprintf(&sb, "[scan] %s scanned — %d finding", strings.Join(lockfiles, " · "), len(findings))
	} else {
		fmt.Fprintf(&sb, "[scan] scanned — %d finding", len(findings))
	}
	if len(findings) != 1 {
		sb.WriteString("s")
	}
	sb.WriteString("\n")

	// Finding rows.
	for _, f := range findings {
		fix := "no fix"
		if f.fixedIn != "" {
			fix = "fix: " + f.fixedIn
		}
		fmt.Fprintf(&sb, "%-10s %-16s %s@%s  → %s\n",
			f.severity, f.cveID, f.pkg, f.version, fix)
	}

	// Divider + action hint.
	sb.WriteString("─────────────────────────────────────────────────────────────────\n")
	sb.WriteString(`Run: milliwaysctl security accept <cve-id> --reason "..." --expires 2026-08-01`)

	return sb.String()
}

// scanFinding is an internal parsed representation of a finding map.
type scanFinding struct {
	cveID    string
	pkg      string
	version  string
	fixedIn  string
	severity string
	summary  string
}

// extractFindings pulls the findings list from the result map.
func extractFindings(result map[string]any) []scanFinding {
	raw, ok := result["findings"]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]scanFinding, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, scanFinding{
			cveID:    stringField(m, "cve_id"),
			pkg:      stringField(m, "package"),
			version:  stringField(m, "version"),
			fixedIn:  stringField(m, "fixed_in"),
			severity: strings.ToUpper(stringField(m, "severity")),
			summary:  stringField(m, "summary"),
		})
	}
	return out
}

// extractLockfiles returns lockfile names from the result map.
func extractLockfiles(result map[string]any) []string {
	raw, ok := result["lockfiles"]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// stringField extracts a string value from a map, returning "" if absent or wrong type.
func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
