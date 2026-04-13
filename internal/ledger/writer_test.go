package ledger

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewEntry(t *testing.T) {
	e := NewEntry("explain auth flow", "claude", "think", 2.5, 0)

	if e.Kitchen != "claude" {
		t.Errorf("expected kitchen 'claude', got %q", e.Kitchen)
	}
	if e.Station != "think" {
		t.Errorf("expected station 'think', got %q", e.Station)
	}
	if e.Outcome != "success" {
		t.Errorf("expected outcome 'success', got %q", e.Outcome)
	}
	if !strings.HasPrefix(e.TaskHash, "sha256:") {
		t.Errorf("expected task_hash starting with sha256:, got %q", e.TaskHash)
	}
	if e.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestNewEntry_Failure(t *testing.T) {
	e := NewEntry("code something", "opencode", "code", 5.0, 1)

	if e.Outcome != "failure" {
		t.Errorf("expected outcome 'failure' for exit code 1, got %q", e.Outcome)
	}
}

func TestWriter_Write(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "ledger.ndjson")

	w := NewWriter(path)

	e1 := NewEntry("task one", "claude", "think", 1.0, 0)
	e2 := NewEntry("task two", "opencode", "code", 3.0, 0)

	if err := w.Write(e1); err != nil {
		t.Fatalf("write e1: %v", err)
	}
	if err := w.Write(e2); err != nil {
		t.Fatalf("write e2: %v", err)
	}

	// Read back and verify ndjson format
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	var entries []Entry
	for scanner.Scan() {
		var e Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal line: %v", err)
		}
		entries = append(entries, e)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Kitchen != "claude" {
		t.Errorf("entry 0 kitchen: got %q, want 'claude'", entries[0].Kitchen)
	}
	if entries[1].Kitchen != "opencode" {
		t.Errorf("entry 1 kitchen: got %q, want 'opencode'", entries[1].Kitchen)
	}
}

func TestWriter_Path(t *testing.T) {
	w := NewWriter("/tmp/test-ledger.ndjson")
	if w.Path() != "/tmp/test-ledger.ndjson" {
		t.Errorf("expected path '/tmp/test-ledger.ndjson', got %q", w.Path())
	}
}

func TestHashPrompt_Deterministic(t *testing.T) {
	e1 := NewEntry("same prompt", "claude", "think", 1.0, 0)
	e2 := NewEntry("same prompt", "claude", "think", 2.0, 0)

	if e1.TaskHash != e2.TaskHash {
		t.Errorf("expected same hash for same prompt, got %q vs %q", e1.TaskHash, e2.TaskHash)
	}
}

func TestHashPrompt_DifferentPrompts(t *testing.T) {
	e1 := NewEntry("prompt one", "claude", "think", 1.0, 0)
	e2 := NewEntry("prompt two", "claude", "think", 1.0, 0)

	if e1.TaskHash == e2.TaskHash {
		t.Error("expected different hashes for different prompts")
	}
}
