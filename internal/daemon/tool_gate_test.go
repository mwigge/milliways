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

package daemon

import "testing"

func TestRunGateTool_Decisions(t *testing.T) {
	t.Setenv("MILLIWAYS_PERMISSION_MODE", "") // default → auto mode
	s := &Server{}                            // nil pantryDB

	t.Run("empty tool name allows", func(t *testing.T) {
		if got := s.runGateTool(gateToolParams{ToolName: "  "}); got.Decision != "allow" {
			t.Fatalf("decision = %q, want allow", got.Decision)
		}
	})

	t.Run("ask without approval store fails open to allow", func(t *testing.T) {
		// An unknown tool → auto mode "asks"; with no pantryDB to record/await an
		// approval, the gate must fail open (allow) rather than brick the agent.
		got := s.runGateTool(gateToolParams{ToolName: "Frobnicate", CWD: t.TempDir()})
		if got.Decision != "allow" {
			t.Fatalf("decision = %q, want allow (fail-open); reason=%q", got.Decision, got.Reason)
		}
	})
}
