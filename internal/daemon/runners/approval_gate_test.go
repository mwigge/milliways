package runners

import (
	"strings"
	"testing"
	"time"
)

func TestApprovalGateNewRequestIncludesCentralFields(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.FixedZone("CEST", 2*60*60))

	req := approvalGateNewRequest(AgentIDMiniMax, "/repo", "run the formatter", now)

	if req.Client != AgentIDMiniMax {
		t.Fatalf("client = %q, want %q", req.Client, AgentIDMiniMax)
	}
	if req.Workspace != "/repo" {
		t.Fatalf("workspace = %q, want /repo", req.Workspace)
	}
	if req.Operation != "run" {
		t.Fatalf("operation = %q, want run", req.Operation)
	}
	if req.RiskCategory == "" || req.RiskReason == "" {
		t.Fatalf("risk fields must be set: %#v", req)
	}
	if req.CommandText != "run the formatter" {
		t.Fatalf("command text = %q, want run the formatter", req.CommandText)
	}
	if req.ActionText != "run the formatter" {
		t.Fatalf("action text = %q, want run the formatter", req.ActionText)
	}
	if got, want := req.ExpiresAt, now.Add(approvalGateDefaultTTL).UTC(); !got.Equal(want) {
		t.Fatalf("expires_at = %s, want %s", got, want)
	}
}

func TestApprovalGateExpiryBoundary(t *testing.T) {
	now := time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)
	req := approvalGateNewRequest(AgentIDLocal, "", "implement the feature", now)

	if approvalGateExpired(req, req.ExpiresAt.Add(-time.Nanosecond)) {
		t.Fatal("request expired before expires_at")
	}
	if !approvalGateExpired(req, req.ExpiresAt) {
		t.Fatal("request did not expire at expires_at")
	}
	if !approvalGateExpired(req, req.ExpiresAt.Add(time.Nanosecond)) {
		t.Fatal("request did not expire after expires_at")
	}
}

func TestApprovalGatePromptUsesRequestModel(t *testing.T) {
	now := time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)
	req := approvalGateNewRequest(AgentIDLocal, "/workspace", "delete generated files", now)

	prompt := approvalGatePrompt(req)
	for _, want := range []string{
		"reply `y` to implement this one action",
		"Client: local",
		"Workspace: /workspace",
		"Operation: delete",
		"Risk: tool-execution",
		"Action: delete generated files",
		"Expires at: 2026-05-14T08:10:00Z",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
