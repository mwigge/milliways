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

package pantry

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestClassifyStability(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		churn90d int
		want     string
	}{
		{"zero churn", 0, "stable"},
		{"low churn", 2, "stable"},
		{"boundary stable", 2, "stable"},
		{"active", 5, "active"},
		{"active upper", 15, "active"},
		{"volatile", 16, "volatile"},
		{"high volatile", 100, "volatile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyStability(tt.churn90d); got != tt.want {
				t.Errorf("classifyStability(%d) = %q, want %q", tt.churn90d, got, tt.want)
			}
		})
	}
}

func TestGitGraphStore_IsHotspot_NotFound(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	fs, err := db.GitGraph().IsHotspot("/some/repo", "nonexistent.go")
	if err != nil {
		t.Fatal(err)
	}
	if fs != nil {
		t.Error("expected nil for nonexistent file")
	}
}

func TestGitGraphStore_UpsertAndQuery(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	gg := db.GitGraph()

	// Manually insert via SQL (simulating a Sync result)
	_, err := db.conn.Exec(`
		INSERT INTO mw_gitgraph (repo, file_path, churn_30d, churn_90d, authors_30d, last_author, stability, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, "/test/repo", "store.py", 12, 45, 3, "morgan@example.com", "volatile")
	if err != nil {
		t.Fatal(err)
	}

	fs, err := gg.IsHotspot("/test/repo", "store.py")
	if err != nil {
		t.Fatal(err)
	}
	if fs == nil {
		t.Fatal("expected file stats")
	}
	if fs.Stability != "volatile" {
		t.Errorf("stability: got %q, want 'volatile'", fs.Stability)
	}
	if fs.Churn30d != 12 {
		t.Errorf("churn30d: got %d, want 12", fs.Churn30d)
	}
	if fs.Churn90d != 45 {
		t.Errorf("churn90d: got %d, want 45", fs.Churn90d)
	}
	if fs.Authors30d != 3 {
		t.Errorf("authors30d: got %d, want 3", fs.Authors30d)
	}
}

func TestGitGraphStore_Sync_TempRepo(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Create a temporary git repo with known commits.
	repoDir := t.TempDir()
	for _, cmd := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test"},
	} {
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Dir = repoDir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}

	// Create a file and commit it.
	testFile := filepath.Join(repoDir, "hello.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range [][]string{
		{"git", "add", "hello.go"},
		{"git", "commit", "-m", "initial"},
	} {
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Dir = repoDir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}

	count, err := db.GitGraph().Sync(repoDir)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least 1 file synced, got 0")
	}

	fs, err := db.GitGraph().IsHotspot(repoDir, "hello.go")
	if err != nil {
		t.Fatal(err)
	}
	if fs == nil {
		t.Fatal("expected hello.go in gitgraph after sync")
	}
	if fs.Stability != "stable" {
		t.Errorf("expected stability 'stable' for single commit, got %q", fs.Stability)
	}
}
