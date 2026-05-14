package runners

import (
	"strings"
	"time"
)

const approvalGateDefaultTTL = 10 * time.Minute

type approvalGateRequest struct {
	Client       string
	Workspace    string
	Operation    string
	RiskCategory string
	RiskReason   string
	CommandText  string
	ActionText   string
	ExpiresAt    time.Time
}

type approvalGatePending struct {
	OriginalPrompt string
	Plan           string
	Request        approvalGateRequest
}

func approvalGateNeedsPlan(prompt string) bool {
	text := strings.ToLower(strings.Join(strings.Fields(prompt), " "))
	if text == "" {
		return false
	}
	verbs := []string{
		"implement",
		"fix",
		"change",
		"edit",
		"write",
		"create",
		"remove",
		"delete",
		"update",
		"install",
		"run",
		"execute",
		"commit",
		"push",
		"merge",
		"release",
		"format",
		"refactor",
	}
	for _, verb := range verbs {
		if strings.Contains(text, verb) {
			return true
		}
	}
	for _, phrase := range []string{
		"add feature",
		"add test",
		"add file",
		"add support",
		"add code",
		"add implementation",
		"add endpoint",
		"add command",
		"add dependency",
		"add package",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func approvalGateDecision(reply string) (approved bool, rejected bool) {
	text := strings.ToLower(strings.TrimSpace(reply))
	text = strings.Trim(text, ".! \t\r\n")
	switch text {
	case "y", "yes", "approve", "approved", "proceed", "continue", "go", "ok", "okay", "implement":
		return true, false
	case "n", "no", "cancel", "stop", "abort", "reject", "rejected":
		return false, true
	default:
		return false, false
	}
}

func approvalGateNewRequest(client, workspace, actionText string, now time.Time) approvalGateRequest {
	actionText = strings.TrimSpace(actionText)
	return approvalGateRequest{
		Client:       strings.TrimSpace(client),
		Workspace:    strings.TrimSpace(workspace),
		Operation:    approvalGateOperation(actionText),
		RiskCategory: "tool-execution",
		RiskReason:   "The requested action may run tools or modify the workspace.",
		CommandText:  actionText,
		ActionText:   actionText,
		ExpiresAt:    approvalGateExpiresAt(now),
	}
}

func approvalGateExpiresAt(now time.Time) time.Time {
	return now.Add(approvalGateDefaultTTL).UTC()
}

func approvalGateExpired(req approvalGateRequest, now time.Time) bool {
	return !req.ExpiresAt.IsZero() && !now.Before(req.ExpiresAt)
}

func approvalGateOperation(actionText string) string {
	text := strings.ToLower(strings.Join(strings.Fields(actionText), " "))
	for _, verb := range []string{
		"implement",
		"fix",
		"change",
		"edit",
		"write",
		"create",
		"remove",
		"delete",
		"update",
		"install",
		"run",
		"execute",
		"commit",
		"push",
		"merge",
		"release",
		"format",
		"refactor",
	} {
		if strings.Contains(text, verb) {
			return verb
		}
	}
	return "implement"
}

func approvalGatePrompt(req approvalGateRequest) string {
	workspace := req.Workspace
	if workspace == "" {
		workspace = "(default)"
	}
	return "\n\nMilliWays approval gate: reply `y` to implement this one action, or `n` to cancel. No tools have been run.\n" +
		"Client: " + fallback(req.Client, "unknown") + "\n" +
		"Workspace: " + workspace + "\n" +
		"Operation: " + fallback(req.Operation, "implement") + "\n" +
		"Risk: " + fallback(req.RiskCategory, "tool-execution") + " - " + fallback(req.RiskReason, "requires explicit approval") + "\n" +
		"Action: " + fallback(req.ActionText, "(unspecified)") + "\n" +
		"Expires at: " + req.ExpiresAt.Format(time.RFC3339)
}

func approvalGatePlanPrompt(task string) string {
	return "Prepare an implementation plan for the user's task. Do not call tools, do not run commands, do not modify files, and do not claim that work is complete. " +
		"Return only a concise plan, risks, and verification steps.\n\nTask:\n" + task
}

func approvalGateImplementPrompt(task, plan string) string {
	return "The user approved implementation. Implement the approved plan now.\n\nOriginal task:\n" + task + "\n\nApproved plan:\n" + plan
}

func approvalGateCancelled(stream Pusher) {
	stream.Push(encodeData("Implementation cancelled.\n"))
	chunk := zeroUsageChunkEnd()
	chunk["approval_cancelled"] = true
	stream.Push(chunk)
}

func approvalGateExpiredInput(stream Pusher) {
	stream.Push(encodeData("Approval expired. Please send the request again.\n"))
	chunk := zeroUsageChunkEnd()
	chunk["approval_expired"] = true
	chunk["needs_input"] = true
	stream.Push(chunk)
}

func approvalGateNeedsInput(stream Pusher, chunk map[string]any, req approvalGateRequest) {
	stream.Push(encodeData(approvalGatePrompt(req) + "\n"))
	chunk["needs_input"] = true
	chunk["approval_required"] = true
	chunk["approval_request"] = map[string]any{
		"client":        req.Client,
		"workspace":     req.Workspace,
		"operation":     req.Operation,
		"risk_category": req.RiskCategory,
		"risk_reason":   req.RiskReason,
		"command_text":  req.CommandText,
		"action_text":   req.ActionText,
		"expires_at":    req.ExpiresAt.Format(time.RFC3339),
	}
	stream.Push(chunk)
}
