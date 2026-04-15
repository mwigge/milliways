package conversation

import (
	"strings"
	"testing"
)

func TestBuildContinuationPrompt(t *testing.T) {
	t.Parallel()

	c := New("conv-1", "b1", "review milliways")
	c.Memory.WorkingSummary = "We already inspected routing and adapters."
	c.Memory.NextAction = "Patch the failover orchestrator."
	c.Context.SpecRefs = []string{"openspec/provider-continuity"}
	c.Context.CodeGraphText = "Relevant code: sommelier, tui, adapters."
	c.AppendTurn(RoleAssistant, "claude", "I found a routing issue.")

	out := BuildContinuationPrompt(ContinueInput{
		Conversation: c,
		NextProvider: "codex",
		Reason:       "Claude became exhausted.",
	})

	for _, want := range []string{
		"Original goal:",
		"Claude became exhausted.",
		"We already inspected routing and adapters.",
		"Patch the failover orchestrator.",
		"openspec/provider-continuity",
		"Relevant code: sommelier, tui, adapters.",
		"Continue from the current state in codex.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("continuation prompt missing %q", want)
		}
	}
}

func TestBuildContinuationPrompt_BoundsTranscript(t *testing.T) {
	t.Parallel()

	c := New("conv-1", "b1", "review milliways")
	for i := 0; i < 60; i++ {
		c.AppendTurn(RoleAssistant, "claude", strings.Repeat("x", 400))
	}

	out := BuildContinuationPrompt(ContinueInput{
		Conversation: c,
		NextProvider: "codex",
	})

	if !strings.Contains(out, "omitted to keep the continuation payload bounded") {
		t.Fatalf("expected bounded transcript notice, got %q", out)
	}
	if strings.Count(out, "[assistant:claude]") >= 60 {
		t.Fatalf("expected truncated transcript window, got full transcript")
	}
}
