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

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
	mwtools "github.com/mwigge/milliways/internal/tools"
)

// gateToolParams is the request a PreToolUse hook (via `milliwaysctl tool-gate`)
// sends to gate one external-CLI tool call (e.g. claude's Bash/Edit).
type gateToolParams struct {
	SessionID  string         `json:"session_id"`
	Client     string         `json:"client"`
	ToolName   string         `json:"tool_name"`
	ToolInput  map[string]any `json:"tool_input"`
	ToolCallID string         `json:"tool_use_id"`
	CWD        string         `json:"cwd"`
}

// gateToolResult is the allow/deny decision returned to the hook, which maps it
// to claude's PreToolUse permissionDecision.
type gateToolResult struct {
	Decision string `json:"decision"` // "allow" | "deny"
	Reason   string `json:"reason,omitempty"`
}

// gateToolApprovalTimeout bounds how long a pending approval blocks the calling
// hook (and thus claude's tool call) before it is auto-denied.
const gateToolApprovalTimeout = 30 * time.Minute

// securityGateTool handles "security.gate_tool". External CLIs (claude) run
// their own tools, so milliways can't intercept them in-process the way it does
// for the local/minimax runners; instead a PreToolUse hook calls here. The
// permission policy decides allow/deny outright, or — for "ask" — records a
// pending approval, surfaces it to the session, and blocks until the user
// responds via /approve or /deny (approval.respond).
func (s *Server) securityGateTool(enc *json.Encoder, req *Request) {
	var p gateToolParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	writeResult(enc, req.ID, s.runGateTool(p))
}

func (s *Server) runGateTool(p gateToolParams) gateToolResult {
	toolName := strings.TrimSpace(p.ToolName)
	if toolName == "" {
		return gateToolResult{Decision: "allow", Reason: "no tool name; nothing to gate"}
	}

	perm := mwtools.NewPermissionPolicy(codingPermissionModeFromEnv()).
		Evaluate(mwtools.BuildToolPermissionRequest(toolName, p.ToolInput))

	switch perm.Decision {
	case mwtools.PermissionAllow:
		return gateToolResult{Decision: "allow", Reason: perm.Reason}
	case mwtools.PermissionDeny:
		return gateToolResult{Decision: "deny", Reason: perm.Reason}
	}

	// PermissionAsk: needs a human decision.
	if s.pantryDB == nil {
		// No store to record/await an approval — fall back to allow rather than
		// bricking the agent (the user explicitly wants "ask, then run", not a
		// hard block when the approval store is unavailable).
		return gateToolResult{Decision: "allow", Reason: "approval store unavailable; allowing"}
	}
	store := s.pantryDB.Coding()
	approvalID, err := recordToolApproval(store, pantry.ToolApproval{
		Workspace:  strings.TrimSpace(p.CWD),
		SessionID:  strings.TrimSpace(p.SessionID),
		Client:     fallbackString(p.Client, "claude"),
		ToolCallID: p.ToolCallID,
		ToolName:   toolName,
		Operation:  string(perm.Request.Operation),
		Path:       perm.Request.Path,
		Preview:    sensitiveToolPreview(perm.Request),
		Decision:   "pending",
		Reason:     perm.Reason,
		CreatedAt:  time.Now().UTC(),
	}, perm.Decision)
	if err != nil {
		return gateToolResult{Decision: "deny", Reason: "could not record approval: " + err.Error()}
	}

	// Surface the pending approval to the session so the TUI shows it live
	// (the user can also see it via /approvals). Best-effort.
	s.notifySessionApprovalPrompt(p.SessionID, fallbackString(p.Client, "claude"), approvalID, toolName, perm)

	parent := s.bgCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, gateToolApprovalTimeout)
	defer cancel()

	decision, err := waitForToolApproval(ctx, store, approvalID)
	if err != nil {
		markPendingToolApprovalDenied(store, approvalID, err.Error())
		return gateToolResult{Decision: "deny", Reason: "approval not granted: " + err.Error()}
	}
	if strings.EqualFold(decision, "approve") {
		return gateToolResult{Decision: "allow", Reason: "approved by user"}
	}
	return gateToolResult{Decision: "deny", Reason: "denied by user"}
}

// notifySessionApprovalPrompt pushes an approval_prompt event to the matching
// session's subscriber streams so the TUI surfaces it immediately. The fields
// mirror what the chat client's approval renderer reads (client/operation/
// action); the approval id is folded into the action so the user sees how to
// respond.
func (s *Server) notifySessionApprovalPrompt(sessionID, client string, approvalID int64, toolName string, perm mwtools.ToolPermissionResult) {
	if s.agents == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	action := toolName
	if p := strings.TrimSpace(perm.Request.Path); p != "" {
		action += " " + p
	}
	action += fmt.Sprintf(" — /approve %d  (or /deny %d)", approvalID, approvalID)
	event := map[string]any{
		"t":           "approval_prompt",
		"client":      client,
		"operation":   string(perm.Request.Operation),
		"action":      action,
		"approval_id": approvalID,
		"reason":      perm.Reason,
	}
	s.agents.PushToSession(sessionID, event)
}

func fallbackString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
