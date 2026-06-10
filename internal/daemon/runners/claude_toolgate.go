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
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// claudeToolGateHookTimeoutSecs bounds how long claude waits for the PreToolUse
// hook (and thus a pending approval) before treating it as failed. Generous so
// a human has time to /approve.
const claudeToolGateHookTimeoutSecs = 1800

var (
	tgOnce         sync.Once
	tgSettingsPath string
)

// claudeToolGateArgs returns the claude args that install milliways' PreToolUse
// approval hook (`--settings <file>`), or nil when disabled or unavailable.
//
// Claude executes its own tools, so milliways can't intercept them in-process
// the way it gates the local/minimax runners. Instead claude runs the
// `milliwaysctl tool-gate` PreToolUse hook for each tool call; the hook asks
// the daemon (security.gate_tool), which applies the permission policy and —
// for "ask" operations — blocks until the user responds via /approve or /deny.
// Disable with MILLIWAYS_TOOL_GATE=off.
func claudeToolGateArgs() []string {
	if claudeToolGateDisabled() {
		return nil
	}
	tgOnce.Do(initClaudeToolGate)
	if tgSettingsPath == "" {
		return nil
	}
	return []string{"--settings", tgSettingsPath}
}

func claudeToolGateDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MILLIWAYS_TOOL_GATE"))) {
	case "off", "0", "false", "none", "disable", "disabled":
		return true
	default:
		return false
	}
}

func initClaudeToolGate() {
	ctlBin, err := exec.LookPath("milliwaysctl")
	if err != nil || strings.TrimSpace(ctlBin) == "" {
		// claude runs the hook through a shell, which may still resolve it on
		// PATH; if not, the hook fails open (allow).
		ctlBin = "milliwaysctl"
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": claudeShellQuote(ctlBin) + " tool-gate",
							"timeout": claudeToolGateHookTimeoutSecs,
						},
					},
				},
			},
		},
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return
	}
	f, err := os.CreateTemp("", "milliways-claude-settings-*.json")
	if err != nil {
		return
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return
	}
	tgSettingsPath = f.Name()
}

// claudeShellQuote single-quotes s for safe inclusion in the hook's shell
// command string when it contains shell-significant characters.
func claudeShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`(){}[]*?|&;<>") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
