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

package termcolor

import (
	"os"
	"testing"
)

func TestSupportsANSI(t *testing.T) {
	tests := []struct {
		name        string
		term        string
		colorTerm   string
		termProgram string
		want        bool
	}{
		{name: "empty", want: false},
		{name: "dumb", term: "dumb", want: false},
		{name: "unknown", term: "unknown", want: false},
		{name: "plain vt100", term: "vt100", want: false},
		{name: "xterm", term: "xterm", want: true},
		{name: "256 color", term: "xterm-256color", want: true},
		{name: "linux console", term: "linux", want: true},
		{name: "truecolor", colorTerm: "truecolor", want: true},
		{name: "wezterm program", termProgram: "WezTerm", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SupportsANSI(tt.term, tt.colorTerm, tt.termProgram); got != tt.want {
				t.Fatalf("SupportsANSI(%q, %q, %q) = %v, want %v", tt.term, tt.colorTerm, tt.termProgram, got, tt.want)
			}
		})
	}
}

func TestEnabledRespectsEnvironment(t *testing.T) {
	oldNoColor, hadNoColor := os.LookupEnv("NO_COLOR")
	oldTerm, hadTerm := os.LookupEnv("TERM")
	oldColorTerm, hadColorTerm := os.LookupEnv("COLORTERM")
	oldTermProgram, hadTermProgram := os.LookupEnv("TERM_PROGRAM")
	t.Cleanup(func() {
		restoreEnv("NO_COLOR", oldNoColor, hadNoColor)
		restoreEnv("TERM", oldTerm, hadTerm)
		restoreEnv("COLORTERM", oldColorTerm, hadColorTerm)
		restoreEnv("TERM_PROGRAM", oldTermProgram, hadTermProgram)
	})

	_ = os.Unsetenv("NO_COLOR")
	_ = os.Unsetenv("COLORTERM")
	_ = os.Unsetenv("TERM_PROGRAM")
	_ = os.Setenv("TERM", "dumb")
	if Enabled() {
		t.Fatal("Enabled() with TERM=dumb = true, want false")
	}
	_ = os.Unsetenv("TERM")
	if Enabled() {
		t.Fatal("Enabled() with empty TERM = true, want false")
	}
	_ = os.Setenv("TERM", "xterm-256color")
	if !Enabled() {
		t.Fatal("Enabled() with xterm-256color = false, want true")
	}
	_ = os.Setenv("NO_COLOR", "1")
	if Enabled() {
		t.Fatal("Enabled() with NO_COLOR = true, want false")
	}
}

func restoreEnv(key, value string, ok bool) {
	if ok {
		_ = os.Setenv(key, value)
		return
	}
	_ = os.Unsetenv(key)
}
