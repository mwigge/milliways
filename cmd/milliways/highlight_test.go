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
	"strings"
	"testing"
)

// TestCodeHighlighter_PlainTextPassthrough verifies that content without
// any code fences is written to the underlying writer immediately and
// without buffering. Zero latency for non-code content is a hard requirement.
func TestCodeHighlighter_PlainTextPassthrough(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	h := newCodeHighlighter(&out)

	inputs := []string{
		"Hello, world!\n",
		"This is a normal line.\n",
		"Another line.\n",
	}
	for _, s := range inputs {
		n, err := h.Write([]byte(s))
		if err != nil {
			t.Fatalf("Write(%q) returned error: %v", s, err)
		}
		if n != len(s) {
			t.Errorf("Write(%q) = %d bytes written, want %d", s, n, len(s))
		}
	}

	got := out.String()
	want := "Hello, world!\nThis is a normal line.\nAnother line.\n"
	if got != want {
		t.Errorf("plain text output = %q, want %q", got, want)
	}
}

// TestCodeHighlighter_CompleteGoFenceProducesANSI verifies that a complete
// ```go ... ``` block is syntax-highlighted and the output contains at least
// one ANSI escape code (ESC = 0x1b).
func TestCodeHighlighter_CompleteGoFenceProducesANSI(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	h := newCodeHighlighter(&out)

	input := "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n"
	_, err := h.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "\x1b") {
		t.Errorf("expected ANSI escape codes in highlighted output, got: %q", got)
	}
	// The code content must survive highlighting (at minimum the identifier
	// "main" must appear somewhere in the output).
	if !strings.Contains(got, "main") {
		t.Errorf("highlighted output does not contain source token %q; got: %q", "main", got)
	}
}

// TestCodeHighlighter_TextBeforeAndAfterFence verifies that plain text
// before and after a code block is passed through correctly alongside
// the highlighted block.
func TestCodeHighlighter_TextBeforeAndAfterFence(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	h := newCodeHighlighter(&out)

	input := "Here is a snippet:\n```python\nprint('hi')\n```\nThat was the snippet.\n"
	_, err := h.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Here is a snippet:") {
		t.Errorf("output missing pre-fence text; got: %q", got)
	}
	if !strings.Contains(got, "That was the snippet.") {
		t.Errorf("output missing post-fence text; got: %q", got)
	}
	if !strings.Contains(got, "\x1b") {
		t.Errorf("output missing ANSI codes for highlighted block; got: %q", got)
	}
}

// TestCodeHighlighter_FenceWithNoLangFallsBackToPlainText verifies that a
// fence with no language tag does not crash and produces output containing
// the source code.
func TestCodeHighlighter_FenceWithNoLangFallsBackToPlainText(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	h := newCodeHighlighter(&out)

	input := "```\nsome plain code\n```\n"
	_, err := h.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "some plain code") {
		t.Errorf("output does not contain code content; got: %q", got)
	}
}

// TestCodeHighlighter_UnclosedFenceFlushedAsPlain verifies that an open
// fence (no closing ```) is written out as plain text when Flush() is called.
// This covers streaming truncation where the model output ends mid-fence.
func TestCodeHighlighter_UnclosedFenceFlushedAsPlain(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	h := newCodeHighlighter(&out)

	// Write an opening fence but no closing fence.
	input := "```go\nfunc init() {}\n"
	_, err := h.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	// Nothing should have been written to out yet (still buffering).
	// Flushing must emit the buffered content as plain text.
	if err := h.Flush(); err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "func init()") {
		t.Errorf("Flush() did not emit buffered content; got: %q", got)
	}
}

// TestCodeHighlighter_PartialLineNotWrittenUntilNewline verifies that a
// partial line (no trailing newline) is held in the pending buffer and not
// prematurely flushed to the underlying writer.
func TestCodeHighlighter_PartialLineNotWrittenUntilNewline(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	h := newCodeHighlighter(&out)

	// Write a partial line — no newline.
	_, _ = h.Write([]byte("partial"))

	if out.Len() != 0 {
		t.Errorf("partial line was written to out before newline; got %q", out.String())
	}

	// Complete the line.
	_, _ = h.Write([]byte(" line\n"))

	got := out.String()
	if !strings.Contains(got, "partial line") {
		t.Errorf("completed line not in output; got %q", got)
	}
}

