package main

import (
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/rpc"
)

func TestRenderParallelComparisonShowsColumnsAgreementAndConsensus(t *testing.T) {
	status := rpc.GroupStatusResult{
		GroupID: "group-123456789",
		Prompt:  "review checkout flow",
		Status:  "running",
		Slots: []rpc.GroupSlotStatus{
			{
				Provider:  "codex",
				Status:    "done",
				TokensOut: 1200,
				Text:      "shared issue: missing nil check\ncodex only: rename confusing helper",
			},
			{
				Provider:  "gemini",
				Status:    "streaming",
				TokensOut: 900,
				Text:      "shared issue: missing nil check\ngemini only: add integration coverage",
			},
		},
	}
	out := renderParallelComparison(status, "consensus summary\n", 100)
	for _, want := range []string{
		"parallel comparison",
		"codex done",
		"gemini streaming",
		"shared issue: missing nil check",
		"codex: codex only",
		"gemini: gemini only",
		"consensus summary",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered comparison missing %q:\n%s", want, out)
		}
	}
}

func TestParseParallelViewArgs(t *testing.T) {
	watch, groupID := parseParallelViewArgs("--watch group-1")
	if !watch || groupID != "group-1" {
		t.Fatalf("parseParallelViewArgs = %v/%q, want watch group-1", watch, groupID)
	}
	watch, groupID = parseParallelViewArgs("group-2")
	if watch || groupID != "group-2" {
		t.Fatalf("parseParallelViewArgs = %v/%q, want no-watch group-2", watch, groupID)
	}
}

func TestParallelGroupDoneTreatsStreamingAsRunning(t *testing.T) {
	status := rpc.GroupStatusResult{Slots: []rpc.GroupSlotStatus{{Status: "done"}, {Status: "streaming"}}}
	if parallelGroupDone(status) {
		t.Fatal("parallelGroupDone = true, want false while a slot streams")
	}
	status.Slots[1].Status = "done"
	if !parallelGroupDone(status) {
		t.Fatal("parallelGroupDone = false, want true after all slots terminal")
	}
}
