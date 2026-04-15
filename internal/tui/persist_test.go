package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
)

func TestSaveAndLoadSession(t *testing.T) {
	// Use a temp dir to avoid polluting real config.
	tmpDir := t.TempDir()
	origBase := SessionsBaseDir
	SessionsBaseDir = tmpDir
	t.Cleanup(func() { SessionsBaseDir = origBase })

	blocks := []Block{
		{
			ID:             "b1",
			ConversationID: "conv-1",
			Prompt:         "explain auth",
			Kitchen:        "claude",
			ProviderChain:  []string{"claude", "codex"},
			State:          StateDone,
			ExitCode:       0,
			StartedAt:      time.Now().Add(-30 * time.Second),
			Duration:       25 * time.Second,
			Cost:           &adapter.CostInfo{USD: 0.05, InputTokens: 100, OutputTokens: 200},
			Lines: []OutputLine{
				{Kitchen: "claude", Type: LineText, Text: "The auth middleware validates JWT tokens."},
			},
			Collapsed: false,
			Conversation: &conversation.Conversation{
				ID:     "conv-1",
				Prompt: "explain auth",
				Status: conversation.StatusDone,
				Segments: []conversation.ProviderSegment{
					{ID: "conv-1-seg-1", Provider: "claude", Status: conversation.SegmentDone},
				},
			},
		},
		{
			ID:        "b2",
			Prompt:    "fix bug",
			Kitchen:   "opencode",
			State:     StateFailed,
			ExitCode:  1,
			StartedAt: time.Now().Add(-10 * time.Second),
			Duration:  8 * time.Second,
		},
		{
			// Active block — should NOT be persisted.
			ID:        "b3",
			Prompt:    "still running",
			Kitchen:   "gemini",
			State:     StateStreaming,
			StartedAt: time.Now(),
		},
	}

	err := SaveSession("test-session", blocks)
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// Verify file exists.
	dir := filepath.Join(tmpDir, "sessions")
	if _, err := os.Stat(filepath.Join(dir, "test-session.json")); os.IsNotExist(err) {
		t.Fatal("session file should exist")
	}

	// Load and verify.
	loaded, err := LoadSession("test-session")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	if len(loaded) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(loaded))
	}

	// Check first block.
	if loaded[0].Prompt != "explain auth" {
		t.Errorf("block 0 prompt = %q", loaded[0].Prompt)
	}
	if loaded[0].State != StateDone {
		t.Errorf("block 0 state = %v, want StateDone", loaded[0].State)
	}
	if loaded[0].Cost == nil || loaded[0].Cost.USD != 0.05 {
		t.Error("block 0 cost not preserved")
	}
	if loaded[0].Conversation == nil || loaded[0].Conversation.ID != "conv-1" {
		t.Error("block 0 conversation not preserved")
	}
	if len(loaded[0].ProviderChain) != 2 || loaded[0].ProviderChain[1] != "codex" {
		t.Error("block 0 provider chain not preserved")
	}
	if len(loaded[0].Lines) != 1 {
		t.Error("block 0 lines not preserved")
	}

	// Check second block.
	if loaded[1].State != StateFailed {
		t.Errorf("block 1 state = %v, want StateFailed", loaded[1].State)
	}

	if loaded[2].State != StateStreaming {
		t.Errorf("block 2 state = %v, want StateStreaming", loaded[2].State)
	}

	dir = filepath.Join(tmpDir, "sessions")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat(dir): %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("sessions dir perms = %#o", got)
	}
	fileInfo, err := os.Stat(filepath.Join(dir, "test-session.json"))
	if err != nil {
		t.Fatalf("Stat(file): %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("session file perms = %#o", got)
	}
}

func TestLoadSession_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	origBase := SessionsBaseDir
	SessionsBaseDir = tmpDir
	t.Cleanup(func() { SessionsBaseDir = origBase })

	_, err := LoadSession("nonexistent")
	if err == nil {
		t.Error("expected error loading nonexistent session")
	}
}
