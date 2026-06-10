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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mwigge/milliways/internal/rpc"
)

// toolGateHookInput is the PreToolUse payload claude writes to the hook's stdin.
type toolGateHookInput struct {
	SessionID string         `json:"session_id"`
	CWD       string         `json:"cwd"`
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
	ToolUseID string         `json:"tool_use_id"`
}

// toolGateResult mirrors the daemon's security.gate_tool result.
type toolGateResult struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// runToolGate implements `milliwaysctl tool-gate`: a Claude Code PreToolUse hook
// that asks milliwaysd whether a tool call is allowed, blocking on the user's
// approval when the policy says "ask". It always emits a PreToolUse decision on
// stdout. Infrastructure failures fail OPEN (allow) so a misconfigured or
// unreachable gate does not brick the agent — the user's intent is "ask, then
// run", not a hard block when the gate itself is broken.
func runToolGate(_ []string) int {
	raw, _ := io.ReadAll(os.Stdin)
	var in toolGateHookInput
	_ = json.Unmarshal(raw, &in)

	decision, reason := toolGateDecision(in)

	out, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       decision,
			"permissionDecisionReason": reason,
		},
	})
	fmt.Fprintln(os.Stdout, string(out))
	return 0
}

func toolGateDecision(in toolGateHookInput) (decision, reason string) {
	socket := strings.TrimSpace(os.Getenv("MILLIWAYS_DAEMON_SOCKET"))
	if socket == "" {
		socket = defaultSocket()
	}
	client, err := rpc.Dial(socket)
	if err != nil {
		return "allow", "milliways gate unreachable; allowing"
	}
	defer func() { _ = client.Close() }()

	sessionID := strings.TrimSpace(os.Getenv("MILLIWAYS_SESSION_ID"))
	if sessionID == "" {
		sessionID = in.SessionID
	}
	clientName := strings.TrimSpace(os.Getenv("MILLIWAYS_CLIENT_ID"))
	if clientName == "" {
		clientName = "claude"
	}

	params := map[string]any{
		"session_id":  sessionID,
		"client":      clientName,
		"tool_name":   in.ToolName,
		"tool_input":  in.ToolInput,
		"tool_use_id": in.ToolUseID,
		"cwd":         in.CWD,
	}
	// This call blocks until the user responds (or the daemon's approval
	// timeout) when the policy says "ask" — that's intentional.
	var res toolGateResult
	if err := client.Call("security.gate_tool", params, &res); err != nil {
		return "allow", "milliways gate error; allowing: " + err.Error()
	}
	if strings.EqualFold(res.Decision, "deny") {
		if strings.TrimSpace(res.Reason) == "" {
			return "deny", "denied by milliways"
		}
		return "deny", res.Reason
	}
	return "allow", res.Reason
}
