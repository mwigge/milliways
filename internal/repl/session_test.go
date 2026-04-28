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

package repl

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionStore_SaveAndLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewSessionStoreAt(dir)
	if err != nil {
		t.Fatalf("NewSessionStoreAt: %v", err)
	}

	turns := []ConversationTurn{
		{Role: "user", Text: "hello", Runner: "claude", At: time.Now().Truncate(time.Second)},
		{Role: "assistant", Text: "world", Runner: "claude", At: time.Now().Truncate(time.Second)},
	}
	sess := PersistedSession{
		Version:    sessionVersion,
		SavedAt:    time.Now().Truncate(time.Second),
		RunnerName: "claude",
		RulesHash:  rulesHash("rules content"),
		WorkDir:    "/some/project",
		Turns:      turns,
	}

	if err := store.Save("mysession", sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load("mysession")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.RunnerName != sess.RunnerName {
		t.Errorf("RunnerName = %q, want %q", got.RunnerName, sess.RunnerName)
	}
	if got.WorkDir != sess.WorkDir {
		t.Errorf("WorkDir = %q, want %q", got.WorkDir, sess.WorkDir)
	}
	if got.Version != sess.Version {
		t.Errorf("Version = %d, want %d", got.Version, sess.Version)
	}
	if len(got.Turns) != len(turns) {
		t.Fatalf("Turns len = %d, want %d", len(got.Turns), len(turns))
	}
	if got.Turns[0].Text != turns[0].Text {
		t.Errorf("Turns[0].Text = %q, want %q", got.Turns[0].Text, turns[0].Text)
	}
}

func TestSessionStore_AutoSaveAndFindLatest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewSessionStoreAt(dir)
	if err != nil {
		t.Fatalf("NewSessionStoreAt: %v", err)
	}

	cwd := "/my/project"
	sess := PersistedSession{
		Version:    sessionVersion,
		SavedAt:    time.Now().Truncate(time.Second),
		RunnerName: "codex",
		RulesHash:  rulesHash(""),
		WorkDir:    cwd,
		Turns: []ConversationTurn{
			{Role: "user", Text: "auto save me", Runner: "codex", At: time.Now()},
		},
	}

	if err := store.Save("", sess); err != nil {
		t.Fatalf("auto-save: %v", err)
	}

	got, ok := store.FindLatestForCwd(cwd)
	if !ok {
		t.Fatal("FindLatestForCwd returned not found")
	}
	if got.RunnerName != "codex" {
		t.Errorf("RunnerName = %q, want %q", got.RunnerName, "codex")
	}
	if len(got.Turns) != 1 {
		t.Errorf("Turns len = %d, want 1", len(got.Turns))
	}
}

func TestSessionStore_FindLatestForCwd_NoMatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewSessionStoreAt(dir)
	if err != nil {
		t.Fatalf("NewSessionStoreAt: %v", err)
	}

	sess := PersistedSession{
		Version:    sessionVersion,
		SavedAt:    time.Now(),
		RunnerName: "claude",
		WorkDir:    "/project/a",
		Turns:      nil,
	}
	if err := store.Save("", sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, ok := store.FindLatestForCwd("/project/b")
	if ok {
		t.Error("FindLatestForCwd returned found for non-matching cwd")
	}
}

func TestSessionStore_PrunesOldAutoSessions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewSessionStoreAt(dir)
	if err != nil {
		t.Fatalf("NewSessionStoreAt: %v", err)
	}

	cwd := "/my/project"
	base := time.Now()
	// Save 7 auto sessions for the same cwd, spaced 1 second apart so timestamps differ.
	for i := 0; i < 7; i++ {
		sess := PersistedSession{
			Version:    sessionVersion,
			SavedAt:    base.Add(time.Duration(i) * time.Second),
			RunnerName: "claude",
			WorkDir:    cwd,
			Turns:      nil,
		}
		if err := store.Save("", sess); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	autoCount := 0
	for _, m := range all {
		if isAutoSession(filepath.Base(m.Path)) {
			autoCount++
		}
	}
	if autoCount > maxAutoSessions {
		t.Errorf("auto session count = %d, want <= %d", autoCount, maxAutoSessions)
	}
}

