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

func TestWriteOSCTitle_NormalTerminal(t *testing.T) {
	var buf bytes.Buffer
	writeOSCTitle(&buf, "● claude", "milliways · claude · $0.0042 session")
	got := buf.String()

	// OSC 0 must set the tab title
	if !strings.Contains(got, "\033]0;● claude\007") {
		t.Errorf("missing OSC 0 tab sequence; got %q", got)
	}
	// OSC 2 must override the window title
	if !strings.Contains(got, "\033]2;milliways · claude · $0.0042 session\007") {
		t.Errorf("missing OSC 2 window sequence; got %q", got)
	}
	// No DCS passthrough in non-tmux context
	if strings.Contains(got, "\033Ptmux;") {
		t.Errorf("unexpected tmux DCS passthrough in non-tmux context; got %q", got)
	}
}

func TestWriteOSCTitle_TmuxPassthrough(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	var buf bytes.Buffer
	writeOSCTitle(&buf, "● codex", "milliways · codex")
	got := buf.String()

	// DCS passthrough must wrap both sequences
	if !strings.Contains(got, "\033Ptmux;") {
		t.Errorf("missing tmux DCS passthrough; got %q", got)
	}
	// The inner OSC 0 sequence must be present inside the DCS wrapper
	if !strings.Contains(got, "● codex") {
		t.Errorf("tab title missing from tmux output; got %q", got)
	}
	if !strings.Contains(got, "milliways · codex") {
		t.Errorf("window title missing from tmux output; got %q", got)
	}
}

func TestSanitiseOSC_StripsControlChars(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// BEL terminates the OSC sequence — must be stripped
		{"hello\007world", "helloworld"},
		// ESC can start a new sequence — must be stripped
		{"hello\033]2;injected\007world", "hello]2;injectedworld"},
		// CR and LF could break terminal display — must be stripped
		{"line1\r\nline2", "line1line2"},
		// Normal unicode must pass through intact
		{"● claude · sonnet-4-6", "● claude · sonnet-4-6"},
		// Cost string must pass through intact
		{"$0.0042 session · 1200→340 tok", "$0.0042 session · 1200→340 tok"},
	}
	for _, tc := range tests {
		got := sanitiseOSC(tc.input)
		if got != tc.want {
			t.Errorf("sanitiseOSC(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestWriteOSCTitle_SanitisesInputs(t *testing.T) {
	var buf bytes.Buffer
	// Inject a BEL that would terminate the OSC sequence early and an ESC
	// that would start a new sequence inside the window title string.
	writeOSCTitle(&buf, "tab\007early", "win\033]0;injected\007evil")
	got := buf.String()

	// BEL inside the tab argument must be gone so the OSC 0 sequence
	// is not terminated before the closing \007 we emit.
	if strings.Contains(got, "\007early") {
		t.Errorf("BEL in tab title not stripped; got %q", got)
	}
	// ESC inside the window argument must be gone so no new OSC sequence
	// can be opened inside our window title value. The word "injected"
	// may remain as harmless literal text — the injection is the ESC that
	// starts a new sequence, and that ESC must be absent.
	if strings.Contains(got, "\033]0;injected") {
		t.Errorf("ESC+OSC injection in window title not stripped; got %q", got)
	}
}
