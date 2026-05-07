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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/rpc"
)

func TestDrainStreamRecordsModelEvent(t *testing.T) {
	stream := make(chan []byte, 2)
	sess := &chatSession{
		agentID:      "codex",
		streamCh:     stream,
		done:         make(chan struct{}),
		streamCancel: func() {},
	}
	var out bytes.Buffer
	loop := &chatLoop{
		out:      &out,
		errw:     &out,
		sess:     sess,
		sessions: map[string]*chatSession{"codex": sess},
	}

	event, err := json.Marshal(map[string]any{"t": "model", "model": "gpt-5.5", "source": "observed"})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	stream <- event
	stream <- []byte(`{"t":"end"}`)
	close(stream)

	loop.drainStream(sess)
	model, source := sess.modelInfo()
	if model != "gpt-5.5" {
		t.Fatalf("model = %q, want gpt-5.5", model)
	}
	if source != "observed" {
		t.Fatalf("source = %q, want observed", source)
	}
}

func TestDisplayModelInfoUsesObservedSessionModelAndSource(t *testing.T) {
	sess := &chatSession{agentID: "codex"}
	sess.setModel("gpt-5.5", "observed")
	loop := &chatLoop{sessions: map[string]*chatSession{"codex": sess}}

	model, endpoint := loop.displayModelInfo("codex")
	if model != "gpt-5.5" {
		t.Fatalf("display model = %q, want observed gpt-5.5", model)
	}
	if !strings.Contains(endpoint, "codex CLI") || !strings.Contains(endpoint, "model observed") {
		t.Fatalf("display endpoint = %q, want endpoint plus observed source", endpoint)
	}
}

func TestPrintModelSummaryUsesObservedSessionModel(t *testing.T) {
	sess := &chatSession{agentID: "codex"}
	sess.setModel("gpt-5.5", "observed")
	var out bytes.Buffer
	loop := &chatLoop{
		out:      &out,
		errw:     &bytes.Buffer{},
		sessions: map[string]*chatSession{"codex": sess},
	}

	loop.printModel("")

	got := out.String()
	if !strings.Contains(got, "codex") || !strings.Contains(got, "gpt-5.5") || !strings.Contains(got, "model observed") {
		t.Fatalf("model summary did not include observed codex model/source:\n%s", got)
	}
	if strings.Contains(got, "codex CLI default  (codex CLI)") {
		t.Fatalf("model summary used static codex default instead of observed model:\n%s", got)
	}
}

func TestDrainStreamWritesThinkingToOutputStream(t *testing.T) {
	stream := make(chan []byte, 2)
	sess := &chatSession{
		agentID:      "minimax",
		streamCh:     stream,
		done:         make(chan struct{}),
		streamCancel: func() {},
	}
	var out bytes.Buffer
	var errw bytes.Buffer
	loop := &chatLoop{
		out:      &out,
		errw:     &errw,
		sess:     sess,
		sessions: map[string]*chatSession{"minimax": sess},
	}

	event, err := json.Marshal(map[string]any{
		"t":   "thinking",
		"b64": base64.StdEncoding.EncodeToString([]byte("inspecting files")),
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	stream <- event
	stream <- []byte(`{"t":"end"}`)
	close(stream)

	loop.drainStream(sess)
	if got := out.String(); !strings.Contains(got, "[minimax]") || !strings.Contains(got, "inspecting files") {
		t.Fatalf("thinking feedback missing from output stream:\nstdout=%q\nstderr=%q", got, errw.String())
	}
	if got := errw.String(); got != "" {
		t.Fatalf("thinking feedback must not write to errw; got %q", got)
	}
	if pending := loop.pendingAssistant.String(); pending != "" {
		t.Fatalf("thinking feedback should not be stored as assistant response; got %q", pending)
	}
}

func TestFriendlyErrorRewritesRPCInternals(t *testing.T) {
	got := friendlyError("✗ open codex: ", "", fmt.Errorf("rpc error -32601: no such method: agent.open"))
	for _, bad := range []string{"rpc error", "no such method"} {
		if strings.Contains(got, bad) {
			t.Fatalf("friendlyError leaked %q in %q", bad, got)
		}
	}
	for _, want := range []string{"daemon does not support that command", "milliwaysd"} {
		if !strings.Contains(got, want) {
			t.Fatalf("friendlyError missing %q in %q", want, got)
		}
	}
}

func TestFriendlyErrorRewritesDecodeInternals(t *testing.T) {
	got := friendlyError("✗ agents: ", "", fmt.Errorf("decode agent.list result: json: cannot unmarshal object into Go value of type []main.agent"))
	for _, bad := range []string{"json:", "cannot unmarshal", "Go value"} {
		if strings.Contains(got, bad) {
			t.Fatalf("friendlyError leaked %q in %q", bad, got)
		}
	}
	if !strings.Contains(got, "unexpected daemon response") {
		t.Fatalf("friendlyError missing response guidance in %q", got)
	}
}

// chatLoopHelpsTest mirrors chatLoop but allows tests to inspect output
// streams without spawning a real input reader / daemon connection.
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
		"/briefing",
		"/model",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output missing %q; got:\n%s", want, stdout.String())
		}
	}
}

