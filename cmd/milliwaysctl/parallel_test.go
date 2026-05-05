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

// TestParallelList_NoGroups verifies that `milliwaysctl parallel list`
// with an empty groups array prints "no groups found".
func TestParallelList_NoGroups(t *testing.T) {
	t.Parallel()

	var out, errw bytes.Buffer
	// emptyGroups simulates the daemon returning an empty list.
	emptyGroups := map[string]any{
		"groups": []any{},
	}
	renderParallelList(emptyGroups, &out)

	if !strings.Contains(out.String(), "no groups found") {
		t.Errorf("expected 'no groups found'; got: %q", out.String())
		_ = errw.String() // suppress unused warning
	}
}

// TestParallelList_WithGroups verifies that `milliwaysctl parallel list`
// renders the table columns for non-empty groups.
func TestParallelList_WithGroups(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	groups := map[string]any{
		"groups": []any{
			map[string]any{
				"group_id":   "abcd1234-0000-0000-0000-000000000000",
				"prompt":     "review internal/server/ for performance issues",
				"status":     "done",
				"created_at": "2026-05-05T10:00:00Z",
				"slot_count": float64(2),
			},
		},
	}
	renderParallelList(groups, &out)

	rendered := out.String()
	for _, want := range []string{"abcd1234", "done", "GROUP ID", "STATUS"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("expected %q in list output; got:\n%s", want, rendered)
		}
	}
}

// TestParallelStatus_UnknownGroup verifies that parallel status with an
// unknown group returns exit code 1 and prints an error.
func TestParallelStatus_UnknownGroup(t *testing.T) {
	t.Parallel()

	var errw bytes.Buffer
	code := runParallelStatus([]string{"no-such-group"}, nil, &errw)
	if code != 1 {
		t.Errorf("expected exit code 1 for unknown group; got %d", code)
	}
	if errw.Len() == 0 {
		t.Error("expected error output for unknown group")
	}
}

// TestParallelStatus_MissingArg verifies that `parallel status` with no
// argument returns exit code 2.
func TestParallelStatus_MissingArg(t *testing.T) {
	t.Parallel()

	var errw bytes.Buffer
	code := runParallelStatus([]string{}, nil, &errw)
	if code != 2 {
		t.Errorf("expected exit code 2 for missing group_id; got %d", code)
	}
}

// TestParallelFlagParsing verifies the sub-command dispatch for
// `milliwaysctl parallel <verb>`.
func TestParallelFlagParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{
			name:     "no subcommand",
			args:     []string{},
			wantCode: 2,
		},
		{
			name:     "help flag",
			args:     []string{"--help"},
			wantCode: 0,
		},
		{
			name:     "unknown verb",
			args:     []string{"frobnicate"},
			wantCode: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out, errw bytes.Buffer
			code := runParallel(tt.args, &out, &errw)
			if code != tt.wantCode {
				t.Errorf("runParallel(%v) = %d, want %d; stdout=%q stderr=%q",
					tt.args, code, tt.wantCode, out.String(), errw.String())
			}
		})
	}
}

// TestRenderParallelStatusTable verifies that renderParallelStatusTable
// produces the expected column headers and slot data.
func TestRenderParallelStatusTable(t *testing.T) {
	t.Parallel()

	statusResult := map[string]any{
		"group_id":   "test-group",
		"prompt":     "test prompt",
		"status":     "done",
		"created_at": "2026-05-05T10:00:00Z",
		"slots": []any{
			map[string]any{
				"handle":      float64(1),
				"provider":    "claude",
				"status":      "done",
				"started_at":  "2026-05-05T10:00:01Z",
				"completed_at": "2026-05-05T10:02:33Z",
				"tokens_in":   float64(1234),
				"tokens_out":  float64(567),
			},
		},
	}

	var out bytes.Buffer
	renderParallelStatus(statusResult, &out)

	rendered := out.String()
	for _, want := range []string{"SLOT", "PROVIDER", "STATUS", "claude", "done"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("expected %q in status output; got:\n%s", want, rendered)
		}
	}
}
