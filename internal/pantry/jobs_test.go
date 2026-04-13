package pantry

import (
	"testing"
)

func TestListRecent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := db.Tickets()

	// Seed three tickets.
	if _, err := store.Create("claude", "task one", "async", 100, ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create("opencode", "task two", "detached", 101, ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create("gemini", "task three", "async", 102, ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	tests := []struct {
		name      string
		n         int
		wantCount int
	}{
		{"limit 2", 2, 2},
		{"limit 10 returns all", 10, 3},
		{"n=0 returns all", 0, 3},
		{"limit 1", 1, 1},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tickets, err := store.ListRecent(tc.n)
			if err != nil {
				t.Fatalf("ListRecent(%d): %v", tc.n, err)
			}
			if len(tickets) != tc.wantCount {
				t.Errorf("got %d tickets, want %d", len(tickets), tc.wantCount)
			}
		})
	}
}

func TestCountByStatus(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := db.Tickets()

	id1, _ := store.Create("claude", "a", "async", 1, "")
	_, _ = store.Create("opencode", "b", "async", 2, "")
	_, _ = store.Create("gemini", "c", "async", 3, "")

	// Mark one complete.
	_ = store.UpdateStatus(id1, "complete", 0, nil)

	counts, err := store.CountByStatus()
	if err != nil {
		t.Fatalf("CountByStatus: %v", err)
	}

	if counts["running"] != 2 {
		t.Errorf("running: got %d, want 2", counts["running"])
	}
	if counts["complete"] != 1 {
		t.Errorf("complete: got %d, want 1", counts["complete"])
	}
}
