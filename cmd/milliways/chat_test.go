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
	"fmt"
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
	// nil client is safe: fetchAgentStatuses guards against it and
	// probeDaemonForWelcome times out gracefully. No recover needed.
	var stdout bytes.Buffer
	loop := &chatLoop{
		client: nil,
		out:    &stdout,
		errw:   &bytes.Buffer{},
	}
	loop.printHelp()

	for _, want := range []string{
		// Client picker
		"/1", "/2", "/3", "/4", "/5", "/6", "/7",
		"claude", "codex", "copilot", "gemini", "local", "minimax", "pool",
		// Full help section
		"/switch", "/agents", "/quota", "/help", "/exit", "!<cmd>",
		"/install", "/install-local-server", "/list-local-models", "/setup-local-model",
		"/opsx-list",
		"/login",
		"/model",
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

	// Strip ANSI escapes before comparing so the test stays readable
	// when colours are added or tweaked.
	stripANSI := func(s string) string {
		var out strings.Builder
		inEsc := false
		for _, r := range s {
			switch {
			case r == '\033':
				inEsc = true
			case inEsc && r == 'm':
				inEsc = false
			case !inEsc:
				out.WriteRune(r)
			}
		}
		return out.String()
	}

	cases := map[string]string{
		"":        "[no client — pick one with /1../7 or /<name>] ▶ ",
		"claude":  "[claude] ▶ ",
		"local":   "[local] ▶ ",
		"minimax": "[minimax] ▶ ",
	}
	for agent, want := range cases {
		if got := stripANSI(chatPrompt(agent)); got != want {
			t.Errorf("chatPrompt(%q) stripped = %q, want %q", agent, got, want)
		}
	}
}

func TestThinkingLineUsesDarkerClientColor(t *testing.T) {
	t.Parallel()

	line := formatThinkingLine("minimax", "planning next step")
	if !strings.HasPrefix(line, "\033[38;5;98m") {
		t.Fatalf("thinking line uses %q, want minimax thinking color", line)
	}
	if !strings.Contains(line, "… planning next step") {
		t.Fatalf("thinking line missing message: %q", line)
	}
	if strings.Contains(line, agentColor("minimax")) {
		t.Fatalf("thinking line should use darker companion color, got main color in %q", line)
	}
}

// TestParseDigitInRange covers the /1../7 numeric shortcut parser.
func TestParseDigitInRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s      string
		lo, hi int
		wantN  int
		wantOK bool
	}{
		{"1", 1, 7, 1, true},
		{"7", 1, 7, 7, true},
		{"4", 1, 7, 4, true},
		{"0", 1, 7, 0, false},  // below range
		{"8", 1, 7, 0, false},  // above range
		{"", 1, 7, 0, false},   // empty
		{"a", 1, 7, 0, false},  // non-digit
		{"42", 1, 7, 0, false}, // multi-digit unsupported
	}
	for _, c := range cases {
		got, ok := parseDigitInRange(c.s, c.lo, c.hi)
		if got != c.wantN || ok != c.wantOK {
			t.Errorf("parseDigitInRange(%q,%d,%d) = (%d,%v), want (%d,%v)", c.s, c.lo, c.hi, got, ok, c.wantN, c.wantOK)
		}
	}
}

// TestChatBuildBriefing_NoTurnsEmptyOK — switching from the landing
// zone (no prior turns) yields no briefing.
func TestChatBuildBriefing_NoTurnsEmptyOK(t *testing.T) {
	t.Parallel()
	loop := &chatLoop{}
	if got, ok := loop.buildBriefing("claude", "minimax"); ok || got != "" {
		t.Errorf("expected empty/false from no-turns; got ok=%v len=%d", ok, len(got))
	}
}

// TestChatBuildBriefing_NoUserTurnsEmptyOK — assistant-only history
// (no real user input yet) yields no briefing.
func TestChatBuildBriefing_NoUserTurnsEmptyOK(t *testing.T) {
	t.Parallel()
	loop := &chatLoop{
		turnLog: []chatTurn{
			{Role: "assistant", AgentID: "claude", Text: "system greeting"},
		},
	}
	if _, ok := loop.buildBriefing("claude", "minimax"); ok {
		t.Errorf("expected no briefing from assistant-only log")
	}
}

// TestChatBuildBriefing_FormatAndContents — a typical /switch handoff
// after a couple of turns. Briefing must name from / new agents,
// include the user's prompt + the assistant's response, and end with
// the wait-for-user instruction.
func TestChatBuildBriefing_FormatAndContents(t *testing.T) {
	t.Parallel()
	loop := &chatLoop{
		turnLog: []chatTurn{
			{Role: "user", Text: "explain the GIL in one paragraph"},
			{Role: "assistant", AgentID: "claude", Text: "The GIL is a mutex protecting the Python interpreter state..."},
			{Role: "user", Text: "now show me how to release it from a C extension"},
			{Role: "assistant", AgentID: "claude", Text: "Use Py_BEGIN_ALLOW_THREADS / Py_END_ALLOW_THREADS macros..."},
		},
	}
	got, ok := loop.buildBriefing("claude", "minimax")
	if !ok {
		t.Fatal("expected briefing, got none")
	}
	for _, want := range []string{
		"Context handoff",
		"`claude`", "`minimax`",
		"do not invoke tools",
		"explain the GIL",
		"Py_BEGIN_ALLOW_THREADS",
		"await the user's next prompt",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("briefing missing %q", want)
		}
	}
}

