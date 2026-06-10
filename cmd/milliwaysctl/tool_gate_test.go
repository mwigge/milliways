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
	"path/filepath"
	"strings"
	"testing"
)

// TestToolGateDecision_FailsOpenWhenDaemonUnreachable verifies the hook allows
// the operation (rather than hard-blocking the agent) when it can't reach the
// daemon — the user's intent is "ask, then run", not "block when gate broken".
func TestToolGateDecision_FailsOpenWhenDaemonUnreachable(t *testing.T) {
	t.Setenv("MILLIWAYS_DAEMON_SOCKET", filepath.Join(t.TempDir(), "nonexistent.sock"))

	dec, reason := toolGateDecision(toolGateHookInput{
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "echo hi"},
	})
	if dec != "allow" {
		t.Fatalf("decision = %q, want allow (fail-open); reason=%q", dec, reason)
	}
	if !strings.Contains(strings.ToLower(reason), "unreachable") && !strings.Contains(strings.ToLower(reason), "allowing") {
		t.Fatalf("reason = %q, want it to explain the fail-open", reason)
	}
}