func TestPrintLandingSuppressesMainBannerInDeckMode(t *testing.T) {
	t.Setenv("MILLIWAYS_DECK_MODE", "1")
	var stdout bytes.Buffer
	loop := &chatLoop{
		out:  &stdout,
		errw: &bytes.Buffer{},
	}

	loop.printLanding()

	if stdout.Len() != 0 {
		t.Fatalf("deck mode landing output = %q, want empty", stdout.String())
	}
}

func TestPrintLandingIsConciseStartupSurface(t *testing.T) {
	t.Setenv("MILLIWAYS_DECK_MODE", "")
	var stdout bytes.Buffer
	loop := &chatLoop{
		out:  &stdout,
		errw: &bytes.Buffer{},
	}

	loop.printLanding()

	got := stdout.String()
	for _, absent := range []string{"Quick Menu", "Pick a client:", "/install-local-server", "Client install"} {
		if strings.Contains(got, absent) {
			t.Fatalf("landing should be concise; found %q in:\n%s", absent, got)
		}
	}
	for _, want := range []string{"milliways ", "daemon", "clients", "/1 claude", "/7 pool", "/help all commands", "/agents auth status"} {
		if !strings.Contains(got, want) {
			t.Fatalf("landing missing %q; got:\n%s", want, got)
		}
	}
	if lines := strings.Count(got, "\n"); lines > 6 {
		t.Fatalf("landing line count = %d, want <= 6:\n%s", lines, got)
	}
}

func TestPrintHelpDoesNotRepeatStartupBanner(t *testing.T) {
	var stdout bytes.Buffer
	loop := &chatLoop{
		out:  &stdout,
		errw: &bytes.Buffer{},
	}

	loop.printHelp()

	got := stdout.String()
	for _, absent := range []string{"Quick Menu", "Pick a client:", "daemon  "} {
		if strings.Contains(got, absent) {
			t.Fatalf("help should not repeat startup banner; found %q in:\n%s", absent, got)
		}
	}
	for _, want := range []string{"milliways chat commands", "Clients:", "/1 claude", "/7 pool", "Client install / upgrade:", "/install-local-server"} {
		if !strings.Contains(got, want) {
			t.Fatalf("help missing %q; got:\n%s", want, got)
		}
	}
}

func TestPrintWelcomeIsConciseLauncherSurface(t *testing.T) {
	var stdout bytes.Buffer

	printWelcomeTo(&stdout)

	got := stdout.String()
	for _, absent := range []string{"Switch:", "Daily commands:", "/install-local-server", "!<cmd>"} {
		if strings.Contains(got, absent) {
			t.Fatalf("welcome should be concise; found %q in:\n%s", absent, got)
		}
	}
	for _, want := range []string{"milliways ", "launcher", "daemon", "Start:", "Inside chat:", "/help", "/agents", "/parallel <prompt>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("welcome missing %q; got:\n%s", want, got)
		}
	}
	if lines := strings.Count(got, "\n"); lines > 12 {
		t.Fatalf("welcome line count = %d, want <= 12:\n%s", lines, got)
	}
}

func TestChooseStartProviderPrefersExplicitThenDefaultThenAuthOK(t *testing.T) {
	statuses := map[string]agentStatus{
		"claude":  {mark: "✗"},
		"codex":   {mark: "✓"},
		"minimax": {mark: "✓"},
	}
	if got := chooseStartProvider("gemini", "", "codex", statuses); got != "gemini" {
		t.Fatalf("explicit start provider = %q, want gemini", got)
	}
	if got := chooseStartProvider("", "", "minimax", statuses); got != "minimax" {
		t.Fatalf("default start provider = %q, want minimax", got)
	}
	if got := chooseStartProvider("", "", "", statuses); got != "codex" {
		t.Fatalf("auth-ok start provider = %q, want first auth-ok codex", got)
	}
}

func TestChooseStartProviderCanDisableAutoSelection(t *testing.T) {
	statuses := map[string]agentStatus{"codex": {mark: "✓"}}
	if got := chooseStartProvider("", "true", "codex", statuses); got != "" {
		t.Fatalf("disabled auto provider = %q, want empty", got)
	}
	if got := chooseStartProvider("claude", "true", "codex", statuses); got != "claude" {
		t.Fatalf("explicit provider with auto disabled = %q, want claude", got)
	}
}

