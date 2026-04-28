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

package ledger

import (
	"strconv"
	"time"

	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/pantry"
)

// LedgerSink writes ledger entries when the orchestrator emits segment_end events.
// It implements observability.Sink.
type LedgerSink struct {
	db *pantry.DB
}

// NewLedgerSink returns a LedgerSink that writes segment_end events to mw_ledger.
func NewLedgerSink(db *pantry.DB) *LedgerSink {
	return &LedgerSink{db: db}
}

// Emit writes a ledger entry for segment_end events.
func (s *LedgerSink) Emit(evt observability.Event) {
	if s.db == nil || evt.Kind != "segment_end" {
		return
	}
	status := evt.Fields["status"]
	reason := evt.Fields["reason"]
	provider := evt.Provider
	if provider == "" {
		provider = "unknown"
	}

	segIndex := 0
	if idx := evt.Fields["segment_index"]; idx != "" {
		if n, err := strconv.Atoi(idx); err == nil {
			segIndex = n
		}
	}

	exitCode := 0
	outcome := statusToOutcome(status)
	if outcome == "exhausted" || outcome == "failure" {
		exitCode = 1
	}

	entry := pantry.LedgerEntry{
		Timestamp:      evt.At.UTC().Format(time.RFC3339),
		Kitchen:        provider,
		SegmentIndex:   segIndex + 1, // 1-based
		EndReason:      reason,
		ExitCode:       exitCode,
		Outcome:        outcome,
		ConversationID: evt.ConversationID,
		SegmentID:      evt.SegmentID,
	}

	if _, err := s.db.Ledger().Insert(entry); err != nil {
		// Best-effort — do not block the event stream
	}
}

func statusToOutcome(status string) string {
	switch status {
	case "done":
		return "success"
	case "exhausted":
		return "exhausted"
	default:
		return "failure"
	}
}
