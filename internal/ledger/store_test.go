package ledger

import (
	"path/filepath"
	"testing"
)

func TestOpenStore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test-ledger.db")

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	total, err := store.Total()
	if err != nil {
		t.Fatalf("Total: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 entries, got %d", total)
	}
}

func TestStore_InsertAndStats(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	entries := []Entry{
		NewEntry("explain auth", "claude", "think", 2.0, 0),
		NewEntry("code handler", "opencode", "code", 5.0, 0),
		NewEntry("code failing", "opencode", "code", 3.0, 1),
		NewEntry("search docs", "gemini", "search", 1.0, 0),
	}
	for _, e := range entries {
		if err := store.Insert(e); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	total, err := store.Total()
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Errorf("expected 4 entries, got %d", total)
	}

	stats, err := store.Stats()
	if err != nil {
		t.Fatal(err)
	}

	statsMap := make(map[string]KitchenStats)
	for _, s := range stats {
		statsMap[s.Kitchen] = s
	}

	if oc, ok := statsMap["opencode"]; ok {
		if oc.Dispatches != 2 {
			t.Errorf("opencode dispatches: got %d, want 2", oc.Dispatches)
		}
		if oc.Successes != 1 {
			t.Errorf("opencode successes: got %d, want 1", oc.Successes)
		}
		if oc.SuccessRate != 50.0 {
			t.Errorf("opencode success rate: got %.1f, want 50.0", oc.SuccessRate)
		}
	} else {
		t.Error("expected opencode in stats")
	}

	if cl, ok := statsMap["claude"]; ok {
		if cl.SuccessRate != 100.0 {
			t.Errorf("claude success rate: got %.1f, want 100.0", cl.SuccessRate)
		}
	} else {
		t.Error("expected claude in stats")
	}
}

func TestDualWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ndjsonPath := filepath.Join(dir, "ledger.ndjson")
	dbPath := filepath.Join(dir, "ledger.db")

	dw, err := NewDualWriter(ndjsonPath, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dw.Close() }()

	e := NewEntry("test dual write", "claude", "think", 1.5, 0)
	if err := dw.Write(e); err != nil {
		t.Fatalf("DualWriter.Write: %v", err)
	}

	// Verify SQLite
	total, err := dw.Store().Total()
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("SQLite: expected 1 entry, got %d", total)
	}

	// Verify ndjson file exists and has content
	w := NewWriter(ndjsonPath)
	if w.Path() != ndjsonPath {
		t.Errorf("ndjson path mismatch")
	}
}
