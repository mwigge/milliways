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

package parallel

import (
	"bufio"
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
)

// findingLineRe matches lines of the form:
//
//	path/to/file.go: description text
//
// The file part must start at column 0 (no leading whitespace or bullets).
var findingLineRe = regexp.MustCompile(`^([\w./:-]+\.go):\s+(.+)$`)

// ExtractFindings parses session output and returns file-level findings.
// Lines that do not match the pattern (including bullet-prefixed lines) are
// silently skipped. The returned slice is always non-nil.
func ExtractFindings(text string) []Finding {
	findings := make([]Finding, 0)
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		m := findingLineRe.FindStringSubmatch(line)
		if len(m) == 3 {
			findings = append(findings, Finding{
				File:        m[1],
				Description: strings.TrimSpace(m[2]),
			})
		}
	}
	return findings
}

// WriteFindings persists each finding to the MemPalace knowledge graph.
//
// Errors from individual KGAdd calls are logged at DEBUG and discarded so that
// a single transient KG failure does not block the caller. A nil mp is treated
// as a no-op (MemPalace unavailable).
func WriteFindings(ctx context.Context, findings []Finding, mp MPClient, source, groupID string) error {
	if mp == nil {
		return nil
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	for _, f := range findings {
		err := mp.KGAdd(ctx, "file:"+f.File, "has_finding", f.Description, map[string]string{
			"source":   source,
			"group_id": groupID,
			"ts":       ts,
		})
		if err != nil {
			slog.Debug("parallel: KGAdd failed", "file", f.File, "err", err)
		}
	}
	return nil
}

// OnSlotDone is the post-session hook. It extracts findings from the final
// session text, writes them to MemPalace, and updates the slot status to done.
//
// All errors are logged at WARN and not returned — this is a fire-and-forget
// hook that must not block or crash the caller.
// OnSlotDoneStore is the store subset OnSlotDone needs.
type OnSlotDoneStore interface {
	UpdateSlotStatus(handle int64, status pantry.ParallelStatus, tokensIn, tokensOut int) error
}

func OnSlotDone(ctx context.Context, slot SlotRecord, groupID, finalText string, mp MPClient, store OnSlotDoneStore) {
	findings := ExtractFindings(finalText)

	if err := WriteFindings(ctx, findings, mp, slot.Provider, groupID); err != nil {
		slog.Warn("parallel: WriteFindings failed", "slot", slot.Handle, "err", err)
	}

	if err := store.UpdateSlotStatus(slot.Handle, pantry.ParallelStatusDone, slot.TokensIn, slot.TokensOut); err != nil {
		slog.Warn("parallel: UpdateSlotStatus failed", "slot", slot.Handle, "err", err)
	}
}