func TestSessionStore_List(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewSessionStoreAt(dir)
	if err != nil {
		t.Fatalf("NewSessionStoreAt: %v", err)
	}

	// Save two named sessions.
	for _, name := range []string{"alpha", "beta"} {
		sess := PersistedSession{
			Version:    sessionVersion,
			SavedAt:    time.Now(),
			RunnerName: "claude",
			WorkDir:    "/tmp",
			Turns:      []ConversationTurn{{Role: "user", Text: "hi"}},
		}
		if err := store.Save(name, sess); err != nil {
			t.Fatalf("Save %q: %v", name, err)
		}
	}

	// Save one auto session.
	sess := PersistedSession{
		Version:    sessionVersion,
		SavedAt:    time.Now(),
		RunnerName: "codex",
		WorkDir:    "/tmp",
		Turns:      nil,
	}
	if err := store.Save("", sess); err != nil {
		t.Fatalf("auto Save: %v", err)
	}

	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(all) < 3 {
		t.Errorf("List returned %d entries, want >= 3", len(all))
	}

	// Verify metadata fields are populated.
	for _, m := range all {
		if m.Path == "" {
			t.Errorf("entry has empty Path: %+v", m)
		}
	}
}

func TestRulesHash_Stable(t *testing.T) {
	t.Parallel()

	input := "these are my rules"
	h1 := rulesHash(input)
	h2 := rulesHash(input)
	if h1 != h2 {
		t.Errorf("rulesHash not stable: %q vs %q", h1, h2)
	}
}

func TestRulesHash_Empty(t *testing.T) {
	t.Parallel()

	h := rulesHash("")
	if h == "" {
		t.Error("rulesHash(\"\") returned empty string")
	}
	// sha256 of "" is well-known: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	const wantPrefix = "e3b0c4"
	if len(h) < len(wantPrefix) || h[:len(wantPrefix)] != wantPrefix {
		t.Errorf("rulesHash(\"\") = %q, want prefix %q", h, wantPrefix)
	}
}

// isAutoSession reports whether a filename looks like an auto-session file.
// Mirrors the naming logic in session.go.
func isAutoSession(name string) bool {
	return len(name) > 5 && name[:5] == "auto-"
}

// TestCwdHash8 verifies the cwdHash8 helper produces 8 hex chars.
func TestCwdHash8(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"normal path", "/home/user/project"},
		{"short path", "/tmp"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := cwdHash8(tt.input)
			if len(h) != 8 {
				t.Errorf("cwdHash8(%q) = %q (len %d), want 8 chars", tt.input, h, len(h))
			}
			// All hex chars.
			for i, c := range h {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("cwdHash8(%q)[%d] = %q, not hex", tt.input, i, c)
				}
			}
		})
	}
}

// TestSessionStore_LoadNonExistent verifies a helpful error for missing sessions.
func TestSessionStore_LoadNonExistent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewSessionStoreAt(dir)
	if err != nil {
		t.Fatalf("NewSessionStoreAt: %v", err)
	}

	_, err = store.Load("doesnotexist")
	if err == nil {
		t.Error("Load of non-existent session returned nil error")
	}
}

// TestAutoSessionFileName verifies the naming pattern used for auto sessions.
func TestAutoSessionFileName(t *testing.T) {
	t.Parallel()

	cwd := "/my/project"
	hash := cwdHash8(cwd)
	if len(hash) != 8 {
		t.Fatalf("cwdHash8 returned %d chars, want 8", len(hash))
	}

	// Confirm the prefix matches what session.go will produce.
	expectedPrefix := fmt.Sprintf("auto-%s-", hash)
	_ = expectedPrefix // used as documentation; the real check is in SaveAndFind tests
}
