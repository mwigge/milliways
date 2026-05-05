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

// Package security provides the security context injection layer for milliways.
// It formats active security findings into a priming block injected into agent
// sessions before the first user turn.
package security

import (
	"fmt"
	"strings"

	"github.com/mwigge/milliways/internal/pantry"
)

// DefaultTokenCap is the maximum estimated token budget for the security
// context block. Token estimation: 1 token ≈ 4 chars.
const DefaultTokenCap = 2000

// separator is the horizontal rule used at the bottom of the context block.
const separator = "─────────────────────────────────────────────────────────────"

// BuildContextBlock formats a [security context] priming block from findings.
// Findings must be pre-filtered (CRITICAL+HIGH only) and pre-sorted (CRITICAL
// first, then HIGH; within tier by first_seen desc).
//
// Truncates to tokenCap estimated tokens (1 token ≈ 4 chars).
// Returns empty string when findings is empty.
func BuildContextBlock(findings []pantry.SecurityFinding, tokenCap int) string {
	if len(findings) == 0 {
		return ""
	}

	total := len(findings)

	header := fmt.Sprintf("[security context — %d active findings in this workspace]\n", total)
	footer := separator + "\nRun /scan to refresh · milliwaysctl security list for full report\n"

	var sb strings.Builder
	sb.WriteString(header)

	included := 0
	for _, f := range findings {
		line := formatFinding(f)

		// Check if adding this finding plus footer would exceed the cap.
		// We measure the complete prospective block.
		prospective := sb.String() + line + footer
		if len(prospective)/4 > tokenCap {
			// Would exceed cap — emit truncation notice and stop.
			truncNote := fmt.Sprintf(
				"[truncated — showing %d of %d findings. Run /scan for full report]\n",
				included, total,
			)
			sb.WriteString(truncNote)
			sb.WriteString(footer)
			return sb.String()
		}

		sb.WriteString(line)
		included++
	}

	sb.WriteString(footer)
	return sb.String()
}

// formatFinding renders one SecurityFinding as two lines:
//
//	CRITICAL  CVE-2024-12345  github.com/foo/bar@v1.2.0  fixed in v1.2.1
//	          Arbitrary code execution...
func formatFinding(f pantry.SecurityFinding) string {
	pkgVersion := f.PackageName + "@" + f.InstalledVersion

	var fixNote string
	if f.FixedInVersion != "" {
		fixNote = "fixed in " + f.FixedInVersion
	} else {
		fixNote = "no fix available"
	}

	firstLine := fmt.Sprintf("%-9s %-20s %-40s %s\n",
		f.Severity, f.CVEID, pkgVersion, fixNote)

	var lines strings.Builder
	lines.WriteString(firstLine)
	if f.Summary != "" {
		lines.WriteString(fmt.Sprintf("%-9s %s\n", "", f.Summary))
	}
	return lines.String()
}
