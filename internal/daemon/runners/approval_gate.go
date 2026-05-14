package runners

import "strings"

const approvalGatePromptSuffix = "\n\nMilliWays approval gate: reply `y` to implement this plan, or `n` to cancel. No tools have been run."

type approvalGatePending struct {
	OriginalPrompt string
	Plan           string
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

func approvalGateNeedsInput(stream Pusher, chunk map[string]any) {
	stream.Push(encodeData(approvalGatePromptSuffix + "\n"))
	chunk["needs_input"] = true
	chunk["approval_required"] = true
	stream.Push(chunk)
}
