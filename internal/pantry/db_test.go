package pantry

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpen(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	total, err := db.Ledger().Total()
	if err != nil {
		t.Fatalf("Total: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0, got %d", total)
	}
}

func TestOpen_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = db1.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	_ = db2.Close()
}

func TestLedger_InsertAndStats(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ledger := db.Ledger()

	entries := []LedgerEntry{
		{Timestamp: time.Now().UTC().Format(time.RFC3339), TaskHash: "h1", Kitchen: "claude", Outcome: "success", DurationSec: 2.0},
		{Timestamp: time.Now().UTC().Format(time.RFC3339), TaskHash: "h2", Kitchen: "opencode", Outcome: "success", DurationSec: 5.0},
		{Timestamp: time.Now().UTC().Format(time.RFC3339), TaskHash: "h3", Kitchen: "opencode", Outcome: "failure", DurationSec: 3.0, ExitCode: 1},
		{Timestamp: time.Now().UTC().Format(time.RFC3339), TaskHash: "h4", Kitchen: "gemini", Outcome: "success", DurationSec: 1.0},
	}
	for _, e := range entries {
		if _, err := ledger.Insert(e); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	total, err := ledger.Total()
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Errorf("expected 4, got %d", total)
	}

	stats, err := ledger.Stats()
	if err != nil {
		t.Fatal(err)
	}

	statsMap := make(map[string]KitchenStats)
	for _, s := range stats {
		statsMap[s.Kitchen] = s
	}

	if oc := statsMap["opencode"]; oc.Dispatches != 2 || oc.Successes != 1 {
		t.Errorf("opencode: dispatches=%d successes=%d, want 2/1", oc.Dispatches, oc.Successes)
	}
	if cl := statsMap["claude"]; cl.SuccessRate != 100.0 {
		t.Errorf("claude success rate: %.1f, want 100", cl.SuccessRate)
	}
}

func TestLedger_InsertReturnsID(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	id1, err := db.Ledger().Insert(LedgerEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339), TaskHash: "h1", Kitchen: "claude", Outcome: "success",
	})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := db.Ledger().Insert(LedgerEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339), TaskHash: "h2", Kitchen: "claude", Outcome: "success",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id2 <= id1 {
		t.Errorf("expected auto-incrementing IDs: %d <= %d", id2, id1)
	}
}

func TestRouting_RecordAndQuery(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	routing := db.Routing()

	// Record outcomes
	for range 5 {
		if err := routing.RecordOutcome("think", "", "claude", true, 2.0); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		if err := routing.RecordOutcome("think", "", "opencode", true, 5.0); err != nil {
			t.Fatal(err)
		}
	}
	if err := routing.RecordOutcome("think", "", "opencode", false, 8.0); err != nil {
		t.Fatal(err)
	}

	// Query best kitchen
	best, rate, err := routing.BestKitchen("think", "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if best != "claude" {
		t.Errorf("expected claude as best kitchen, got %q", best)
	}
	if rate != 100.0 {
		t.Errorf("expected 100%% rate for claude, got %.1f", rate)
	}
}

func TestRouting_BestKitchen_InsufficientData(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.Routing().RecordOutcome("code", "", "opencode", true, 1.0); err != nil {
		t.Fatal(err)
	}

	// Only 1 data point, require 5
	best, _, err := db.Routing().BestKitchen("code", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if best != "" {
		t.Errorf("expected empty string with insufficient data, got %q", best)
	}
}

func TestQuotas_IncrementAndQuery(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	quotas := db.Quotas()

	if err := quotas.Increment("claude", 2.5, false); err != nil {
		t.Fatal(err)
	}
	if err := quotas.Increment("claude", 3.0, false); err != nil {
		t.Fatal(err)
	}
	if err := quotas.Increment("claude", 1.0, true); err != nil {
		t.Fatal(err)
	}

	count, err := quotas.DailyDispatches("claude")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 daily dispatches, got %d", count)
	}
}

func TestQuotas_DifferentKitchens(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.Quotas().Increment("claude", 1.0, false); err != nil {
		t.Fatal(err)
	}
	if err := db.Quotas().Increment("opencode", 2.0, false); err != nil {
		t.Fatal(err)
	}

	claude, _ := db.Quotas().DailyDispatches("claude")
	opencode, _ := db.Quotas().DailyDispatches("opencode")

	if claude != 1 || opencode != 1 {
		t.Errorf("expected 1/1, got claude=%d opencode=%d", claude, opencode)
	}
}

func TestQuotas_NoPriorUsage(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	count, err := db.Quotas().DailyDispatches("never-used")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 for unused kitchen, got %d", count)
	}
}
