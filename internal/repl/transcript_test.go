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

package repl

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTranscriptWriter_PassThrough(t *testing.T) {
	t.Parallel()

	var inner bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "test.log")

	tw := NewTranscriptWriter(&inner, logPath)
	defer func() { _ = tw.Close() }()

	input := []byte("hello world\n")
	n, err := tw.Write(input)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(input) {
		t.Errorf("Write returned n=%d, want %d", n, len(input))
	}
	if got := inner.String(); got != "hello world\n" {
		t.Errorf("inner writer = %q, want %q", got, "hello world\n")
	}
}

func TestTranscriptWriter_ANSIStripped_InLog(t *testing.T) {
	t.Parallel()

	var inner bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "test.log")

	tw := NewTranscriptWriter(&inner, logPath)

	input := []byte("\x1b[32mhello\x1b[0m world\n")
	if _, err := tw.Write(input); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Inner writer receives raw bytes unchanged.
	if got := inner.Bytes(); !bytes.Equal(got, input) {
		t.Errorf("inner = %q, want %q", got, input)
	}

	// Log file receives stripped bytes.
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	want := "hello world\n"
	if got := string(logData); got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestTranscriptWriter_OSCSequenceStripped(t *testing.T) {
	t.Parallel()

	var inner bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "test.log")

	tw := NewTranscriptWriter(&inner, logPath)

	// OSC terminated by BEL.
	input := []byte("\x1b]0;window title\x07some text\n")
	if _, err := tw.Write(input); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	want := "some text\n"
	if got := string(logData); got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestTranscriptWriter_OSCTerminatedByST(t *testing.T) {
	t.Parallel()

	var inner bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "test.log")

	tw := NewTranscriptWriter(&inner, logPath)

	// OSC terminated by ST (ESC \).
	input := []byte("\x1b]0;title\x1b\\text\n")
	if _, err := tw.Write(input); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	want := "text\n"
	if got := string(logData); got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestTranscriptWriter_CursorSequences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "erase line",
			input: "before\x1b[Kafter\n",
			want:  "beforeafter\n",
		},
		{
			name:  "cursor home",
			input: "\x1b[Htext\n",
			want:  "text\n",
		},
		{
			name:  "cursor save restore",
			input: "\x1b7saved\x1b8end\n",
			want:  "savedend\n",
		},
		{
			name:  "SGR reset",
			input: "\x1b[0mclean\n",
			want:  "clean\n",
		},
		{
			name:  "complex SGR",
			input: "\x1b[1;32mBold green\x1b[0m\n",
			want:  "Bold green\n",
		},
		{
			name:  "plain text passthrough",
			input: "no escapes here\n",
			want:  "no escapes here\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var inner bytes.Buffer
			logPath := filepath.Join(t.TempDir(), "test.log")

			tw := NewTranscriptWriter(&inner, logPath)
			if _, err := tw.Write([]byte(tt.input)); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if err := tw.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			logData, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("reading log: %v", err)
			}
			if got := string(logData); got != tt.want {
				t.Errorf("log = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTranscriptWriter_GracefulDegradation_BadPath(t *testing.T) {
	t.Parallel()

	var inner bytes.Buffer
	// Use an invalid path that cannot be opened.
	tw := NewTranscriptWriter(&inner, "/nonexistent/dir/test.log")
	defer func() { _ = tw.Close() }()

	// Write should still succeed — bytes go to inner writer.
	input := []byte("data\n")
	n, err := tw.Write(input)
	if err != nil {
		t.Fatalf("Write should not fail on degraded writer: %v", err)
	}
	if n != len(input) {
		t.Errorf("n = %d, want %d", n, len(input))
	}
	if got := inner.String(); got != "data\n" {
		t.Errorf("inner = %q, want %q", got, "data\n")
	}
}

func TestTranscriptWriter_MultipleWrites(t *testing.T) {
	t.Parallel()

	var inner bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "test.log")

	tw := NewTranscriptWriter(&inner, logPath)

	writes := []string{
		"\x1b[31mred\x1b[0m",
		" plain ",
		"\x1b[1mbold\x1b[0m\n",
	}
	for _, w := range writes {
		if _, err := tw.Write([]byte(w)); err != nil {
			t.Fatalf("Write %q: %v", w, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	want := "red plain bold\n"
	if got := string(logData); got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}
