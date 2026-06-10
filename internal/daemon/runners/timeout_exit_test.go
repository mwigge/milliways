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

package runners

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunnerRequestTimeoutOrDefault(t *testing.T) {
	const def = 6 * time.Hour

	t.Run("unset uses default", func(t *testing.T) {
		t.Setenv("CLAUDE_TIMEOUT", "")
		if got := runnerRequestTimeoutOrDefault("CLAUDE_TIMEOUT", def); got != def {
			t.Fatalf("unset: got %v, want default %v", got, def)
		}
	})

	t.Run("off disables the cap", func(t *testing.T) {
		for _, v := range []string{"off", "none", "0"} {
			t.Setenv("CLAUDE_TIMEOUT", v)
			if got := runnerRequestTimeoutOrDefault("CLAUDE_TIMEOUT", def); got != 0 {
				t.Fatalf("CLAUDE_TIMEOUT=%q: got %v, want 0 (disabled)", v, got)
			}
		}
	})

	t.Run("explicit duration overrides default", func(t *testing.T) {
		t.Setenv("CLAUDE_TIMEOUT", "90m")
		if got := runnerRequestTimeoutOrDefault("CLAUDE_TIMEOUT", def); got != 90*time.Minute {
			t.Fatalf("got %v, want 90m", got)
		}
	})
}

func TestExitMsg_SignalKillIsLegible(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil { // SIGKILL → ExitCode() == -1
		t.Fatalf("kill: %v", err)
	}
	waitErr := cmd.Wait()
	if waitErr == nil {
		t.Fatal("expected a wait error after SIGKILL")
	}
	msg := exitMsg("claude", waitErr, nil)
	if !strings.Contains(msg, "was killed") {
		t.Fatalf("msg = %q, want it to report the signal kill (not opaque code -1)", msg)
	}
	if strings.Contains(msg, "code -1") {
		t.Fatalf("msg = %q, should not surface raw 'code -1' for a signal kill", msg)
	}
}

func TestExitMsg_NormalExitKeepsCodeAndStderr(t *testing.T) {
	waitErr := exec.Command("sh", "-c", "exit 3").Run()
	if waitErr == nil {
		t.Fatal("expected a non-zero exit error")
	}
	msg := exitMsg("codex", waitErr, []string{"some context", "fatal: boom"})
	if !strings.Contains(msg, "exited (code 3)") {
		t.Fatalf("msg = %q, want 'exited (code 3)'", msg)
	}
	if !strings.Contains(msg, "fatal: boom") {
		t.Fatalf("msg = %q, want last stderr line appended", msg)
	}
}
