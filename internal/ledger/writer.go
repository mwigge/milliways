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
	return Entry{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		TaskHash:    HashPrompt(prompt),
		Kitchen:     kitchen,
		Station:     station,
		DurationSec: durationSec,
		ExitCode:    exitCode,
		Outcome:     OutcomeFromExitCode(exitCode),
	}
}

// OutcomeFromExitCode returns "success" for exit code 0 and "failure" otherwise.
func OutcomeFromExitCode(exitCode int) string {
	if exitCode == 0 {
		return "success"
	}
	return "failure"
}

// HashPrompt returns a truncated SHA-256 hash of the prompt for deduplication.
func HashPrompt(prompt string) string {
	runes := []rune(prompt)
	if len(runes) > 200 {
		prompt = string(runes[:200])
	}
	h := sha256.Sum256([]byte(prompt))
	return fmt.Sprintf("sha256:%x", h[:8])
}
