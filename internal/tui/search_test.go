package tui

import "testing"

func TestFilterHistory_Empty(t *testing.T) {
	t.Parallel()
	entries := []HistoryEntry{
		{Prompt: "explain auth", Kitchen: "claude", Status: "done"},
		{Prompt: "fix login", Kitchen: "opencode", Status: "done"},
	}
	matches := FilterHistory(entries, "")
	if len(matches) != 2 {
		t.Errorf("empty query should return all, got %d", len(matches))
	}
}

func TestFilterHistory_Match(t *testing.T) {
	t.Parallel()
	entries := []HistoryEntry{
		{Prompt: "explain auth", Kitchen: "claude", Status: "done"},
		{Prompt: "fix login", Kitchen: "opencode", Status: "done"},
		{Prompt: "search owasp", Kitchen: "gemini", Status: "failed"},
	}
	matches := FilterHistory(entries, "auth")
	if len(matches) != 1 {
		t.Errorf("expected 1 match for 'auth', got %d", len(matches))
	}
	if matches[0].Kitchen != "claude" {
		t.Error("should match the claude entry")
	}
}

func TestFilterHistory_Fuzzy(t *testing.T) {
	t.Parallel()
	entries := []HistoryEntry{
		{Prompt: "explain auth middleware", Kitchen: "claude", Status: "done"},
	}
	matches := FilterHistory(entries, "eath")
	if len(matches) != 1 {
		t.Error("fuzzy match should find 'explain auth' with 'eath'")
	}
}

func TestBuildHistoryFromBlocks(t *testing.T) {
	t.Parallel()
	blocks := []Block{
		{Prompt: "task 1", Kitchen: "claude", State: StateDone, ExitCode: 0},
		{Prompt: "task 2", Kitchen: "opencode", State: StateStreaming},
		{Prompt: "task 3", Kitchen: "gemini", State: StateFailed, ExitCode: 1},
	}
	entries := BuildHistoryFromBlocks(blocks)
	if len(entries) != 2 {
		t.Errorf("expected 2 completed entries, got %d", len(entries))
	}
	// Most recent first.
	if entries[0].Prompt != "task 3" {
		t.Error("should be most recent first")
	}
	if entries[0].Status != "failed" {
		t.Error("task 3 should be failed")
	}
	if entries[1].Status != "done" {
		t.Error("task 1 should be done")
	}
}

func TestRenderSearch_NonEmpty(t *testing.T) {
	t.Parallel()
	entries := []HistoryEntry{
		{Prompt: "explain auth", Kitchen: "claude", Status: "done"},
	}
	result := RenderSearch(entries, 0, "auth", 60)
	if result == "" {
		t.Error("search render should not be empty")
	}
}
