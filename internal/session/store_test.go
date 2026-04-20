package session

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreSaveLoadAndList(t *testing.T) {
	t.Parallel()

	store := NewFileStore(filepath.Join(t.TempDir(), "sessions"))

	now := time.Date(2026, time.April, 20, 10, 30, 0, 0, time.UTC)
	first := Session{
		ID:        "session-1",
		CreatedAt: now,
		UpdatedAt: now,
		Model:     "minimax",
		Messages:  []Message{{Role: "user", Content: "hello"}},
	}
	second := Session{
		ID:        "session-2",
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(2 * time.Minute),
		Model:     "minimax",
		Messages:  []Message{{Role: "assistant", Content: "world"}},
	}

	if err := store.Save(first); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	if err := store.Save(second); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}

	loaded, err := store.Load("session-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ID != first.ID {
		t.Fatalf("loaded.ID = %q, want %q", loaded.ID, first.ID)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "hello" {
		t.Fatalf("loaded.Messages = %#v", loaded.Messages)
	}

	summaries, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("len(List()) = %d, want 2", len(summaries))
	}
	if summaries[0].ID != "session-2" {
		t.Fatalf("summaries[0].ID = %q, want session-2", summaries[0].ID)
	}
	if summaries[1].ID != "session-1" {
		t.Fatalf("summaries[1].ID = %q, want session-1", summaries[1].ID)
	}
}

func TestFileStoreLoadMissingSession(t *testing.T) {
	t.Parallel()

	store := NewFileStore(filepath.Join(t.TempDir(), "sessions"))

	_, err := store.Load("missing")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Load() error = %v, want ErrSessionNotFound", err)
	}
}
