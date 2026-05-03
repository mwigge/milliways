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

package runners

import (
	"slices"
	"testing"
)

func TestBuildCodexCmdArgs_DefaultsInjectSandboxAndApproval(t *testing.T) {
	t.Parallel()

	args := buildCodexCmdArgs("do the thing", "/tmp/proj", nil)
	if !containsCodexPair(args, "--sandbox", "workspace-write") {
		t.Errorf("missing --sandbox workspace-write default in %v", args)
	}
	if !containsCodexPair(args, "--ask-for-approval", "never") {
		t.Errorf("missing --ask-for-approval never default in %v", args)
	}
	idxExec := slices.Index(args, "exec")
	if idxExec < 0 {
		t.Fatalf("missing exec subcommand: %v", args)
	}
	if idxSandbox := slices.Index(args, "--sandbox"); idxSandbox < 0 || idxSandbox > idxExec {
		t.Errorf("--sandbox should be a root flag before exec: %v", args)
	}
	if idxApproval := slices.Index(args, "--ask-for-approval"); idxApproval < 0 || idxApproval > idxExec {
		t.Errorf("--ask-for-approval should be a root flag before exec: %v", args)
	}
	// Prompt must come after the -- sentinel.
	idx := slices.Index(args, "--")
	if idx < 0 || idx >= len(args)-1 {
		t.Errorf("-- sentinel not before prompt: %v", args)
	}
	if got := args[len(args)-1]; got != "do the thing" {
		t.Errorf("last arg = %q, want %q (prompt)", got, "do the thing")
	}
}

func TestBuildCodexCmdArgs_RespectsExtraOverride(t *testing.T) {
	t.Parallel()

	args := buildCodexCmdArgs("p", "/tmp/proj", []string{"--sandbox", "read-only"})
	if containsCodexPair(args, "--sandbox", "workspace-write") {
		t.Errorf("default --sandbox workspace-write injected despite override: %v", args)
	}
	if !containsCodexPair(args, "--sandbox", "read-only") {
		t.Errorf("user --sandbox read-only not present: %v", args)
	}
	if !containsCodexPair(args, "--ask-for-approval", "never") {
		t.Errorf("default --ask-for-approval never should still be injected: %v", args)
	}
}

func TestCodexLineLooksProxyBlocked(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
		want bool
	}{
		{"zscaler html", `<html><head><title>Internet Security by Zscaler</title></head>`, true},
		{"403 forbidden", `unexpected status 403 forbidden from upstream`, true},
		{"307 temporary redirect", `307 temporary redirect to login.zscaler.net`, true},
		{"backend connect failure", `failed to connect to chatgpt.com/backend-api/codex`, true},
		{"benign line", `connecting to api...`, false},
		{"empty", ``, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := codexLineLooksProxyBlocked(c.line); got != c.want {
				t.Errorf("codexLineLooksProxyBlocked(%q) = %v, want %v", c.line, got, c.want)
			}
		})
	}
}

func TestCodexLineSignalsSessionLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
		want bool
	}{
		{"max_turns event", `{"type":"max_turns"}`, true},
		{"context_length_exceeded event", `{"type":"context_length_exceeded"}`, true},
		{"error with limit msg", `{"type":"error","message":"context window limit reached"}`, true},
		{"benign assistant", `{"type":"assistant","message":"hi"}`, false},
		{"non-json", `not json`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := codexLineSignalsSessionLimit(c.line); got != c.want {
				t.Errorf("codexLineSignalsSessionLimit(%q) = %v, want %v", c.line, got, c.want)
			}
		})
	}
}

func containsCodexPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
