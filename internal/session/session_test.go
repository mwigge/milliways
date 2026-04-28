// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestToolCallJSONDurationMilliseconds(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(ToolCall{Name: "Read", Duration: 45 * time.Millisecond})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(encoded) == "" || !json.Valid(encoded) {
		t.Fatalf("invalid json: %s", encoded)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded["duration_ms"].(float64) != 45 {
		t.Fatalf("duration_ms = %v, want 45", decoded["duration_ms"])
	}
	var roundTrip ToolCall
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("round trip error = %v", err)
	}
	if roundTrip.Duration != 45*time.Millisecond {
		t.Fatalf("roundTrip.Duration = %s, want 45ms", roundTrip.Duration)
	}
}

func TestFileStoreSaveLoadAndList(t *testing.T) {
	t.Parallel()

	store := NewFileStore(filepath.Join(t.TempDir(), "sessions"))

	now := time.Date(2026, time.April, 20, 10, 30, 0, 0, time.UTC)
	first := Session{
		ID:        "session-1",
		CreatedAt: now,
		UpdatedAt: now,
		Model:     "minimax",
		Messages:  []Message{{Role: RoleUser, Content: "hello"}},
		Tools:     []ToolCall{{Name: "Read", Duration: 45 * time.Millisecond}},
	}
	second := Session{
		ID:        "session-2",
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(2 * time.Minute),
		Model:     "minimax",
		Messages:  []Message{{Role: RoleAssistant, Content: "ignored"}, {Role: RoleUser, Content: "world"}},
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
	if len(loaded.Tools) != 1 || loaded.Tools[0].Duration != 45*time.Millisecond {
		t.Fatalf("loaded.Tools = %#v", loaded.Tools)
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
	if summaries[0].Preview != "world" {
		t.Fatalf("preview = %q, want world", summaries[0].Preview)
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

func TestFileStoreSaveBlocksReadOnlyModePath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	privateDir := filepath.Join(homeDir, "personal")
	t.Setenv("MILLIWAYS_PRIVATE_ROOTS", privateDir)
	t.Setenv("MILLIWAYS_COMPANY_ROOTS", filepath.Join(homeDir, "work"))

	sessionsDir := filepath.Join(privateDir, "sessions")
	store := NewFileStore(sessionsDir)
	err := store.Save(Session{ID: "session-1", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	if err == nil {
		t.Fatal("Save() error = nil, want error")
	}
	if _, statErr := os.Stat(filepath.Join(sessionsDir, "session-1.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("session file stat error = %v, want not exist", statErr)
	}
}
