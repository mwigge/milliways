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
// surface (slash commands + ! escape). Regression guard against silently
// dropping commands during refactors.
func TestChatHelpEnumeratesKnownCommands(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	loop := &chatLoop{
		out:  &stdout,
		errw: &bytes.Buffer{},
	}
	loop.printHelp()

	for _, want := range []string{
		"/<runner>",
		"/switch",
		"/agents",
		"/quota",
		"/help",
		"/exit",
		"!<command>",
		"claude", "codex", "copilot", "gemini", "local", "minimax", "pool",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output missing %q; got:\n%s", want, stdout.String())
		}
	}
}

// TestChatPromptFormat — the prompt header reflects the active agent so
// users always see which runner their next typed line goes to.
func TestChatPromptFormat(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"claude":   "[claude] ▶ ",
		"local":    "[local] ▶ ",
		"minimax":  "[minimax] ▶ ",
	}
	for agent, want := range cases {
		if got := chatPrompt(agent); got != want {
			t.Errorf("chatPrompt(%q) = %q, want %q", agent, got, want)
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
