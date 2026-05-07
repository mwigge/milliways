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

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSwitchableCompleterCompletesSlashCommand(t *testing.T) {
	t.Parallel()

	c := &switchableCompleter{}
	c.set(buildCompleter("minimax"))

	suffixes, replace := c.Complete("/sw", len("/sw"))
	if replace != 3 {
		t.Fatalf("replace = %d, want 3", replace)
	}
	if !slices.Contains(suffixes, "itch") {
		t.Fatalf("suffixes = %#v, want itch", suffixes)
	}
}

func TestSwitchableCompleterCompletesShellPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o600); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	line := "!cat " + filepath.Join(dir, "sam")
	suffixes, replace := shellPathComplete(line)
	if replace == 0 {
		t.Fatal("replace = 0, want path prefix length")
	}
	if !slices.Contains(suffixes, "ple.txt") {
		t.Fatalf("suffixes = %#v, want ple.txt", suffixes)
	}
}

func TestCommonPrefix(t *testing.T) {
	t.Parallel()

	if got := commonPrefix([]string{"itch", "ap"}); got != "" {
		t.Fatalf("commonPrefix mismatch = %q, want empty", got)
	}
	if got := commonPrefix([]string{"pletion", "pact"}); got != "p" {
		t.Fatalf("commonPrefix shared = %q, want p", got)
	}
	if got := commonPrefix([]string{"single"}); got != "single" {
		t.Fatalf("commonPrefix single = %q", got)
	}
	if got := commonPrefix(nil); got != "" {
		t.Fatalf("commonPrefix nil = %q", got)
	}
}

func TestLineReaderSavesCappedHistory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "history")
	r := &chatLineReader{historyFile: path}
	for i := 0; i < 1005; i++ {
		r.history = append(r.history, strings.Repeat("x", 1)+string(rune('a'+i%26)))
	}
	if err := r.saveHistory(); err != nil {
		t.Fatalf("saveHistory: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1000 {
		t.Fatalf("history lines = %d, want 1000", len(lines))
	}
}