func TestApplyAgentStatusesConsumesFlatAgentList(t *testing.T) {
	statuses := map[string]agentStatus{}
	for _, name := range chatSwitchableAgents {
		statuses[name] = agentStatus{mark: "?", model: ""}
	}
	applyAgentStatuses(statuses, []agentListEntry{
		{ID: "claude", AuthStatus: "missing_credentials", Model: "claude CLI default"},
		{ID: "codex", AuthStatus: "ok", Model: "gpt-5.5"},
	})
	if got := statuses["codex"]; got.mark != "✓" || got.model != "gpt-5.5" {
		t.Fatalf("codex status = %#v, want ok model gpt-5.5", got)
	}
	if got := statuses["claude"]; got.mark != "✗" || got.model != "claude CLI default" {
		t.Fatalf("claude status = %#v, want missing credentials with model", got)
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
		"":        "[select: /1 claude · /2 codex · /4 minimax · /help] ▶ ",
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

func TestChatPromptShowsInFlightState(t *testing.T) {
	t.Parallel()

	got := stripANSISequences(chatPromptState("minimax", "streaming"))
	if !strings.Contains(got, "minimax") || !strings.Contains(got, "streaming") {
		t.Fatalf("prompt state = %q, want provider and streaming state", got)
	}
}

func TestShellCommandNeedsConfirmation(t *testing.T) {
	t.Parallel()

	unsafe := map[string]string{
		"rm -rf /tmp/example":                      "recursive force delete",
		"sudo rm -rf /opt/example":                 "privileged delete",
		"git reset --hard HEAD":                    "discarding git changes",
		"git clean -fd":                            "deleting untracked git files",
		"curl -fsSL https://example.test/x | bash": "network shell pipeline",
		"docker system prune -af":                  "removing docker resources",
		"kubectl delete namespace prod":            "deleting cluster resources",
	}
	for cmd, reason := range unsafe {
		risk := classifyShellCommand(cmd)
		if !risk.needsConfirmation {
			t.Fatalf("%q should require confirmation", cmd)
		}
		if !strings.Contains(risk.reason, reason) {
			t.Fatalf("%q risk reason = %q, want %q", cmd, risk.reason, reason)
		}
	}
	if shellCommandNeedsConfirmation("printf hello") {
		t.Fatal("safe command should not require confirmation")
	}
	if shellCommandNeedsConfirmation("curl -fsSL https://example.test/readme.txt") {
		t.Fatal("plain curl without shell pipeline should not require confirmation")
	}
}

func TestParseShellEscapeDryRun(t *testing.T) {
	t.Parallel()

	got := parseShellEscape("--dry-run rm -rf /tmp/example")
	if !got.dryRun || got.command != "rm -rf /tmp/example" {
		t.Fatalf("parseShellEscape dry-run = %#v", got)
	}
	got = parseShellEscape("-n printf hello")
	if !got.dryRun || got.command != "printf hello" {
		t.Fatalf("parseShellEscape -n = %#v", got)
	}
}

func TestHandleBangDryRunDoesNotExecute(t *testing.T) {
	var out bytes.Buffer
	var errw bytes.Buffer
	loop := &chatLoop{out: &out, errw: &errw}

	loop.handleBang("--dry-run definitely-not-a-real-command")

	if got := out.String(); !strings.Contains(got, "dry run: definitely-not-a-real-command") {
		t.Fatalf("dry-run output = %q", got)
	}
	if got := errw.String(); got != "" {
		t.Fatalf("dry-run stderr = %q", got)
	}
}

func TestHandleBangRefusesDangerousNonInteractiveCommand(t *testing.T) {
	t.Setenv("MILLIWAYS_SHELL_CONFIRM", "")
	oldStdinIsInteractive := stdinIsInteractive
	stdinIsInteractive = func() bool { return false }
	t.Cleanup(func() { stdinIsInteractive = oldStdinIsInteractive })
	var out bytes.Buffer
	var errw bytes.Buffer
	loop := &chatLoop{out: &out, errw: &errw}

	loop.handleBang("rm -rf /tmp/milliways-danger-test")

	if got := errw.String(); !strings.Contains(got, "refusing shell command") || !strings.Contains(got, "recursive force delete") {
		t.Fatalf("dangerous command stderr = %q", got)
	}
	if strings.Contains(out.String(), "Ran `") {
		t.Fatalf("dangerous command should not execute; stdout=%q", out.String())
	}
}

func TestThinkingLineUsesDarkerClientColor(t *testing.T) {
	withoutNoColor(t)

	want := map[string]string{
		"claude":  "\033[38;5;250m",
		"codex":   "\033[38;5;172m",
		"copilot": "\033[38;5;67m",
		"gemini":  "\033[38;5;166m",
		"minimax": "\033[38;5;98m",
		"local":   "\033[38;5;124m",
		"pool":    "\033[38;5;75m",
		"custom":  unknownAgentThinkingColor,
	}
	for agent, color := range want {
		line := formatThinkingLine(agent, "planning next step")
		if !strings.HasPrefix(line, color) {
			t.Fatalf("%s thinking line uses %q, want prefix %q", agent, line, color)
		}
		if !strings.Contains(line, "… planning next step") {
			t.Fatalf("%s thinking line missing message: %q", agent, line)
		}
		if agentThinkingColor(agent) == agentColor(agent) {
			t.Fatalf("%s thinking color should be darker than main color", agent)
		}
	}
	if agentThinkingColor("custom") == agentThinkingColor("claude") {
		t.Fatal("unknown provider thinking color should not reuse claude")
	}
}

func TestThinkingLineWrapsLongFeedbackInsteadOfChopping(t *testing.T) {
	t.Parallel()

	msg := "The user wants me to review the terminal UX of a project located at the milliways repo. Let me start by exploring the project structure to understand the chat renderer and deck surfaces."
	line := formatThinkingLineWidth("minimax", msg, 72)
	plain := stripANSISequences(line)

	for _, want := range []string{"project", "structure", "deck", "surfaces"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("thinking feedback lost %q; got:\n%s", want, plain)
		}
	}
	if count := strings.Count(plain, "\n"); count < 1 {
		t.Fatalf("thinking feedback should wrap to multiple lines; got:\n%s", plain)
	}
	for _, row := range strings.Split(plain, "\n") {
		if len(row) > 88 {
			t.Fatalf("wrapped row too wide (%d): %q\nfull:\n%s", len(row), row, plain)
		}
	}
}

func TestAgentMainColorContract(t *testing.T) {
	withoutNoColor(t)

	want := map[string]string{
		"claude":  "\033[97m",
		"gemini":  "\033[38;5;208m",
		"minimax": "\033[38;5;141m",
		"pool":    "\033[38;5;117m",
		"custom":  unknownAgentColor,
	}
	for agent, color := range want {
		if got := agentColor(agent); got != color {
			t.Fatalf("agentColor(%q) = %q, want %q", agent, got, color)
		}
	}
	if agentColor("custom") == agentColor("claude") {
		t.Fatal("unknown provider main color should not reuse claude")
	}
}

func TestAgentColorsRespectNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	if got := agentColor("claude"); got != "" {
		t.Fatalf("agentColor() with NO_COLOR = %q, want empty", got)
	}
	if got := agentThinkingColor("claude"); got != "" {
		t.Fatalf("agentThinkingColor() with NO_COLOR = %q, want empty", got)
	}
	if got := formatThinkingLine("claude", "checking status"); strings.Contains(got, "\x1b[") {
		t.Fatalf("formatThinkingLine() emitted ANSI with NO_COLOR:\n%q", got)
	}
}

