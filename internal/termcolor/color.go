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
	"strings"
)

// Enabled reports whether terminal color should be emitted for the current
// process environment.
func Enabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return SupportsANSI(os.Getenv("TERM"), os.Getenv("COLORTERM"), os.Getenv("TERM_PROGRAM"))
}

// SupportsANSI reports whether the provided terminal environment is known to
// support ANSI color escapes.
func SupportsANSI(term, colorTerm, termProgram string) bool {
	term = strings.ToLower(strings.TrimSpace(term))
	colorTerm = strings.ToLower(strings.TrimSpace(colorTerm))
	termProgram = strings.ToLower(strings.TrimSpace(termProgram))
	if colorTerm == "truecolor" || colorTerm == "24bit" {
		return true
	}
	if termProgram == "wezterm" || termProgram == "iterm.app" || termProgram == "apple_terminal" {
		return true
	}
	if term == "" || term == "dumb" || term == "unknown" {
		return false
	}
	for _, marker := range []string{"color", "ansi", "xterm", "screen", "tmux", "rxvt", "kitty", "wezterm", "linux"} {
		if strings.Contains(term, marker) {
			return true
		}
	}
	return false
}
