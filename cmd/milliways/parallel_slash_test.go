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

// newTestLoop builds a chatLoop wired to in-memory writers for test inspection.
// The client is nil — tests that require RPC must wire their own.
func newTestLoop(out, errw *bytes.Buffer) *chatLoop {
	return &chatLoop{
		client: nil,
		out:    out,
		errw:   errw,
	}
}

// TestParallelSlash_EmptyPromptPrintsUsage verifies that /parallel with no
// prompt (and no --providers) prints a usage line.
func TestParallelSlash_EmptyPromptPrintsUsage(t *testing.T) {
	t.Parallel()

	var out, errw bytes.Buffer
	l := newTestLoop(&out, &errw)

	l.handleSlash("parallel")

	combined := out.String() + errw.String()
	if !strings.Contains(combined, "usage") && !strings.Contains(combined, "Usage") {
		t.Errorf("expected usage message for /parallel with no args; got stdout=%q stderr=%q", out.String(), errw.String())
	}
}

// TestParallelSlash_ProvidersFlagParsed verifies that --providers is parsed
// and the prompt is extracted correctly.
func TestParallelSlash_ProvidersFlagParsed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		args          string
		wantProviders []string
		wantPrompt    string
	}{
		{
			name:          "providers before prompt",
			args:          "--providers claude,codex review my code",
			wantProviders: []string{"claude", "codex"},
			wantPrompt:    "review my code",
		},
		{
			name:          "single provider",
			args:          "--providers _echo hello world",
			wantProviders: []string{"_echo"},
			wantPrompt:    "hello world",
		},
		{
			name:          "no providers flag",
			args:          "just a prompt",
			wantProviders: nil,
			wantPrompt:    "just a prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			providers, prompt := parseParallelArgs(tt.args)
			if tt.wantPrompt != prompt {
				t.Errorf("parseParallelArgs(%q) prompt = %q, want %q", tt.args, prompt, tt.wantPrompt)
			}
			if len(tt.wantProviders) == 0 && len(providers) != 0 {
				t.Errorf("parseParallelArgs(%q) providers = %v, want empty", tt.args, providers)
			}
			for i, want := range tt.wantProviders {
				if i >= len(providers) {
					t.Errorf("parseParallelArgs(%q) missing provider[%d]=%q", tt.args, i, want)
					continue
				}
				if providers[i] != want {
					t.Errorf("parseParallelArgs(%q) providers[%d] = %q, want %q", tt.args, i, providers[i], want)
				}
			}
		})
	}
}

// TestParallelSlash_HelpIsInHelp verifies that /parallel appears in the
// help output.
func TestParallelSlash_HelpIsInHelp(t *testing.T) {
	t.Parallel()

	var out, errw bytes.Buffer
	l := newTestLoop(&out, &errw)
	l.printHelp()

	if !strings.Contains(out.String(), "/parallel") {
		t.Errorf("printHelp does not mention /parallel; got:\n%s", out.String())
	}
}