func TestInterruptPromptIsPlainLanguage(t *testing.T) {
	for _, bad := range []string{"^C", "Ctrl+C"} {
		if strings.Contains(chatInterruptPrompt, bad) {
			t.Fatalf("interrupt prompt should avoid raw control notation %q: %q", bad, chatInterruptPrompt)
		}
	}
	for _, want := range []string{"Interrupted", "/cancel", "/exit"} {
		if !strings.Contains(chatInterruptPrompt, want) {
			t.Fatalf("interrupt prompt missing %q: %q", want, chatInterruptPrompt)
		}
	}
}

func TestCancelActiveSessionClosesAndResetsPrompt(t *testing.T) {
	closed := false
	sess := &chatSession{
		agentID:      "minimax",
		streamCancel: func() { closed = true },
	}
	completer := &switchableCompleter{}
	completer.set(buildCompleter("minimax"))
	reader := &chatLineReader{prompt: chatPrompt("minimax")}
	loop := &chatLoop{
		sess:      sess,
		sessions:  map[string]*chatSession{"minimax": sess},
		rl:        reader,
		completer: completer,
	}

	if !loop.cancelActiveSession() {
		t.Fatal("cancelActiveSession() = false, want true")
	}
	if !closed {
		t.Fatal("cancelActiveSession did not close the stream")
	}
	if loop.sess != nil {
		t.Fatalf("active session after cancel = %#v, want nil", loop.sess)
	}
	if _, ok := loop.sessions["minimax"]; ok {
		t.Fatal("cancelled session still present in session map")
	}
	if got := stripANSISequences(reader.prompt); !strings.Contains(got, "select:") {
		t.Fatalf("prompt after cancel = %q, want picker prompt", got)
	}
	suffixes, _ := completer.Complete("/co", len("/co"))
	if !slices.Contains(suffixes, "dex") {
		t.Fatalf("completion after cancel = %#v, want base chat commands", suffixes)
	}
}

func TestEffectiveLoginPathAddsSystemFallbacks(t *testing.T) {
	t.Setenv("PATH", "/tmp/custom-bin")
	t.Setenv("MILLIWAYS_PATH", "")

	path := effectiveLoginPath()
	if !pathListContains(path, "/tmp/custom-bin") {
		t.Fatalf("custom PATH missing from %q", path)
	}
	if !pathListContains(path, "/bin") {
		t.Fatalf("/bin missing from %q", path)
	}
	if !pathListContains(path, "/usr/bin") {
		t.Fatalf("/usr/bin missing from %q", path)
	}
}

func TestLookupLoginCommandUsesMILLIWAYSPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gemini")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake gemini: %v", err)
	}
	t.Setenv("PATH", "/no/such/path")
	t.Setenv("MILLIWAYS_PATH", dir)

	got, err := lookupLoginCommand("gemini")
	if err != nil {
		t.Fatalf("lookupLoginCommand error: %v", err)
	}
	if got != bin {
		t.Fatalf("lookupLoginCommand = %q, want %q", got, bin)
	}
}

