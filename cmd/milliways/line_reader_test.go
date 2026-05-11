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
	"bytes"
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

func TestLineReaderCursorEditing(t *testing.T) {
	var out bytes.Buffer
	r := &chatLineReader{out: &out, prompt: "> ", buf: []rune("abcd"), cursor: 4}

	r.moveCursor(-2)
	r.insertRune('X')
	if got := string(r.buf); got != "abXcd" {
		t.Fatalf("insert at cursor = %q, want abXcd", got)
	}
	if r.cursor != 3 {
		t.Fatalf("cursor after insert = %d, want 3", r.cursor)
	}

	r.backspace()
	if got := string(r.buf); got != "abcd" {
		t.Fatalf("backspace at cursor = %q, want abcd", got)
	}
	if r.cursor != 2 {
		t.Fatalf("cursor after backspace = %d, want 2", r.cursor)
	}

	r.deleteAtCursor()
	if got := string(r.buf); got != "abd" {
		t.Fatalf("delete at cursor = %q, want abd", got)
	}
	if r.cursor != 2 {
		t.Fatalf("cursor after delete = %d, want 2", r.cursor)
	}
}

func TestLineReaderRedrawRestoresCursorPosition(t *testing.T) {
	var out bytes.Buffer
	r := &chatLineReader{out: &out, prompt: "> ", buf: []rune("abcd"), cursor: 2}

	r.redrawLocked()

	got := out.String()
	if !strings.Contains(got, "\rabcd") && !strings.Contains(got, "> abcd") {
		t.Fatalf("redraw missing buffer: %q", got)
	}
	if !strings.Contains(got, "\033[4C") {
		t.Fatalf("redraw should restore cursor column after repaint; got %q", got)
	}
}

func TestLineReaderRedrawClearsPreviousWrappedRows(t *testing.T) {
	var out bytes.Buffer
	oldTermWidth := lineReaderTermWidth
	lineReaderTermWidth = func() int { return 20 }
	t.Cleanup(func() { lineReaderTermWidth = oldTermWidth })

	r := &chatLineReader{
		out:    &out,
		prompt: "> ",
		buf:    []rune("this is a long prompt that wraps"),
		cursor: len([]rune("this is a long prompt that wraps")),
	}
	r.redrawLocked()
	out.Reset()

	r.buf = []rune("short")
	r.cursor = len(r.buf)
	r.redrawLocked()

	got := out.String()
	if !strings.Contains(got, "\033[1A") {
		t.Fatalf("redraw should move up to clear wrapped rows; got %q", got)
	}
	if strings.Count(got, "\033[2K") < 2 {
		t.Fatalf("redraw should clear every previous wrapped row; got %q", got)
	}
}

func TestLineReaderCursorPositionAtWrapBoundary(t *testing.T) {
	t.Parallel()
	row, col := cursorPosition(20, 20)
	if row != 0 || col != 20 {
		t.Fatalf("cursor at boundary = row %d col %d, want row 0 col 20", row, col)
	}
	row, col = cursorPosition(21, 20)
	if row != 1 || col != 1 {
		t.Fatalf("cursor after boundary = row %d col %d, want row 1 col 1", row, col)
	}
}

func TestLineReaderRedrawDoesNotMoveBelowExactWrappedLine(t *testing.T) {
	var out bytes.Buffer
	oldTermWidth := lineReaderTermWidth
	lineReaderTermWidth = func() int { return 20 }
	t.Cleanup(func() { lineReaderTermWidth = oldTermWidth })

	r := &chatLineReader{
		out:    &out,
		prompt: "> ",
		buf:    []rune(strings.Repeat("x", 18)),
		cursor: 18,
	}
	r.redrawLocked()

	got := out.String()
	if strings.Contains(got, "\033[1A") {
		t.Fatalf("exact-width redraw moved up from a phantom row: %q", got)
	}
	if !strings.Contains(got, "\033[20C") {
		t.Fatalf("exact-width redraw should park cursor at end of row, got %q", got)
	}
}

func TestLineReaderExternalOutputHidesAndRestoresPrompt(t *testing.T) {
	var out bytes.Buffer
	r := &chatLineReader{
		out:    &out,
		prompt: "> ",
		buf:    []rune("draft"),
		cursor: len("draft"),
		active: true,
	}
	r.redrawLocked()
	out.Reset()

	r.BeginExternalOutput()
	if !r.promptHidden {
		t.Fatal("BeginExternalOutput did not hide prompt")
	}
	if !strings.Contains(out.String(), "\033[2K") {
		t.Fatalf("BeginExternalOutput should clear active prompt, got %q", out.String())
	}

	out.Reset()
	r.EndExternalOutput()
	if r.promptHidden {
		t.Fatal("EndExternalOutput left prompt hidden")
	}
	if got := out.String(); !strings.Contains(got, "> draft") {
		t.Fatalf("EndExternalOutput should redraw prompt and buffer, got %q", got)
	}
}

func TestLineReaderRefreshDoesNotPaintInactivePrompt(t *testing.T) {
	var out bytes.Buffer
	r := &chatLineReader{
		out:    &out,
		prompt: "pool …thinking ▶ ",
		active: false,
	}

	r.Refresh()

	if got := out.String(); got != "" {
		t.Fatalf("inactive Refresh wrote prompt %q", got)
	}
}

func TestLineReaderSubmittedLineReturnsToColumnZero(t *testing.T) {
	var out bytes.Buffer
	r := &chatLineReader{
		out:    &out,
		prompt: "pool ▶ ",
	}

	r.writeSubmittedLineLocked("/switch claude")

	if got, want := out.String(), "pool ▶ /switch claude\r\n"; got != want {
		t.Fatalf("submitted line = %q, want %q", got, want)
	}
}