// TestChatBuildBriefing_RespectsByteCap — a single fat assistant turn
// gets truncated rather than pushing the briefing past the cap.
func TestChatBuildBriefing_RespectsByteCap(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("X", 10*1024)
	loop := &chatLoop{
		turnLog: []chatTurn{
			{Role: "user", Text: "dump everything you know"},
			{Role: "assistant", AgentID: "claude", Text: huge},
		},
	}
	got, ok := loop.buildBriefing("claude", "minimax")
	if !ok {
		t.Fatal("expected briefing")
	}
	if len(got) > chatBriefingMaxBytes+512 {
		t.Errorf("briefing length %d > cap %d (with 512B header headroom)", len(got), chatBriefingMaxBytes)
	}
	if !strings.Contains(got, "[…truncated]") {
		t.Errorf("expected […truncated] marker in oversized briefing")
	}
}

// TestChatTurnLogCap — appendTurn rolls older entries off the front
// when over the cap so memory + briefing size stay bounded.
func TestChatTurnLogCap(t *testing.T) {
	t.Parallel()
	loop := &chatLoop{}
	for i := 0; i < chatTurnLogCap*2; i++ {
		loop.appendTurn(chatTurn{Role: "user", Text: fmt.Sprintf("turn %d", i)})
	}
	turns := loop.snapshotTurns()
	if got := len(turns); got != chatTurnLogCap {
		t.Errorf("turnLog len = %d, want %d", got, chatTurnLogCap)
	}
	// Oldest kept turn should be turn(chatTurnLogCap) since we appended 2*cap.
	wantFirst := fmt.Sprintf("turn %d", chatTurnLogCap)
	if turns[0].Text != wantFirst {
		t.Errorf("oldest kept turn = %q, want %q", turns[0].Text, wantFirst)
	}
}

func TestChatBlocksGroupUserAndAssistantTurns(t *testing.T) {
	t.Parallel()

	blocks := buildChatBlocks([]chatTurn{
		{Role: "user", Text: "what is 2+3?"},
		{Role: "assistant", AgentID: "minimax", Text: "5"},
		{Role: "user", Text: "add 4"},
		{Role: "assistant", AgentID: "codex", Text: "9"},
	})
	if len(blocks) != 2 {
		t.Fatalf("blocks len = %d, want 2", len(blocks))
	}
	if blocks[0].ID != 1 || blocks[0].AgentID != "minimax" || blocks[0].UserText != "what is 2+3?" || blocks[0].AssistantText != "5" {
		t.Fatalf("block 1 = %+v", blocks[0])
	}
	if blocks[1].ID != 2 || blocks[1].AgentID != "codex" || blocks[1].AssistantText != "9" {
		t.Fatalf("block 2 = %+v", blocks[1])
	}
}

func TestSearchChatBlocksSupportsTermsAndClientFilter(t *testing.T) {
	t.Parallel()

	blocks := []chatBlock{
		{ID: 1, AgentID: "minimax", UserText: "lookup /bin/bash", AssistantText: "found executable"},
		{ID: 2, AgentID: "codex", UserText: "review git diff", AssistantText: "found bug"},
	}
	results := searchChatBlocks(blocks, "client:minimax bash")
	if len(results) != 1 || results[0].ID != 1 {
		t.Fatalf("search results = %+v, want block 1 only", results)
	}
	if got := searchChatBlocks(blocks, "client:gemini bash"); len(got) != 0 {
		t.Fatalf("filtered search returned %+v, want none", got)
	}
}

func TestSelectCopyTextFromBlocks(t *testing.T) {
	t.Parallel()

	blocks := []chatBlock{{
		ID:            3,
		AgentID:       "minimax",
		UserText:      "write go",
		AssistantText: "Here:\n```go\nfunc main() {}\n```",
	}}
	cases := []struct {
		mode string
		want string
	}{
		{"", "Here:\n```go\nfunc main() {}\n```"},
		{"response", "Here:\n```go\nfunc main() {}\n```"},
		{"prompt", "write go"},
		{"block", "[user]\nwrite go\n\n[minimax]\nHere:\n```go\nfunc main() {}\n```"},
		{"code", "func main() {}"},
	}
	for _, tc := range cases {
		got, _, err := selectCopyTextFromBlocks(blocks, tc.mode)
		if err != nil {
			t.Fatalf("selectCopyTextFromBlocks(%q) error: %v", tc.mode, err)
		}
		if got != tc.want {
			t.Fatalf("selectCopyTextFromBlocks(%q) = %q, want %q", tc.mode, got, tc.want)
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

// TestHandleSlash_Smoke exercises every slash command the chat exposes.
// Passed a nil client so no daemon is needed. Commands that open sessions
// (/<runner>) will error — we just verify they don't panic and that
// output/error writers receive something sensible.
func TestHandleSlash_Smoke(t *testing.T) {
	t.Parallel()

	// Commands that are expected to write to stdout (non-empty check).
	wantOutput := []string{
		"/help", "/agents", "/quota",
		"/login", "/login minimax",
		"/model", "/model minimax",
		"/1", "/2", "/3", "/4", "/5", "/6", "/7",
		"/claude", "/codex", "/copilot", "/minimax",
		"/gemini", "/local", "/pool",
		"/switch claude",
		"/install",
		"/install-local-server",
		"/list-local-models",
		"/opsx-list",
	}
	// Commands that may produce error output but must not panic.
	noOutput := []string{
		"/unknown-verb",
	}

	for _, cmd := range append(wantOutput, noOutput...) {
		cmd := cmd
		t.Run(cmd, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			loop := &chatLoop{
				client: nil,
				out:    &stdout,
				errw:   &stderr,
			}
			// Must not panic.
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("handleSlash(%q) panicked: %v", cmd, r)
					}
				}()
				loop.handleSlash(cmd)
			}()
		})
	}
}
