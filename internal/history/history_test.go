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

package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendAgentHistory(t *testing.T) {
	tmp := t.TempDir()
	err := AppendAgentHistory(tmp, "alice", map[string]string{"msg": "hello"}, 0)
	if err != nil {
		t.Fatalf("AppendAgentHistory: %v", err)
	}
	fpath := filepath.Join(tmp, "history", "alice.ndjson")
	data, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read history file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty history file")
	}
}

func TestAppendAgentHistory_trims(t *testing.T) {
	tmp := t.TempDir()
	max := 5
	for i := 0; i < 20; i++ {
		if err := AppendAgentHistory(tmp, "bob", map[string]int{"i": i}, max); err != nil {
			t.Fatalf("AppendAgentHistory: %v", err)
		}
	}
	fpath := filepath.Join(tmp, "history", "bob.ndjson")
	data, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read history file: %v", err)
	}
	lines := 0
	for _, ch := range data {
		if ch == '\n' {
			lines++
		}
	}
	if lines != max {
		t.Errorf("expected %d lines after trim, got %d", max, lines)
	}
}

func TestReadAgentHistory(t *testing.T) {
	tmp := t.TempDir()
	payloads := []map[string]int{{"n": 1}, {"n": 2}, {"n": 3}}
	for _, p := range payloads {
		if err := AppendAgentHistory(tmp, "charlie", p, 0); err != nil {
			t.Fatalf("AppendAgentHistory: %v", err)
		}
	}

	entries, err := ReadAgentHistory(tmp, "charlie", 0)
	if err != nil {
		t.Fatalf("ReadAgentHistory: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Test limit.
	entries, err = ReadAgentHistory(tmp, "charlie", 2)
	if err != nil {
		t.Fatalf("ReadAgentHistory with limit: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries with limit, got %d", len(entries))
	}
}

func TestReadAgentHistory_nonexistent(t *testing.T) {
	tmp := t.TempDir()
	entries, err := ReadAgentHistory(tmp, "nobody", 0)
	if err != nil {
		t.Fatalf("ReadAgentHistory: %v", err)
	}
	if entries != nil {
		t.Fatalf("expected nil for nonexistent agent, got %v", entries)
	}
}

func TestTrimFileToLines(t *testing.T) {
	tmp := t.TempDir()
	fpath := filepath.Join(tmp, "trimtest.txt")

	// Write 10 lines.
	f, err := os.Create(fpath)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	for i := 0; i < 10; i++ {
		f.WriteString("line\n")
	}
	f.Close()

	err = trimFileToLines(fpath, 5)
	if err != nil {
		t.Fatalf("trimFileToLines: %v", err)
	}

	data, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := 0
	for _, ch := range data {
		if ch == '\n' {
			lines++
		}
	}
	if lines != 5 {
		t.Errorf("expected 5 lines, got %d", lines)
	}
}

func TestTrimFileToLines_underMax(t *testing.T) {
	tmp := t.TempDir()
	fpath := filepath.Join(tmp, "notrim.txt")

	f, err := os.Create(fpath)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	for i := 0; i < 3; i++ {
		f.WriteString("line\n")
	}
	f.Close()

	err = trimFileToLines(fpath, 5)
	if err != nil {
		t.Fatalf("trimFileToLines: %v", err)
	}

	data, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := 0
	for _, ch := range data {
		if ch == '\n' {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("expected 3 lines (no trim), got %d", lines)
	}
}