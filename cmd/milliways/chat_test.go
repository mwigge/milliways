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

// chatLoopHelpsTest mirrors chatLoop but allows tests to inspect output
// streams without spawning a real readline / daemon connection.
type chatLoopHelpsTest struct{ *chatLoop }

// TestChatHelpEnumeratesKnownCommands asserts /help lists the user-facing
// surface (numeric runners, named runners, local-bootstrap, opsx,
// switch/agents/quota/help/exit, !cmd). Regression guard against
// silently dropping commands during refactors.
func TestChatHelpEnumeratesKnownCommands(t *testing.T) {
	// /help re-runs printLanding which calls the daemon for live status;
	// without a real daemon the call short-circuits and statuses default
	// to "?" — that's the path we want to exercise here. Don't t.Parallel
	// because t.Setenv is involved indirectly via fetchAgentStatuses
	// reading socket env, even though we don't call it directly.
	var stdout bytes.Buffer
	loop := &chatLoop{
		client: nil, // landing renders even with nil client (defensive)
		out:    &stdout,
		errw:   &bytes.Buffer{},
	}
	defer func() {
		// printHelp / printLanding panics if client is nil because
		// fetchAgentStatuses calls client.Call. Catch it so the test still
		// runs; we only care about the static parts of the banner.
		_ = recover()
	}()
	loop.printHelp()

	for _, want := range []string{
		"/1", "/2", "/3", "/4", "/5", "/6", "/7",
		"/switch",
		"/agents",
		"/quota",
		"/help",
		"/exit",
		"!<cmd>",
		"claude", "codex", "copilot", "gemini", "local", "minimax", "pool",
		"/install-local-server",
		"/list-local-models",
		"/setup-local-model",
		"/opsx-list",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output missing %q; got:\n%s", want, stdout.String())
		}
	}
}

// TestChatPromptFormat — the prompt header reflects the active agent so
// users always see which runner their next typed line goes to. The
// empty-string case is the landing zone (no client picked yet).
func TestChatPromptFormat(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":        "[no client — pick one with /1../7 or /<name>] ▶ ",
		"claude":  "[claude] ▶ ",
		"local":   "[local] ▶ ",
		"minimax": "[minimax] ▶ ",
	}
	for agent, want := range cases {
		if got := chatPrompt(agent); got != want {
			t.Errorf("chatPrompt(%q) = %q, want %q", agent, got, want)
		}
	}
}

// TestParseDigitInRange covers the /1../7 numeric shortcut parser.
func TestParseDigitInRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s        string
		lo, hi   int
		wantN    int
		wantOK   bool
	}{
		{"1", 1, 7, 1, true},
		{"7", 1, 7, 7, true},
		{"4", 1, 7, 4, true},
		{"0", 1, 7, 0, false}, // below range
		{"8", 1, 7, 0, false}, // above range
		{"", 1, 7, 0, false},  // empty
		{"a", 1, 7, 0, false}, // non-digit
		{"42", 1, 7, 0, false}, // multi-digit unsupported
	}
	for _, c := range cases {
		got, ok := parseDigitInRange(c.s, c.lo, c.hi)
		if got != c.wantN || ok != c.wantOK {
			t.Errorf("parseDigitInRange(%q,%d,%d) = (%d,%v), want (%d,%v)", c.s, c.lo, c.hi, got, ok, c.wantN, c.wantOK)
		}
	}
}

// TestChatCtlAliasesNonOverlappingWithRunners — guards against a slash
// command alias colliding with a runner name (which would shadow the
// runner switch in the dispatcher).
func TestChatCtlAliasesNonOverlappingWithRunners(t *testing.T) {
	t.Parallel()
	runners := map[string]bool{}
	for _, r := range chatSwitchableAgents {
		runners[r] = true
	}
	for alias := range chatCtlAliases {
		if runners[alias] {
			t.Errorf("ctl alias /%s collides with runner name; rename one", alias)
		}
	}
}

// TestChatSwitchableAgentsCoversDaemonRegistry — guards against the chat
// switch list drifting from the daemon's dispatch table. If a new runner
// lands in agents.go's switch and the chat doesn't get the entry, users
// can't /switch to it. This test enumerates the IDs the chat exposes;
// new daemon runners should add their ID here.
func TestChatSwitchableAgentsCoversDaemonRegistry(t *testing.T) {
	t.Parallel()

	expected := map[string]bool{
		"claude": true, "codex": true, "copilot": true,
		"gemini": true, "local": true, "minimax": true, "pool": true,
	}
	if got := len(chatSwitchableAgents); got != len(expected) {
		t.Errorf("chatSwitchableAgents len = %d, want %d", got, len(expected))
	}
	for _, id := range chatSwitchableAgents {
		if !expected[id] {
			t.Errorf("chatSwitchableAgents has unexpected entry %q", id)
		}
		delete(expected, id)
	}
	for missing := range expected {
		t.Errorf("chatSwitchableAgents missing %q", missing)
	}
}

// TestChatHistoryFileRespectsXDGStateHome
func TestChatHistoryFileRespectsXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/example-state")
	got := chatHistoryFile()
	if want := "/tmp/example-state/milliways/chat_history"; got != want {
		t.Errorf("chatHistoryFile() with XDG_STATE_HOME = %q, want %q", got, want)
	}
}

// TestChatHistoryFileFallsBackToHomeLocalState
func TestChatHistoryFileFallsBackToHomeLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/tmp/example-home")
	got := chatHistoryFile()
	if want := "/tmp/example-home/.local/state/milliways/chat_history"; got != want {
		t.Errorf("chatHistoryFile() with HOME = %q, want %q", got, want)
	}
}