// TestCodeHighlighter_MultipleBlocksInSingleWrite verifies that a single
// Write call containing multiple code blocks (and plain text between them)
// is processed correctly end-to-end.
func TestCodeHighlighter_MultipleBlocksInSingleWrite(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	h := newCodeHighlighter(&out)

	input := "First block:\n```go\nvar x int\n```\nSecond block:\n```python\nx = 1\n```\nDone.\n"
	_, err := h.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "First block:") {
		t.Errorf("missing pre-first-block text; got: %q", got)
	}
	if !strings.Contains(got, "Second block:") {
		t.Errorf("missing inter-block text; got: %q", got)
	}
	if !strings.Contains(got, "Done.") {
		t.Errorf("missing post-last-block text; got: %q", got)
	}
	// Both blocks should have been highlighted.
	if count := strings.Count(got, "\x1b"); count < 2 {
		t.Errorf("expected at least 2 ANSI escape sequences for 2 blocks, got %d; output: %q", count, got)
	}
}

// TestCodeHighlighter_WriteReturnsInputLength verifies that Write always
// reports consuming the full input slice length (streaming callers depend
// on this — returning fewer bytes causes an io.ErrShortWrite).
func TestCodeHighlighter_WriteReturnsInputLength(t *testing.T) {
	t.Parallel()

	inputs := []struct {
		name  string
		input string
	}{
		{"plain", "hello world\n"},
		{"fence open", "```go\n"},
		{"fence content", "x := 1\n"},
		{"fence close", "```\n"},
		{"empty", ""},
	}

	var out bytes.Buffer
	h := newCodeHighlighter(&out)
	for _, tc := range inputs {
		n, err := h.Write([]byte(tc.input))
		if err != nil {
			t.Errorf("Write(%q) error = %v", tc.name, err)
		}
		if n != len(tc.input) {
			t.Errorf("Write(%q) = %d, want %d", tc.name, n, len(tc.input))
		}
	}
}

func TestLinkifyURLs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOSC8 bool
		wantURL  string
	}{
		{
			name:     "bare https URL is wrapped",
			input:    "See https://github.com/mwigge/milliways for details.",
			wantOSC8: true,
			wantURL:  "https://github.com/mwigge/milliways",
		},
		{
			name:     "http URL is wrapped",
			input:    "Endpoint: http://localhost:8080/v1",
			wantOSC8: true,
			wantURL:  "http://localhost:8080/v1",
		},
		{
			name:     "plain text without URL is unchanged",
			input:    "No URLs here, just words.",
			wantOSC8: false,
		},
		{
			name:     "text with existing ANSI codes is not linkified",
			input:    "\x1b[32mhttps://example.com\x1b[0m",
			wantOSC8: false,
		},
		{
			name:     "multiple URLs on same line",
			input:    "See https://one.example.com and https://two.example.com",
			wantOSC8: true,
			wantURL:  "https://one.example.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := linkifyURLs(tc.input)
			hasOSC8 := strings.Contains(out, "\x1b]8;;")
			if hasOSC8 != tc.wantOSC8 {
				t.Errorf("OSC8 present=%v want=%v\noutput: %q", hasOSC8, tc.wantOSC8, out)
			}
			if tc.wantURL != "" && !strings.Contains(out, tc.wantURL) {
				t.Errorf("URL %q not found in output: %q", tc.wantURL, out)
			}
			// Original URL text must still be present (visible to user)
			if tc.wantOSC8 && tc.wantURL != "" && !strings.Contains(out, tc.wantURL+"\x1b]8;;\x1b\\") {
				t.Errorf("URL text not visible after OSC8 sequence: %q", out)
			}
		})
	}
}

func TestHighlighterLinkifiesPlainTextURLs(t *testing.T) {
	var out bytes.Buffer
	h := newCodeHighlighter(&out)
	_, _ = h.Write([]byte("Check https://github.com/mwigge/milliways/releases\n"))
	_ = h.Flush()

	result := out.String()
	if !strings.Contains(result, "\x1b]8;;https://github.com/mwigge/milliways/releases") {
		t.Errorf("expected OSC8 hyperlink in output, got: %q", result)
	}
}

func TestHighlighterDoesNotLinkifyInsideFence(t *testing.T) {
	// URLs inside code fences are already ANSI-highlighted — linkifyURLs must not
	// re-process them (the ANSI guard prevents double-wrapping).
	var out bytes.Buffer
	h := newCodeHighlighter(&out)
	_, _ = h.Write([]byte("```go\nclient.Get(\"https://example.com\")\n```\n"))
	_ = h.Flush()

	result := out.String()
	// Must have ANSI from syntax highlighting
	if !strings.Contains(result, "\x1b[") {
		t.Errorf("expected ANSI syntax highlighting in code fence output")
	}
	// Should NOT have an OSC8 sequence wrapping the URL inside the code block
	// (the ANSI guard in linkifyURLs prevents this)
	if strings.Contains(result, "\x1b]8;;https://example.com") {
		t.Errorf("OSC8 link should not appear inside syntax-highlighted code fence")
	}
}
