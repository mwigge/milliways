package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScheduleSessionSave(t *testing.T) {
	cmd := scheduleSessionSave()
	if cmd == nil {
		t.Fatal("scheduleSessionSave() returned nil")
	}
}

func TestSessionSaveMsgHandler(t *testing.T) {
	// Use a temp dir to avoid polluting real config.
	tmpDir := t.TempDir()
	origBase := SessionsBaseDir
	SessionsBaseDir = tmpDir
	t.Cleanup(func() { SessionsBaseDir = origBase })

	// Create a model with some blocks.
	m := Model{
		blocks: []Block{
			{
				ID:        "b1",
				Prompt:    "test prompt",
				Kitchen:   "claude",
				State:     StateDone,
				StartedAt: time.Now().Add(-10 * time.Second),
				Duration:  10 * time.Second,
				ExitCode:  0,
			},
			{
				ID:        "b2",
				Prompt:    "another test",
				Kitchen:   "gemini",
				State:     StateStreaming,
				StartedAt: time.Now(),
			},
		},
	}

	// Create the session save message.
	msg := sessionSaveMsg(time.Now())

	// Update the model with the message.
	updatedModel, cmds := m.Update(msg)

	// Verify the model is returned.
	if updatedModel.blocks == nil {
		t.Fatal("blocks should not be nil after update")
	}

	// Verify the command reschedules.
	if len(cmds) == 0 {
		t.Error("expected scheduleSessionSave() to be called again")
	}

	// Verify session was saved to disk.
	sessionFile := filepath.Join(tmpDir, "sessions", "last.json")
	if _, err := os.Stat(sessionFile); os.IsNotExist(err) {
		t.Fatal("session file should exist after sessionSaveMsg")
	}

	// Verify the saved content.
	loaded, err := LoadSession("last")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 blocks in saved session, got %d", len(loaded))
	}
	if loaded[0].Prompt != "test prompt" {
		t.Errorf("block 0 prompt = %q, want %q", loaded[0].Prompt, "test prompt")
	}
	if loaded[1].Kitchen != "gemini" {
		t.Errorf("block 1 kitchen = %q, want %q", loaded[1].Kitchen, "gemini")
	}
}

func TestSessionSaveMsgHandler_PreservesAllBlockStates(t *testing.T) {
	tmpDir := t.TempDir()
	origBase := SessionsBaseDir
	SessionsBaseDir = tmpDir
	t.Cleanup(func() { SessionsBaseDir = origBase })

	// Create blocks with various states.
	m := Model{
		blocks: []Block{
			{ID: "done", State: StateDone, ExitCode: 0},
			{ID: "failed", State: StateFailed, ExitCode: 1},
			{ID: "streaming", State: StateStreaming},
			{ID: "routing", State: StateRouting},
			{ID: "cancelled", State: StateCancelled},
		},
	}

	msg := sessionSaveMsg(time.Now())
	_, _ = m.Update(msg)

	loaded, err := LoadSession("last")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	stateMap := make(map[string]DispatchState)
	for _, b := range loaded {
		stateMap[b.ID] = b.State
	}

	tests := []struct {
		id        string
		wantState DispatchState
	}{
		{"done", StateDone},
		{"failed", StateFailed},
		{"streaming", StateStreaming},
		{"routing", StateRouting},
		{"cancelled", StateCancelled},
	}

	for _, tc := range tests {
		if got, ok := stateMap[tc.id]; !ok {
			t.Errorf("block %s not found in loaded session", tc.id)
		} else if got != tc.wantState {
			t.Errorf("block %s state = %v, want %v", tc.id, got, tc.wantState)
		}
	}
}

func TestSessionSaveMsgHandler_EmptyBlocks(t *testing.T) {
	tmpDir := t.TempDir()
	origBase := SessionsBaseDir
	SessionsBaseDir = tmpDir
	t.Cleanup(func() { SessionsBaseDir = origBase })

	m := Model{
		blocks: []Block{}, // Empty session.
	}

	msg := sessionSaveMsg(time.Now())
	_, cmds := m.Update(msg)

	// Should still save (empty session is valid).
	sessionFile := filepath.Join(tmpDir, "sessions", "last.json")
	if _, err := os.Stat(sessionFile); os.IsNotExist(err) {
		t.Fatal("empty session should still be saved")
	}

	// Should still reschedule.
	if len(cmds) == 0 {
		t.Error("expected scheduleSessionSave() to be called again for empty session")
	}
}

func TestSessionSaveMsgHandler_NoPanicOnError(t *testing.T) {
	// This test verifies that errors don't panic.
	// We can't easily trigger an error without mocking filesystem,
	// but we can verify the handler completes without panic.

	m := Model{
		blocks: []Block{
			{ID: "b1", State: StateDone},
		},
	}

	msg := sessionSaveMsg(time.Now())

	// This should not panic even if save fails.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Update panicked: %v", r)
		}
	}()

	_, _ = m.Update(msg)
}
