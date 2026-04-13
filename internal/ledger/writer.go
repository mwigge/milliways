package ledger

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Entry represents one dispatch record in the ledger.
type Entry struct {
	Timestamp    string  `json:"ts"`
	TaskHash     string  `json:"task_hash"`
	TaskType     string  `json:"task_type"`
	Kitchen      string  `json:"kitchen"`
	Station      string  `json:"station"`
	File         string  `json:"file,omitempty"`
	DurationSec  float64 `json:"duration_s"`
	ExitCode     int     `json:"exit_code"`
	LinesAdded   int     `json:"lines_added"`
	LinesRemoved int     `json:"lines_removed"`
	CostEstUSD   float64 `json:"cost_est_usd"`
	Outcome      string  `json:"outcome"`
}

// Writer appends entries to the ndjson ledger file.
type Writer struct {
	path string
}

// NewWriter creates a ledger writer for the given file path.
func NewWriter(path string) *Writer {
	return &Writer{path: path}
}

// Write appends a single entry to the ledger file.
func (w *Writer) Write(e Entry) error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return fmt.Errorf("creating ledger directory: %w", err)
	}

	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening ledger file: %w", err)
	}
	defer func() { _ = f.Close() }()

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshalling entry: %w", err)
	}

	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing entry: %w", err)
	}

	return nil
}

// Path returns the ledger file path.
func (w *Writer) Path() string { return w.path }

// NewEntry creates a ledger entry with computed fields.
func NewEntry(prompt, kitchen, station string, durationSec float64, exitCode int) Entry {
	outcome := "success"
	if exitCode != 0 {
		outcome = "failure"
	}

	return Entry{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		TaskHash:    HashPrompt(prompt),
		Kitchen:     kitchen,
		Station:     station,
		DurationSec: durationSec,
		ExitCode:    exitCode,
		Outcome:     outcome,
	}
}

// HashPrompt returns a truncated SHA-256 hash of the prompt for deduplication.
func HashPrompt(prompt string) string {
	truncated := prompt
	if len(truncated) > 200 {
		truncated = truncated[:200]
	}
	h := sha256.Sum256([]byte(truncated))
	return fmt.Sprintf("sha256:%x", h[:8])
}
