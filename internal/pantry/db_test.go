package pantry

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
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

func TestLedger_LastWithConversationFields(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	_, err := db.Ledger().Insert(LedgerEntry{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		TaskHash:       "h1",
		TaskType:       "review",
		Kitchen:        "claude",
		Outcome:        "failure",
		ConversationID: "conv-1",
		SegmentID:      "conv-1-seg-1",
		SegmentIndex:   1,
		EndReason:      "provider exhausted",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := db.Ledger().Last()
	if err != nil {
		t.Fatal(err)
	}
	if got.ConversationID != "conv-1" {
		t.Fatalf("ConversationID = %q", got.ConversationID)
	}
	if got.SegmentID != "conv-1-seg-1" {
		t.Fatalf("SegmentID = %q", got.SegmentID)
	}
	if got.SegmentIndex != 1 {
		t.Fatalf("SegmentIndex = %d", got.SegmentIndex)
	}
	if got.EndReason != "provider exhausted" {
		t.Fatalf("EndReason = %q", got.EndReason)
	}
}

func TestLedger_FailoverChains(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	entries := []LedgerEntry{
		{Timestamp: now, TaskHash: "h1", Kitchen: "claude", Outcome: "failure", ConversationID: "conv-1", SegmentID: "seg-1", SegmentIndex: 1, EndReason: "provider exhausted"},
		{Timestamp: now, TaskHash: "h1", Kitchen: "codex", Outcome: "success", ConversationID: "conv-1", SegmentID: "seg-2", SegmentIndex: 2, EndReason: "completed"},
		{Timestamp: now, TaskHash: "h2", Kitchen: "gemini", Outcome: "success", ConversationID: "conv-2", SegmentID: "seg-1", SegmentIndex: 1, EndReason: "completed"},
	}
	for _, entry := range entries {
		if _, err := db.Ledger().Insert(entry); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	chains, err := db.Ledger().FailoverChains(10)
	if err != nil {
		t.Fatalf("FailoverChains: %v", err)
	}
	if len(chains) != 1 {
		t.Fatalf("chains = %#v", chains)
	}
	if chains[0].ConversationID != "conv-1" {
		t.Fatalf("ConversationID = %q", chains[0].ConversationID)
	}
	if chains[0].Providers != "claude -> codex" {
		t.Fatalf("Providers = %q", chains[0].Providers)
	}
	if chains[0].Failovers != 1 {
		t.Fatalf("Failovers = %d", chains[0].Failovers)
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

func TestDBPing(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestRuntimeEvents_InsertAndList(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	_, err := db.RuntimeEvents().Insert(RuntimeEventRecord{
		ConversationID: "conv-1",
		BlockID:        "b1",
		SegmentID:      "seg-1",
		Kind:           "failover",
		Provider:       "claude",
		Text:           "provider exhausted",
		Fields:         map[string]string{"status": "exhausted"},
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := db.RuntimeEvents().ListByConversation("conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != "failover" {
		t.Fatalf("Kind = %q", events[0].Kind)
	}
	if events[0].Fields["status"] != "exhausted" {
		t.Fatalf("Fields = %#v", events[0].Fields)
	}
}

func TestRuntimeEvents_ReconstructConversation(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	now := time.Now().UTC()

	records := []RuntimeEventRecord{
		{ConversationID: "conv-1", BlockID: "b1", SegmentID: "seg-1", Kind: "segment_start", Provider: "claude", Text: "initial route", At: now.Format(time.RFC3339)},
		{ConversationID: "conv-1", BlockID: "b1", SegmentID: "seg-1", Kind: "provider_output", Provider: "claude", Text: "working", At: now.Add(time.Second).Format(time.RFC3339), Fields: map[string]string{"event_type": "text"}},
		{ConversationID: "conv-1", BlockID: "b1", SegmentID: "seg-1", Kind: "provider_output", Provider: "claude", At: now.Add(2 * time.Second).Format(time.RFC3339), Fields: map[string]string{"event_type": "code_block", "code": "fmt.Println(\"hi\")", "language": "go"}},
		{ConversationID: "conv-1", BlockID: "b1", SegmentID: "seg-1", Kind: "segment_end", Provider: "claude", Text: "provider exhausted", At: now.Add(3 * time.Second).Format(time.RFC3339), Fields: map[string]string{"status": string(conversation.SegmentExhausted), "reason": "provider exhausted"}},
		{ConversationID: "conv-1", BlockID: "b1", SegmentID: "seg-1", Kind: "failover", Provider: "claude", Text: "provider exhausted", At: now.Add(4 * time.Second).Format(time.RFC3339)},
		{ConversationID: "conv-1", BlockID: "b1", SegmentID: "seg-2", Kind: "segment_start", Provider: "codex", Text: "fallback", At: now.Add(5 * time.Second).Format(time.RFC3339)},
		{ConversationID: "conv-1", BlockID: "b1", SegmentID: "seg-2", Kind: "provider_output", Provider: "codex", Text: "continued", At: now.Add(6 * time.Second).Format(time.RFC3339), Fields: map[string]string{"event_type": "text"}},
		{ConversationID: "conv-1", BlockID: "b1", SegmentID: "seg-2", Kind: "segment_end", Provider: "codex", Text: "completed", At: now.Add(7 * time.Second).Format(time.RFC3339), Fields: map[string]string{"status": string(conversation.SegmentDone), "reason": "completed"}},
	}
	for _, rec := range records {
		if _, err := db.RuntimeEvents().Insert(rec); err != nil {
			t.Fatalf("Insert(%s): %v", rec.Kind, err)
		}
	}

	conv, events, err := db.RuntimeEvents().ReconstructConversation("conv-1", "b1", "fix continuity", 0)
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if conv.Status != conversation.StatusDone {
		t.Fatalf("Status = %q", conv.Status)
	}
	if len(conv.Segments) != 2 {
		t.Fatalf("Segments = %d", len(conv.Segments))
	}
	if conv.Segments[0].Status != conversation.SegmentExhausted {
		t.Fatalf("segment[0].Status = %q", conv.Segments[0].Status)
	}
	if conv.Segments[1].Provider != "codex" || conv.Segments[1].Status != conversation.SegmentDone {
		t.Fatalf("segment[1] = %#v", conv.Segments[1])
	}
	if len(events) != len(records) {
		t.Fatalf("events = %d", len(events))
	}
	if len(conv.Transcript) < 4 {
		t.Fatalf("Transcript = %#v", conv.Transcript)
	}
}

func TestRuntimeEvents_ReconstructConversation_NoRows(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	_, _, err := db.RuntimeEvents().ReconstructConversation("missing", "b1", "prompt", 0)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckpoints_InsertAndLatest(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ckpt := conversation.ConversationCheckpoint{
		ID:              "ckpt-1",
		ConversationID:  "conv-1",
		BlockID:         "b1",
		Reason:          "provider exhausted",
		SegmentID:       "seg-1",
		SegmentProvider: "claude",
		TranscriptTurns: 3,
		WorkingMemory:   conversation.MemoryState{WorkingSummary: "continue"},
		TakenAt:         time.Now().UTC(),
	}
	if _, err := db.Checkpoints().Insert(ckpt); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := db.Checkpoints().LatestByConversation("conv-1")
	if err != nil {
		t.Fatalf("LatestByConversation: %v", err)
	}
	if got.ID != "ckpt-1" || got.SegmentProvider != "claude" {
		t.Fatalf("checkpoint = %#v", got)
	}
}

func TestRuntimeEvents_ReconstructConversationFromCheckpoint(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	now := time.Now().UTC()

	ckpt := conversation.ConversationCheckpoint{
		ID:              "ckpt-1",
		ConversationID:  "conv-1",
		BlockID:         "b1",
		Reason:          "provider exhausted",
		Status:          conversation.StatusActive,
		TranscriptTurns: 2,
		Transcript: []conversation.Turn{
			{Role: conversation.RoleUser, Provider: "user", Text: "fix continuity", At: now.Add(-time.Minute)},
			{Role: conversation.RoleAssistant, Provider: "claude", Text: "working", At: now.Add(-50 * time.Second)},
		},
		Segments: []conversation.ProviderSegment{
			{ID: "seg-1", Provider: "claude", Status: conversation.SegmentExhausted, StartedAt: now.Add(-time.Minute), EndedAt: ptrTime(now.Add(-40 * time.Second)), EndReason: "provider exhausted"},
		},
		WorkingMemory: conversation.MemoryState{WorkingSummary: "continue", NextAction: "resume in codex"},
		TakenAt:       now,
	}
	if _, err := db.Checkpoints().Insert(ckpt); err != nil {
		t.Fatalf("Insert checkpoint: %v", err)
	}
	records := []RuntimeEventRecord{
		{ConversationID: "conv-1", BlockID: "b1", SegmentID: "seg-2", Kind: "segment_start", Provider: "codex", Text: "fallback", At: now.Add(time.Second).Format(time.RFC3339)},
		{ConversationID: "conv-1", BlockID: "b1", SegmentID: "seg-2", Kind: "provider_output", Provider: "codex", Text: "continued", At: now.Add(2 * time.Second).Format(time.RFC3339), Fields: map[string]string{"event_type": "text"}},
		{ConversationID: "conv-1", BlockID: "b1", SegmentID: "seg-2", Kind: "segment_end", Provider: "codex", Text: "completed", At: now.Add(3 * time.Second).Format(time.RFC3339), Fields: map[string]string{"status": string(conversation.SegmentDone), "reason": "completed"}},
	}
	for _, rec := range records {
		if _, err := db.RuntimeEvents().Insert(rec); err != nil {
			t.Fatalf("Insert(%s): %v", rec.Kind, err)
		}
	}
	gotCkpt, err := db.Checkpoints().LatestByConversation("conv-1")
	if err != nil {
		t.Fatalf("LatestByConversation: %v", err)
	}
	conv, events, err := db.RuntimeEvents().ReconstructConversationFromCheckpoint(gotCkpt, 0)
	if err != nil {
		t.Fatalf("ReconstructConversationFromCheckpoint: %v", err)
	}
	if len(conv.Segments) != 2 {
		t.Fatalf("Segments = %d", len(conv.Segments))
	}
	if conv.Segments[1].Provider != "codex" || conv.Segments[1].Status != conversation.SegmentDone {
		t.Fatalf("segment[1] = %#v", conv.Segments[1])
	}
	if len(events) != len(records) {
		t.Fatalf("events = %d", len(events))
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestMemoryItems_InsertListAndInvalidate(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	stale := time.Now().Add(-time.Hour)
	if _, err := db.MemoryItems().Insert(conversation.MemoryCandidate{
		SourceKind: "spec",
		MemoryType: conversation.MemoryProcedural,
		Text:       "stay in one block",
		Scope:      "project",
		Confidence: 1.0,
	}, "conv-1"); err != nil {
		t.Fatalf("Insert active: %v", err)
	}
	if _, err := db.MemoryItems().Insert(conversation.MemoryCandidate{
		SourceKind: "accepted_fact",
		MemoryType: conversation.MemorySemantic,
		Text:       "old fact",
		Scope:      "project",
		Confidence: 0.9,
		FreshUntil: &stale,
	}, "conv-1"); err != nil {
		t.Fatalf("Insert stale: %v", err)
	}
	items, err := db.MemoryItems().ListActiveByType(conversation.MemoryProcedural, "project")
	if err != nil {
		t.Fatalf("ListActiveByType: %v", err)
	}
	if len(items) != 1 || items[0] != "stay in one block" {
		t.Fatalf("items = %#v", items)
	}
	invalidated, err := db.MemoryItems().InvalidateExpired(time.Now())
	if err != nil {
		t.Fatalf("InvalidateExpired: %v", err)
	}
	if invalidated == 0 {
		t.Fatal("expected expired memory to be invalidated")
	}
	semantic, err := db.MemoryItems().ListActiveByType(conversation.MemorySemantic, "project")
	if err != nil {
		t.Fatalf("List semantic: %v", err)
	}
	if len(semantic) != 0 {
		t.Fatalf("semantic items = %#v", semantic)
	}
}
