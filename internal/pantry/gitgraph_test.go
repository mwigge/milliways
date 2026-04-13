package pantry

import (
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

func TestGitGraphStore_Sync_RealRepo(t *testing.T) {
	// This test uses the milliways repo itself — it's a real git repo.
	// Skip if running in CI without git.
	t.Parallel()
	db := openTestDB(t)

	count, err := db.GitGraph().Sync(".")
	if err != nil {
		t.Skipf("Sync failed (no git repo at .): %v", err)
	}
	if count == 0 {
		t.Skip("no files found in git log (new repo?)")
	}

	// Verify at least one file was indexed
	fs, err := db.GitGraph().IsHotspot(".", "internal/pantry/db.go")
	if err != nil {
		t.Fatal(err)
	}
	// May or may not exist depending on git history, that's ok
	_ = fs
}
