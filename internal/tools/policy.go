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

package tools

import (
	"fmt"
	"strings"
)

// PermissionDecision is the runner-facing approval outcome for a tool call.
type PermissionDecision string

const (
	PermissionAllow PermissionDecision = "allow"
	PermissionAsk   PermissionDecision = "ask"
	PermissionDeny  PermissionDecision = "deny"
)

// PermissionMode selects the default approval posture when no explicit rule
// matches.
type PermissionMode string

const (
	PermissionModeDefault  PermissionMode = "default"
	PermissionModeReadOnly PermissionMode = "read_only"
	PermissionModeAuto     PermissionMode = "auto"
)

// ToolOperation is a coarse operation category runners can reason about
// independently of provider-specific tool names.
type ToolOperation string

const (
	OperationUnknown    ToolOperation = "unknown"
	OperationRead       ToolOperation = "read"
	OperationSearch     ToolOperation = "search"
	OperationWrite      ToolOperation = "write"
	OperationEdit       ToolOperation = "edit"
	OperationApplyPatch ToolOperation = "apply_patch"
	OperationDelete     ToolOperation = "delete"
	OperationExecute    ToolOperation = "execute"
	OperationNetwork    ToolOperation = "network"
	OperationTodo       ToolOperation = "todo"
	OperationQuestion   ToolOperation = "question"
)

// ToolPermissionRequest is the normalized input a runner can pass to a
// permission evaluator before invoking a handler.
type ToolPermissionRequest struct {
	ToolName  string         `json:"tool_name"`
	Operation ToolOperation  `json:"operation"`
	Path      string         `json:"path,omitempty"`
	Command   string         `json:"command,omitempty"`
	URL       string         `json:"url,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ToolPermissionResult is a stable runner-facing policy response.
type ToolPermissionResult struct {
	Decision PermissionDecision     `json:"decision"`
	Reason   string                 `json:"reason,omitempty"`
	Request  ToolPermissionRequest  `json:"request"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// PermissionRule overrides mode defaults for a tool name or operation.
type PermissionRule struct {
	ToolName        string
	Operation       ToolOperation
	PathPrefix      string
	CommandContains string
	Decision        PermissionDecision
	Reason          string
}

// PermissionPolicy evaluates tool calls for allow/ask/deny decisions.
type PermissionPolicy struct {
	Mode  PermissionMode
	Rules []PermissionRule
}

func NewPermissionPolicy(mode PermissionMode) PermissionPolicy {
	if mode == "" {
		mode = PermissionModeDefault
	}
	return PermissionPolicy{Mode: mode}
}

func (p PermissionPolicy) Evaluate(req ToolPermissionRequest) ToolPermissionResult {
	if req.Operation == "" {
		req.Operation = operationForTool(req.ToolName)
	}
	req.ToolName = strings.TrimSpace(req.ToolName)

	if req.Path != "" && isPathOperation(req.Operation) {
		if resolved, err := containedPath(req.Path); err != nil {
			return ToolPermissionResult{
				Decision: PermissionDeny,
				Reason:   fmt.Sprintf("path refused: %v", err),
				Request:  req,
			}
		} else {
			req.Path = resolved
		}
	}

	for _, rule := range p.Rules {
		if !ruleMatches(rule, req) {
			continue
		}
		reason := rule.Reason
		if reason == "" {
			reason = "matched explicit permission rule"
		}
		return ToolPermissionResult{Decision: rule.Decision, Reason: reason, Request: req}
	}

	decision, reason := p.defaultDecision(req.Operation)
	return ToolPermissionResult{Decision: decision, Reason: reason, Request: req}
}

func (p PermissionPolicy) defaultDecision(op ToolOperation) (PermissionDecision, string) {
	switch p.Mode {
	case PermissionModeAuto:
		if op == OperationDelete {
			return PermissionAsk, "auto mode asks before deleting files"
		}
		if op == OperationUnknown {
			return PermissionAsk, "auto mode asks before unknown tool operation"
		}
		return PermissionAllow, "auto mode allows registered tool operation"
	case PermissionModeReadOnly:
		switch op {
		case OperationRead, OperationSearch, OperationTodo, OperationQuestion:
			return PermissionAllow, "read-only mode allows non-mutating tool operation"
		case OperationNetwork:
			return PermissionAsk, "read-only mode asks before network access"
		default:
			return PermissionDeny, "read-only mode denies mutating or executing tool operation"
		}
	default:
		switch op {
		case OperationRead, OperationSearch, OperationTodo, OperationQuestion:
			return PermissionAllow, "default mode allows non-mutating tool operation"
		default:
			return PermissionAsk, "default mode asks before sensitive tool operation"
		}
	}
}

func ruleMatches(rule PermissionRule, req ToolPermissionRequest) bool {
	if rule.ToolName != "" && !strings.EqualFold(rule.ToolName, req.ToolName) {
		return false
	}
	if rule.Operation != "" && rule.Operation != req.Operation {
		return false
	}
	if rule.PathPrefix != "" && !strings.HasPrefix(req.Path, rule.PathPrefix) {
		return false
	}
	if rule.CommandContains != "" && !strings.Contains(req.Command, rule.CommandContains) {
		return false
	}
	return true
}

func BuildToolPermissionRequest(toolName string, args map[string]any) ToolPermissionRequest {
	req := ToolPermissionRequest{
		ToolName:  toolName,
		Operation: operationForTool(toolName),
	}
	if path, ok := pathArg(args); ok {
		req.Path = path
	}
	if command, ok := stringArg(args, "command"); ok {
		req.Command = command
	}
	if rawURL, ok := stringArg(args, "url"); ok {
		req.URL = rawURL
	}
	return req
}

func operationForTool(toolName string) ToolOperation {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "read":
		return OperationRead
	case "grep", "glob":
		return OperationSearch
	case "write":
		return OperationWrite
	case "edit":
		return OperationEdit
	case "applypatch", "apply_patch":
		return OperationApplyPatch
	case "delete":
		return OperationDelete
	case "bash":
		return OperationExecute
	case "webfetch":
		return OperationNetwork
	case "todo":
		return OperationTodo
	case "question":
		return OperationQuestion
	default:
		return OperationUnknown
	}
}

func isPathOperation(op ToolOperation) bool {
	switch op {
	case OperationRead, OperationSearch, OperationWrite, OperationEdit, OperationApplyPatch, OperationDelete:
		return true
	default:
		return false
	}
}
