package observability

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateTimeline(t *testing.T) {
	t.Parallel()

	events := []AgentTraceEvent{
		{SessionID: "sess-1", Timestamp: time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC), Type: "tool.called", Description: "Bash"},
		{SessionID: "sess-1", Timestamp: time.Date(2026, 4, 20, 10, 1, 0, 0, time.UTC), Type: "delegate", Description: "coder-go"},
	}

	got := GenerateTimeline(events)
	for _, want := range []string{
		"timeline",
		"title Agent Session sess-1",
		"2026-04-20 10:00:00Z : tool.called: Bash",
		"2026-04-20 10:01:00Z : delegate: coder-go",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("GenerateTimeline() missing %q:\n%s", want, got)
		}
	}
}

func TestGenerateCallGraph(t *testing.T) {
	t.Parallel()

	events := []AgentTraceEvent{
		{Type: "delegate", Description: "coder-go", Data: map[string]any{"to": "delegate: coder-go"}},
		{Type: "tool.called", Description: "Bash", Data: map[string]any{"tool_name": "Bash"}},
	}

	got := GenerateCallGraph(events)
	for _, want := range []string{
		"flowchart TD",
		"Orchestrator",
		"delegate: coder-go",
		"tool: Bash",
		"-->",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("GenerateCallGraph() missing %q:\n%s", want, got)
		}
	}
}

func TestGenerateDecisionTree(t *testing.T) {
	t.Parallel()

	events := []AgentTraceEvent{{
		Type:        "routing.decision",
		Description: "Choose kitchen",
		Data: map[string]any{
			"options": []any{"claude", "opencode"},
			"choice":  "opencode",
		},
	}}

	got := GenerateDecisionTree(events)
	for _, want := range []string{"flowchart TD", "Choose kitchen", "claude", "opencode ✓"} {
		if !strings.Contains(got, want) {
			t.Fatalf("GenerateDecisionTree() missing %q:\n%s", want, got)
		}
	}
}