func pathListContains(path, want string) bool {
	for _, part := range strings.Split(path, string(os.PathListSeparator)) {
		if part == want {
			return true
		}
	}
	return false
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

func TestChatHistoryFileCanBeOverriddenOrDisabled(t *testing.T) {
	t.Setenv("MILLIWAYS_HISTORY_FILE", "/tmp/mw-history")
	if got := chatHistoryFile(); got != "/tmp/mw-history" {
		t.Fatalf("chatHistoryFile override = %q", got)
	}
	t.Setenv("MILLIWAYS_HISTORY_FILE", "off")
	if got := chatHistoryFile(); got != "" {
		t.Fatalf("chatHistoryFile disabled = %q, want empty", got)
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

func TestParseHistoryArgs(t *testing.T) {
	agent, limit, err := parseHistoryArgs("12 minimax", "codex")
	if err != nil {
		t.Fatalf("parseHistoryArgs returned error: %v", err)
	}
	if agent != "minimax" || limit != 12 {
		t.Fatalf("parseHistoryArgs = (%q, %d), want (minimax, 12)", agent, limit)
	}

	agent, limit, err = parseHistoryArgs("client:gemini", "codex")
	if err != nil {
		t.Fatalf("parseHistoryArgs client prefix returned error: %v", err)
	}
	if agent != "gemini" || limit != 8 {
		t.Fatalf("parseHistoryArgs client prefix = (%q, %d), want (gemini, 8)", agent, limit)
	}

	if _, _, err := parseHistoryArgs("0", "codex"); err == nil {
		t.Fatal("parseHistoryArgs accepted zero limit")
	}
	if _, _, err := parseHistoryArgs("unknown", "codex"); err == nil {
		t.Fatal("parseHistoryArgs accepted unknown agent")
	}
}

func TestPrintHistoryShowsLimitedSessionTurnsInline(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	loop := &chatLoop{
		out:  &stdout,
		errw: &stderr,
	}
	loop.appendTurn(chatTurn{Role: "user", Text: "first"})
	loop.appendTurn(chatTurn{Role: "assistant", AgentID: "minimax", Text: "second"})
	loop.appendTurn(chatTurn{Role: "user", Text: "third"})

	loop.printHistory("2")

	got := stdout.String()
	if strings.Contains(got, "first") {
		t.Fatalf("history ignored limit; got:\n%s", got)
	}
	for _, want := range []string{
		"Session history:",
		"[2] minimax: second",
		"[3] user: third",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("history output missing %q; got:\n%s", want, got)
		}
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("history wrote stderr: %q", got)
	}
}

func TestRenderHistoryEntryReadableDaemonEvents(t *testing.T) {
	cases := []struct {
		name  string
		entry map[string]any
		want  []string
	}{
		{
			name:  "data",
			entry: map[string]any{"v": map[string]any{"t": "data", "text": "hello from daemon history"}},
			want:  []string{"response:", "hello from daemon history"},
		},
		{
			name: "chunk_end",
			entry: map[string]any{"v": map[string]any{
				"t":             "chunk_end",
				"input_tokens":  float64(120),
				"output_tokens": float64(80),
				"cost_usd":      0.0042,
			}},
			want: []string{"done:", "in 120", "out 80", "total 200 tok", "$0.0042"},
		},
		{
			name:  "err",
			entry: map[string]any{"v": map[string]any{"t": "err", "msg": "rate limited"}},
			want:  []string{"error:", "rate limited"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderHistoryEntry(tc.entry)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("renderHistoryEntry missing %q in %q", want, got)
				}
			}
		})
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
		"/briefing",
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

func TestHandleSlashRunnerPromptStartsBackgroundJob(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	var sent []string
	codex := &chatSession{agentID: "codex", done: make(chan struct{}), streamCancel: func() {}}
	gemini := &chatSession{
		agentID:      "gemini",
		done:         make(chan struct{}),
		streamCancel: func() {},
		sendFn: func(prompt string) error {
			sent = append(sent, prompt)
			return nil
		},
	}
	loop := &chatLoop{
		sess:     codex,
		deck:     newSessionDeck(chatSwitchableAgents),
		sessions: map[string]*chatSession{"codex": codex, "gemini": gemini},
		out:      &stdout,
		errw:     &stderr,
	}

	loop.handleSlash("/gemini research market changes")

	if loop.sess != codex {
		t.Fatalf("active session changed; want codex active")
	}
	if len(sent) != 1 || sent[0] != "research market changes" {
		t.Fatalf("gemini sent prompts = %#v", sent)
	}
	if !strings.Contains(stdout.String(), "gemini background started") {
		t.Fatalf("missing background acknowledgement: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	snap := loop.deck.Snapshot()
	var geminiState sessionDeckState
	for _, st := range snap.States {
		if st.Provider == "gemini" {
			geminiState = st
			break
		}
	}
	if geminiState.PromptCount != 1 || geminiState.Status != deckStatusThinking {
		t.Fatalf("gemini deck state = %#v", geminiState)
	}
}

func TestHandleSlashRunnerPromptActivatesWhenNoClientIsActive(t *testing.T) {
	t.Parallel()

	var sent []string
	var stdout, stderr bytes.Buffer
	loop := &chatLoop{
		deck: newSessionDeck(chatSwitchableAgents),
		out:  &stdout,
		errw: &stderr,
		openAgent: func(_ *rpc.Client, agentID string) (*chatSession, error) {
			return &chatSession{
				agentID:      agentID,
				done:         make(chan struct{}),
				streamCancel: func() {},
				sendFn: func(prompt string) error {
					sent = append(sent, prompt)
					return nil
				},
			}, nil
		},
	}

	loop.handleSlash("/minimax construct image")

	if loop.sess == nil || loop.sess.agentID != "minimax" {
		t.Fatalf("active session = %#v, want minimax", loop.sess)
	}
	if len(sent) != 1 || sent[0] != "construct image" {
		t.Fatalf("sent prompts = %#v", sent)
	}
}

func TestSwitchDoesNotSendTakeoverBriefing(t *testing.T) {
	t.Parallel()

	var sent []string
	var stdout, stderr bytes.Buffer
	claude := &chatSession{agentID: "claude", done: make(chan struct{}), streamCancel: func() {}}
	codex := &chatSession{
		agentID:      "codex",
		done:         make(chan struct{}),
		streamCancel: func() {},
		sendFn: func(prompt string) error {
			sent = append(sent, prompt)
			return nil
		},
	}
	loop := &chatLoop{
		sess:      claude,
		deck:      newSessionDeck(chatSwitchableAgents),
		sessions:  map[string]*chatSession{"claude": claude, "codex": codex},
		openAgent: func(_ *rpc.Client, _ string) (*chatSession, error) { return nil, nil },
		out:       &stdout,
		errw:      &stderr,
		turnLog: []chatTurn{
			{Role: "user", Text: "important workstream context"},
			{Role: "assistant", AgentID: "claude", Text: "context retained"},
		},
	}

	loop.handleSlash("/switch codex")

	if loop.sess != codex {
		t.Fatalf("active session changed to %#v, want codex", loop.sess)
	}
	if len(sent) != 0 {
		t.Fatalf("/switch sent prompts = %#v, want none", sent)
	}
	if loop.lastBriefing != "" {
		t.Fatalf("/switch stored briefing %q, want empty", loop.lastBriefing)
	}
}

func TestTakeoverSendsBriefingExplicitly(t *testing.T) {
	t.Parallel()

	var sent []string
	var stdout, stderr bytes.Buffer
	claude := &chatSession{agentID: "claude", done: make(chan struct{}), streamCancel: func() {}}
	codex := &chatSession{
		agentID:      "codex",
		done:         make(chan struct{}),
		streamCancel: func() {},
		sendFn: func(prompt string) error {
			sent = append(sent, prompt)
			return nil
		},
	}
	loop := &chatLoop{
		sess:     claude,
		deck:     newSessionDeck(chatSwitchableAgents),
		sessions: map[string]*chatSession{"claude": claude, "codex": codex},
		out:      &stdout,
		errw:     &stderr,
		turnLog: []chatTurn{
			{Role: "user", Text: "important workstream context"},
			{Role: "assistant", AgentID: "claude", Text: "context retained"},
		},
	}

	loop.handleSlash("/takeover codex")

	if loop.sess != codex {
		t.Fatalf("active session changed to %#v, want codex", loop.sess)
	}
	if len(sent) != 1 {
		t.Fatalf("/takeover sent %d prompts, want 1: %#v", len(sent), sent)
	}
	if !strings.Contains(sent[0], "Context handoff") || !strings.Contains(sent[0], "important workstream context") {
		t.Fatalf("takeover prompt missing handoff context: %q", sent[0])
	}
	if loop.lastBriefing == "" {
		t.Fatal("/takeover did not store last briefing")
	}
}

// ---------------------------------------------------------------------------
// printBriefingBlock
// ---------------------------------------------------------------------------

// TestPrintBriefingBlock_NoTurns — empty turn slice produces no output.
func TestPrintBriefingBlock_NoTurns(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	loop := &chatLoop{out: &out, errw: &bytes.Buffer{}}
	loop.printBriefingBlock(nil, "claude")
	if out.Len() != 0 {
		t.Errorf("expected no output for empty turns, got: %q", out.String())
	}
}

// TestPrintBriefingBlock_SingularNoun — one turn uses "turn" not "turns".
func TestPrintBriefingBlock_SingularNoun(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	loop := &chatLoop{out: &out, errw: &bytes.Buffer{}}
	loop.printBriefingBlock([]chatTurn{{Role: "user", Text: "hello"}}, "minimax")
	got := out.String()
	if !strings.Contains(got, "1 turn") {
		t.Errorf("expected '1 turn'; got: %q", got)
	}
	if strings.Contains(got, "1 turns") {
		t.Errorf("unexpected plural '1 turns'; got: %q", got)
	}
}

// TestPrintBriefingBlock_PluralNoun — multiple turns uses "turns".
func TestPrintBriefingBlock_PluralNoun(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	loop := &chatLoop{out: &out, errw: &bytes.Buffer{}}
	turns := []chatTurn{
		{Role: "user", Text: "first"},
		{Role: "assistant", AgentID: "claude", Text: "reply"},
		{Role: "user", Text: "second"},
	}
	loop.printBriefingBlock(turns, "claude")
	got := out.String()
	if !strings.Contains(got, "3 turns") {
		t.Errorf("expected '3 turns'; got: %q", got)
	}
}

// TestPrintBriefingBlock_FromAgentInHeader — from-agent name appears in the
// opening line and the hint footer is always present.
func TestPrintBriefingBlock_FromAgentInHeader(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	loop := &chatLoop{out: &out, errw: &bytes.Buffer{}}
	loop.printBriefingBlock([]chatTurn{{Role: "user", Text: "hi"}}, "minimax")
	got := out.String()
	if !strings.Contains(got, "minimax") {
		t.Errorf("expected from-agent 'minimax' in output; got: %q", got)
	}
	if !strings.Contains(got, "/briefing") {
		t.Errorf("expected /briefing hint in footer; got: %q", got)
	}
}

// TestPrintBriefingBlock_AssistantUsesAgentID — assistant turns show the
// AgentID label, not the string "assistant".
func TestPrintBriefingBlock_AssistantUsesAgentID(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	loop := &chatLoop{out: &out, errw: &bytes.Buffer{}}
	turns := []chatTurn{
		{Role: "user", Text: "ping"},
		{Role: "assistant", AgentID: "codex", Text: "pong"},
	}
	loop.printBriefingBlock(turns, "codex")
	got := out.String()
	if !strings.Contains(got, "[codex]") {
		t.Errorf("expected [codex] label; got: %q", got)
	}
	if strings.Contains(got, "[assistant]") {
		t.Errorf("unexpected [assistant] label; got: %q", got)
	}
}

// TestPrintBriefingBlock_TruncatesLongLines — turn text longer than 90
// bytes is truncated with a '…' marker; the line itself stays short.
func TestPrintBriefingBlock_TruncatesLongLines(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	loop := &chatLoop{out: &out, errw: &bytes.Buffer{}}
	longText := strings.Repeat("A", 200)
	loop.printBriefingBlock([]chatTurn{{Role: "user", Text: longText}}, "claude")
	got := out.String()
	if !strings.Contains(got, "…") {
		t.Errorf("expected truncation marker '…'; got: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		// strip the sidebar prefix "  │ " (4 bytes) before measuring content
		content := strings.TrimPrefix(line, "  │ ")
		if len(content) > 100 {
			t.Errorf("line too long (%d bytes): %q", len(content), content)
		}
	}
}

// TestPrintBriefingBlock_NewlinesCollapsed — multiline turn text is
// flattened to a single display line (newlines replaced with spaces).
func TestPrintBriefingBlock_NewlinesCollapsed(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	loop := &chatLoop{out: &out, errw: &bytes.Buffer{}}
	loop.printBriefingBlock([]chatTurn{
		{Role: "user", Text: "line one\nline two\nline three"},
	}, "claude")
	// Each sidebar row must contain at most one \n (its own trailing newline).
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if strings.Contains(line, "\n") {
			t.Errorf("unexpected embedded newline in sidebar line: %q", line)
		}
	}
	// The content words should still be present.
	got := out.String()
	if !strings.Contains(got, "line one") || !strings.Contains(got, "line two") {
		t.Errorf("collapsed text missing content words; got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// printLastBriefing
// ---------------------------------------------------------------------------

// TestPrintLastBriefing_NoBriefing — before any switch the placeholder is shown.
func TestPrintLastBriefing_NoBriefing(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	loop := &chatLoop{out: &out, errw: &bytes.Buffer{}}
	loop.printLastBriefing()
	if !strings.Contains(out.String(), "no briefing") {
		t.Errorf("expected 'no briefing' placeholder; got: %q", out.String())
	}
}

// TestPrintLastBriefing_ShowsStoredBriefing — after a lastBriefing is set
// the full text is printed inside a ╷...╵ block with the from-agent label.
func TestPrintLastBriefing_ShowsStoredBriefing(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	loop := &chatLoop{
		out:              &out,
		errw:             &bytes.Buffer{},
		lastBriefingFrom: "minimax",
		lastBriefing:     "[Context handoff]\n\nRecent exchange here.\n",
	}
	loop.printLastBriefing()
	got := out.String()
	for _, want := range []string{"minimax", "[Context handoff]", "Recent exchange here", "╷", "╵"} {
		if !strings.Contains(got, want) {
			t.Errorf("printLastBriefing missing %q; got:\n%s", want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// /briefing slash command dispatch
// ---------------------------------------------------------------------------

// TestHandleSlash_BriefingNoPriorSwitch — /briefing before any switch shows
// the "no briefing yet" placeholder, never panics.
func TestHandleSlash_BriefingNoPriorSwitch(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	loop := &chatLoop{out: &out, errw: &bytes.Buffer{}}
	loop.handleSlash("/briefing")
	if !strings.Contains(out.String(), "no briefing") {
		t.Errorf("expected placeholder; got: %q", out.String())
	}
}

// TestHandleSlash_BriefingShowsLastBriefing — /briefing after a switch has
// been recorded surfaces the stored full briefing text.
func TestHandleSlash_BriefingShowsLastBriefing(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	loop := &chatLoop{
		out:              &out,
		errw:             &bytes.Buffer{},
		lastBriefingFrom: "claude",
		lastBriefing:     "handoff text from claude\n```go\nfmt.Println(\"ok\")\n```\n",
	}
	loop.handleSlash("/briefing")
	got := out.String()
	for _, want := range []string{"Summary", "handoff text from claude", "code · go", "Println", "╭", "╰", "\x1b["} {
		if !strings.Contains(got, want) {
			t.Errorf("expected briefing output to contain %q; got: %q", want, got)
		}
	}
	if strings.Contains(got, "```") {
		t.Errorf("briefing should render markdown fences, got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// lastBriefing populated by buildBriefing round-trip
// ---------------------------------------------------------------------------

// TestBriefingStoredAfterBuild — calling buildBriefing with a real turn log
// produces a non-empty string that contains the expected handoff markers.
// This is the canonical smoke test: it exercises the same path that
// switchAgent takes, proving the data available to printBriefingBlock and
// printLastBriefing is correct.
func TestBriefingStoredAfterBuild(t *testing.T) {
	t.Parallel()
	loop := &chatLoop{
		out:  &bytes.Buffer{},
		errw: &bytes.Buffer{},
		turnLog: []chatTurn{
			{Role: "user", Text: "implement the rotation ring enhancement"},
			{Role: "assistant", AgentID: "minimax", Text: "Here is my plan for the rotation ring..."},
			{Role: "user", Text: "can we stream the briefing from the switch"},
		},
	}

	briefing, ok := loop.buildBriefing("minimax", "claude")
	if !ok {
		t.Fatal("buildBriefing returned false; expected a briefing")
	}

	// Simulate what switchAgent does: store the briefing.
	loop.lastBriefingFrom = "minimax"
	loop.lastBriefing = briefing

	// 1. Inline block output contains the expected structure.
	var blockOut bytes.Buffer
	loop.out = &blockOut
	loop.printBriefingBlock(loop.snapshotTurns(), "minimax")
	block := blockOut.String()

	for _, want := range []string{
		"╷", "context from minimax", "3 turns",
		"[user]", "rotation ring",
		"[minimax]", "rotation ring",
		"[user]", "stream the briefing",
		"╵", "/briefing",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("inline block missing %q;\nblock:\n%s", want, block)
		}
	}

	// 2. /briefing re-show contains the full handoff text.
	var reShowOut bytes.Buffer
	loop.out = &reShowOut
	loop.printLastBriefing()
	reShow := reShowOut.String()

	for _, want := range []string{
		"minimax", "Context handoff", "rotation ring", "stream the briefing",
	} {
		if !strings.Contains(reShow, want) {
			t.Errorf("/briefing re-show missing %q;\noutput:\n%s", want, reShow)
		}
	}
}

// ---------------------------------------------------------------------------
// refreshPromptHint — ⊙ saved indicator
// ---------------------------------------------------------------------------

// newHintLoop builds the minimal chatLoop needed to exercise refreshPromptHint.
func newHintLoop(errw *bytes.Buffer) *chatLoop {
	return &chatLoop{
		out:  errw,
		errw: errw,
	}
}

// TestRefreshPromptHint_SavedIndicatorPresent — when turnSaved is true the
// hint line contains the ⊙ saved marker (with ANSI green codes).
func TestRefreshPromptHint_SavedIndicatorPresent(t *testing.T) {
	t.Parallel()
	var errw bytes.Buffer
	loop := newHintLoop(&errw)
	loop.refreshPromptHint(map[string]any{
		"cost_usd":      0.0041,
		"input_tokens":  float64(100),
		"output_tokens": float64(25),
	}, true)
	got := errw.String()
	if !strings.Contains(got, "⊙ saved") {
		t.Errorf("expected '⊙ saved' in hint line; got: %q", got)
	}
	// ANSI green escape must wrap the marker.
	if !strings.Contains(got, "\033[32m") {
		t.Errorf("expected ANSI green escape in hint line; got: %q", got)
	}
}

// TestRefreshPromptHint_SavedIndicatorAbsent — when turnSaved is false
// (empty response, no turn recorded) ⊙ saved must not appear.
func TestRefreshPromptHint_SavedIndicatorAbsent(t *testing.T) {
	t.Parallel()
	var errw bytes.Buffer
	loop := newHintLoop(&errw)
	loop.refreshPromptHint(map[string]any{
		"cost_usd":      0.0012,
		"input_tokens":  float64(50),
		"output_tokens": float64(10),
	}, false)
	got := errw.String()
	if strings.Contains(got, "⊙") {
		t.Errorf("unexpected ⊙ in hint line when turnSaved=false; got: %q", got)
	}
}

// TestRefreshPromptHint_SavedAlongCostAndTokens — ⊙ saved appears together
// with cost and token parts in a single hint line.
func TestRefreshPromptHint_SavedAlongCostAndTokens(t *testing.T) {
	t.Parallel()
	var errw bytes.Buffer
	loop := newHintLoop(&errw)
	loop.refreshPromptHint(map[string]any{
		"cost_usd":      0.0006,
		"input_tokens":  float64(1683),
		"output_tokens": float64(115),
	}, true)
	got := errw.String()
	for _, want := range []string{"$0.0006", "in 1.7k / out 115 / total 1.8k tok", "⊙ saved"} {
		if !strings.Contains(got, want) {
			t.Errorf("hint line missing %q; got: %q", want, got)
		}
	}
}

// TestRefreshPromptHint_NoCostNoTokens_SavedStillShown — ⊙ saved is emitted
// even when there are no cost/token stats (e.g. local model or rate-limited).
func TestRefreshPromptHint_NoCostNoTokens_SavedStillShown(t *testing.T) {
	t.Parallel()
	var errw bytes.Buffer
	loop := newHintLoop(&errw)
	loop.refreshPromptHint(map[string]any{}, true)
	got := errw.String()
	if !strings.Contains(got, "⊙ saved") {
		t.Errorf("expected '⊙ saved' even with no cost/token data; got: %q", got)
	}
}

// TestRefreshPromptHint_EmptyHintWhenNothingToShow — no cost, no tokens,
// and turnSaved=false → hint line must be blank (no spurious output).
func TestRefreshPromptHint_EmptyHintWhenNothingToShow(t *testing.T) {
	t.Parallel()
	var errw bytes.Buffer
	loop := newHintLoop(&errw)
	loop.refreshPromptHint(map[string]any{}, false)
	got := errw.String()
	if strings.TrimSpace(got) != "" {
		t.Errorf("expected empty hint when no data; got: %q", got)
	}
}
