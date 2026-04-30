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

// Interactive chat loop on top of daemon RPC. The user persona this
// satisfies: AI-clients-only (typed prompts → active runner) + ! prefix
// for one-off bash escapes + slash commands for runner switching and
// ops. Replaces the user-facing surface that was lost when
// internal/repl/ was deleted in v0.5.0.
//
// Architecture:
//   - One rpc.Client per chat session (daemon UDS).
//   - One agent.open + agent.stream subscription per active runner.
//     Switching runner closes the current session and opens the new one.
//   - chzyer/readline for the input line (history, basic editing).
//   - Stream events drained in a goroutine, content deltas decoded from
//     base64 and printed to stdout in real time.
//   - The reader loop blocks on user input, dispatches by first char:
//       /  → slash command (switch / help / exit / quota / agents)
//       !  → shell escape via $SHELL -c "<cmd>"
//       …  → agent.send the line to the active runner
//   - Ctrl+C cancels the current dispatch (best-effort: we close + reopen
//     the agent stream). Ctrl+D exits cleanly.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/chzyer/readline"
	"github.com/mwigge/milliways/internal/mempalace"
	"github.com/mwigge/milliways/internal/project"
	"github.com/mwigge/milliways/internal/rpc"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// chatSwitchableAgents is the set of runner IDs the user can switch to
// via the /<name> shorthand, the /N numeric shortcut, or /switch <name>.
// The order here defines the /1..7 numeric mapping; mirrors the daemon's
// dispatch table in internal/daemon/agents.go but ordered for the
// landing-zone display (most-used first).
var chatSwitchableAgents = []string{
	"claude",  // /1
	"codex",   // /2
	"copilot", // /3
	"minimax", // /4 — matches wezterm Leader+1..4 mapping
	"gemini",  // /5
	"local",   // /6
	"pool",    // /7
}

// chatCtlAliases maps user-facing slash commands to the milliwaysctl
// argv they invoke under the hood. milliwaysctl itself is internal
// plumbing — users see the slash command, never the ctl call.
//
// Adding a new ctl subcommand: also add an alias entry here so it's
// reachable from the chat. The wezterm Leader+/ palette has the same
// shape (cmd/milliwaysctl/milliways.lua's ctl_choices) — keep them in
// sync until they share a generated source.
var chatCtlAliases = map[string][]string{
	// Client install — `/install <client>` shells to milliwaysctl install.
	// The dispatcher appends the rest of the line (the client name) so a
	// single alias entry covers /install claude, /install codex, etc.
	"install": {"install"},
	// Local-model bootstrap
	"install-local-server": {"local", "install-server"},
	"install-local-swap":   {"local", "install-swap"},
	"list-local-models":    {"local", "list-models"},
	"switch-local-server":  {"local", "switch-server"},
	"download-local-model": {"local", "download-model"},
	"setup-local-model":    {"local", "setup-model"},
	// OpenSpec wrappers
	"opsx-list":     {"opsx", "list"},
	"opsx-status":   {"opsx", "status"},
	"opsx-show":     {"opsx", "show"},
	"opsx-archive":  {"opsx", "archive"},
	"opsx-validate": {"opsx", "validate"},
}

// buildCompleter returns a readline AutoCompleter for all slash commands.
// Agent shortcuts (/claude, /gemini, …) and numbered aliases (/1..7) are
// derived from chatSwitchableAgents; ctl aliases from chatCtlAliases.
func buildCompleter() readline.AutoCompleter {
	installClients := []readline.PrefixCompleterInterface{
		readline.PcItem("claude"),
		readline.PcItem("codex"),
		readline.PcItem("copilot"),
		readline.PcItem("gemini"),
		readline.PcItem("local"),
	}
	switchRunners := make([]readline.PrefixCompleterInterface, len(chatSwitchableAgents))
	for i, name := range chatSwitchableAgents {
		switchRunners[i] = readline.PcItem(name)
	}

	items := []readline.PrefixCompleterInterface{
		// Numbered shortcuts /1../7
		readline.PcItem("/1"), readline.PcItem("/2"), readline.PcItem("/3"),
		readline.PcItem("/4"), readline.PcItem("/5"), readline.PcItem("/6"),
		readline.PcItem("/7"),
		// Agent name shortcuts
	}
	for _, name := range chatSwitchableAgents {
		items = append(items, readline.PcItem("/"+name))
	}
	// Session commands
	items = append(items,
		readline.PcItem("/switch", switchRunners...),
		readline.PcItem("/login", switchRunners...),
		readline.PcItem("/model"),
		readline.PcItem("/agents"),
		readline.PcItem("/quota"),
		readline.PcItem("/help"),
		readline.PcItem("/exit"),
		// Install
		readline.PcItem("/install", installClients...),
		readline.PcItem("/install-local-server"),
		readline.PcItem("/install-local-swap"),
		readline.PcItem("/list-local-models"),
		readline.PcItem("/switch-local-server",
			readline.PcItem("llama-server"),
			readline.PcItem("llama-swap"),
			readline.PcItem("ollama"),
			readline.PcItem("vllm"),
			readline.PcItem("lmstudio"),
		),
		readline.PcItem("/download-local-model"),
		readline.PcItem("/setup-local-model"),
		// OpenSpec
		readline.PcItem("/opsx-list"),
		readline.PcItem("/opsx-status"),
		readline.PcItem("/opsx-show"),
		readline.PcItem("/opsx-archive"),
		readline.PcItem("/opsx-validate"),
	)
	return readline.NewPrefixCompleter(items...)
}

// runChat is the entry point invoked by the cobra `chat` subcommand AND
// by the launcher when the user types `milliways` (no args) inside
// milliways-term.
//
// Returns a non-nil error only on initialisation failure (daemon
// unreachable, agent.open denied). Loop-level errors (single-prompt
// failures, transient network blips) are surfaced inline and the loop
// continues.
func runChat(ctx context.Context) error {
	sock := daemonSocket()
	if !socketReachable(sock, 500*time.Millisecond) {
		return fmt.Errorf("milliwaysd not reachable at %s — start MilliWays.app or run `milliwaysd &` first", sock)
	}
	client, err := rpc.Dial(sock)
	if err != nil {
		return fmt.Errorf("dial milliwaysd: %w", err)
	}
	defer client.Close()

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          chatPrompt(""),
		HistoryFile:     chatHistoryFile(),
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		AutoComplete:    buildCompleter(),
	})
	if err != nil {
		return fmt.Errorf("readline init: %w", err)
	}
	defer rl.Close()

	loop := &chatLoop{
		client: client,
		sess:   nil, // landing zone — no active agent until /<runner> picks one
		rl:     rl,
		out:    os.Stdout,
		errw:   os.Stderr,
	}

	// Wire palace recall for daemon runner sessions. Resolve the project from
	// cwd; if a palace exists, connect and inject context on every user prompt.
	if pc, err := project.ResolveProject(""); err == nil {
		palacePath := ""
		if pc.PalacePath != nil {
			palacePath = *pc.PalacePath
		}
		if mcpCmd, mcpArgs := detectMempalaceMCP(palacePath); mcpCmd != "" {
			if palaceClient, err := mempalace.NewClient(mcpCmd, mcpArgs...); err == nil {
				loop.palace = palaceClient
				defer func() { _ = palaceClient.Close() }()
			}
		}
	}

	loop.printLanding()
	return loop.run(ctx)
}

// chatCmd returns the cobra subcommand `milliways chat`. Wires runChat
// into the cobra surface for users who explicitly want a chat session
// from any context (not just inside milliways-term).
func chatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Interactive chat with the active runner (slash commands + ! shell escape)",
		Long: `Open an interactive chat session with the active runner. Daemon must
be running (start with MilliWays.app or 'milliwaysd &').

Slash commands:
  /<runner>          switch active runner (claude / codex / copilot / gemini /
                     local / minimax / pool)
  /switch <runner>   same
  /agents            list runners with auth status
  /quota             current quota snapshot
  /help              show all slash commands
  /exit              exit chat (Ctrl+D also works)

Shell escape:
  !<command>         run <command> via $SHELL -c "..."

Anything else is sent to the active runner as a prompt.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChat(cmd.Context())
		},
	}
}

// chatHistoryFile returns the path for readline history, or "" to
// disable history (when the user has no resolvable home dir).
func chatHistoryFile() string {
	if h := os.Getenv("XDG_STATE_HOME"); h != "" {
		return h + "/milliways/chat_history"
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home + "/.local/state/milliways/chat_history"
	}
	return ""
}

// chatPrompt renders the readline prompt header for the active agent.
// The active runner name is coloured with its identity colour; the
// reset brings everything back to default before the ▶ cursor.
// The empty-string case is the plain landing-zone prompt.
func chatPrompt(agentID string) string {
	if agentID == "" {
		return "[no client — pick one with /1../7 or /<name>] ▶ "
	}
	color := agentColor(agentID)
	reset := "\033[0m"
	return "[" + color + agentID + reset + "] ▶ "
}

// chatSession owns the lifecycle of one (agent.open + agent.stream)
// pair. Closing the session closes the daemon-side handle.
type chatSession struct {
	client       *rpc.Client
	agentID      string
	handle       int64
	streamCh     <-chan []byte
	streamCancel func()

	// done is closed when the streaming goroutine exits (either the
	// stream channel closed or the session was explicitly closed).
	done chan struct{}

	// busy guards a single in-flight prompt. agent.send returns
	// immediately but the response streams asynchronously; we track the
	// chunk_end signal to know when the next prompt can be issued.
	busyMu sync.Mutex
	busy   bool
}

// openAgentForChat opens a new agent session via daemon RPC and starts
// streaming its events.
func openAgentForChat(client *rpc.Client, agentID string) (*chatSession, error) {
	var openResp struct {
		Handle int64 `json:"handle"`
	}
	if err := client.Call("agent.open", map[string]any{"agent_id": agentID}, &openResp); err != nil {
		return nil, fmt.Errorf("agent.open %s: %w", agentID, err)
	}
	events, cancel, err := client.Subscribe("agent.stream", map[string]any{"handle": openResp.Handle})
	if err != nil {
		return nil, fmt.Errorf("agent.stream %s: %w", agentID, err)
	}
	return &chatSession{
		client:       client,
		agentID:      agentID,
		handle:       openResp.Handle,
		streamCh:     events,
		streamCancel: cancel,
		done:         make(chan struct{}),
	}, nil
}

// close terminates the daemon-side session and stops the stream
// subscription. Idempotent.
func (s *chatSession) close() error {
	if s == nil {
		return nil
	}
	s.streamCancel()
	if err := s.client.Call("agent.close", map[string]any{"handle": s.handle}, nil); err != nil {
		// Best-effort. The stream cancel above is what matters for resource
		// cleanup; agent.close is just notifying the daemon.
		return err
	}
	return nil
}

// send dispatches a user prompt to the active runner.
func (s *chatSession) send(prompt string) error {
	s.busyMu.Lock()
	s.busy = true
	s.busyMu.Unlock()
	return s.client.Call("agent.send", map[string]any{
		"handle": s.handle,
		"bytes":  prompt,
	}, nil)
}

// chatLoop ties the readline input + the daemon stream + slash dispatch
// into one foreground loop.
//
// Memory bridge (v0.7.0): the loop accumulates per-runner turns in
// turnLog so that on /switch, the briefing builder can hand the new
// runner the recent exchange. Today the log is in-memory only — lost
// on chat exit. Future work persists via daemon's history.* RPCs and/or
// mempalace's conversation primitive.
type chatLoop struct {
	client *rpc.Client
	sess   *chatSession
	rl     *readline.Instance
	out    io.Writer
	errw   io.Writer
	// palace, when non-nil, is queried on each user prompt to inject
	// relevant project memory as a context prefix before the runner sees it.
	palace *mempalace.Client

	// turnLog is the rolling exchange across whichever runners the user
	// has talked to in this chat session. Capped at chatTurnLogCap most-
	// recent turns to bound briefing size and memory.
	turnMu  sync.Mutex
	turnLog []chatTurn
	// pendingAssistant accumulates streamed deltas for the in-flight
	// assistant response. Drained into turnLog on chunk_end.
	pendingAssistant strings.Builder
}

// chatTurn is one exchange entry across runners. Role is "user" or
// "assistant"; for assistant turns AgentID names which runner produced
// the text. Used to build the briefing on /switch.
type chatTurn struct {
	Role    string
	AgentID string // empty for user turns
	Text    string
}

// chatTurnLogCap caps how many turns we keep in memory. Old turns roll
// off the front. 12 = roughly 6 user/assistant pairs, comfortably
// covers a meaningful exchange without dragging the briefing past the
// briefing-cap below.
const chatTurnLogCap = 12

// chatBriefingMaxBytes is the upper bound on a /switch briefing body.
// Long assistant responses (multi-KB code dumps) are truncated with a
// "[…truncated]" marker so a single fat turn doesn't blow the new
// runner's context window.
const chatBriefingMaxBytes = 4096

func (l *chatLoop) run(ctx context.Context) error {
	// drainStream is started per-session inside switchAgent; do NOT start
	// it here because l.sess is nil in the landing zone.
	defer func() {
		if l.sess != nil {
			_ = l.sess.close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := l.rl.Readline()
		if err == readline.ErrInterrupt {
			// Ctrl+C — abort current dispatch (best-effort) but stay in loop.
			fmt.Fprintln(l.errw, "^C  (Ctrl+D to exit)")
			continue
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("readline: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "/"):
			l.handleSlash(line)
		case strings.HasPrefix(line, "!"):
			l.handleBang(strings.TrimSpace(line[1:]))
		default:
			l.handlePrompt(line)
		}
	}
}

// drainStream reads NDJSON events from the daemon stream and writes
// content deltas to stdout. Recognised event types:
//   - data       — base64-encoded content; write decoded bytes to stdout
//   - chunk_end  — end of one prompt response; print a trailing newline
//                  if the runner didn't, clear busy
//   - err        — runner error; print and clear busy
//   - rate_limit — surface as inline notice
//   - end        — agent session closed
func (l *chatLoop) drainStream() {
	defer close(l.sess.done)
	for line := range l.sess.streamCh {
		var ev map[string]any
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		t, _ := ev["t"].(string)
		switch t {
		case "data":
			if b64, ok := ev["b64"].(string); ok {
				if raw, err := base64.StdEncoding.DecodeString(b64); err == nil {
					_, _ = l.out.Write(raw)
					// Accumulate for the in-flight assistant turn so /switch
					// can carry the response forward as part of the briefing.
					l.pendingAssistant.Write(raw)
				}
			}
		case "chunk_end":
			fmt.Fprintln(l.out)
			// Snapshot + reset the streamed response into a turn entry.
			if text := strings.TrimRight(l.pendingAssistant.String(), "\n"); text != "" {
				l.appendTurn(chatTurn{Role: "assistant", AgentID: l.sess.agentID, Text: text})
			}
			l.pendingAssistant.Reset()
			l.sess.busyMu.Lock()
			l.sess.busy = false
			l.sess.busyMu.Unlock()
			l.refreshPromptHint(ev)
			// Refresh the readline prompt so the user sees ▶ ready to type.
			l.rl.Refresh()
		case "err":
			msg, _ := ev["msg"].(string)
			fmt.Fprintln(l.errw, "✗ "+msg)
			if strings.Contains(msg, "not set") || strings.Contains(msg, "API_KEY") {
				fmt.Fprintln(l.errw, "  → /login  for auth setup")
			}
			l.sess.busyMu.Lock()
			l.sess.busy = false
			l.sess.busyMu.Unlock()
			l.rl.Refresh()
		case "rate_limit":
			status, _ := ev["status"].(string)
			fmt.Fprintln(l.errw, "⚠ rate limit: "+status)
		case "end":
			return
		}
	}
}

// refreshPromptHint optionally folds chunk_end metadata (token count,
// max_turns_hit) into a one-line trailer below the response so the user
// sees cost/turn signal without flooding stdout.
func (l *chatLoop) refreshPromptHint(chunkEnd map[string]any) {
	var parts []string
	if cost, ok := chunkEnd["cost_usd"].(float64); ok && cost > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", cost))
	}
	if in, ok := chunkEnd["input_tokens"].(float64); ok && in > 0 {
		if outT, _ := chunkEnd["output_tokens"].(float64); outT > 0 {
			parts = append(parts, fmt.Sprintf("%d→%d tok", int(in), int(outT)))
		}
	}
	if mh, _ := chunkEnd["max_turns_hit"].(bool); mh {
		parts = append(parts, "⚠ max-turns-hit")
	}
	if len(parts) > 0 {
		fmt.Fprintln(l.errw, "  ("+strings.Join(parts, " · ")+")")
	}
}

// handleSlash dispatches a /<word> [args...] line.
func (l *chatLoop) handleSlash(line string) {
	verb := strings.TrimPrefix(line, "/")
	rest := ""
	if i := strings.IndexByte(verb, ' '); i >= 0 {
		rest = strings.TrimSpace(verb[i+1:])
		verb = verb[:i]
	}

	// Numeric shortcut: /1 .. /7 → chatSwitchableAgents[N-1]
	if n, ok := parseDigitInRange(verb, 1, len(chatSwitchableAgents)); ok {
		l.switchAgent(chatSwitchableAgents[n-1])
		return
	}

	// Switch shorthand: /<runner>
	for _, name := range chatSwitchableAgents {
		if verb == name {
			l.switchAgent(name)
			return
		}
	}

	// Curated ctl alias: /<alias> → milliwaysctl <args...> [rest...]
	if args, ok := chatCtlAliases[verb]; ok {
		l.runCtl(append(append([]string{}, args...), splitFields(rest)...))
		return
	}

	switch verb {
	case "switch":
		if rest == "" {
			fmt.Fprintln(l.errw, "usage: /switch <agent>  — see /agents for the list")
			return
		}
		l.switchAgent(rest)
	case "agents":
		l.printAgents()
	case "model", "models":
		agent := ""
		if l.sess != nil {
			agent = l.sess.agentID
		}
		if rest == "" {
			l.printModel(agent)
		} else {
			// /model <name> — switch model for the active runner.
			l.setModel(agent, rest)
		}
	case "login":
		agent := rest
		if agent == "" && l.sess != nil {
			agent = l.sess.agentID
		}
		l.printLogin(agent)
	case "quota":
		l.printQuota()
	case "help", "?":
		l.printHelp()
	case "exit", "quit", "bye":
		fmt.Fprintln(l.out, "bye")
		if l.sess != nil {
			_ = l.sess.close()
		}
		_ = l.rl.Close()
		os.Exit(0)
	case "":
		// Bare "/" — show help.
		l.printHelp()
	default:
		// Unknown verb — try shelling to milliwaysctl as a generic
		// fallback (mirrors the wezterm palette's free-form escape).
		// This makes any future ctl subcommand reachable from chat
		// without a code change here.
		l.runCtl(append([]string{verb}, splitFields(rest)...))
	}
}

// parseDigitInRange returns (n, true) if s is a single digit in
// [lo, hi], else (0, false).
func parseDigitInRange(s string, lo, hi int) (int, bool) {
	if len(s) != 1 || s[0] < '0' || s[0] > '9' {
		return 0, false
	}
	n := int(s[0] - '0')
	if n < lo || n > hi {
		return 0, false
	}
	return n, true
}

// splitFields splits on whitespace, dropping empty fields. Used for
// chat-side argv parsing of the rest-of-line after a slash command.
func splitFields(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

// runCtl shells to milliwaysctl with the given argv and streams its
// stdout/stderr inline. milliwaysctl is internal plumbing — users see
// /<alias> not the underlying ctl call. Reuses the user's PATH lookup
// so MILLIWAYSCTL_BIN env-var override works.
func (l *chatLoop) runCtl(args []string) {
	if len(args) == 0 {
		return
	}
	bin := lookupCtlBinary()
	if bin == "" {
		fmt.Fprintln(l.errw, "✗ milliwaysctl not on PATH; install with `make install` or set MILLIWAYSCTL_BIN")
		return
	}
	c := exec.Command(bin, args...)
	c.Stdin = os.Stdin
	c.Stdout = l.out
	c.Stderr = l.errw
	if err := c.Run(); err != nil {
		fmt.Fprintln(l.errw, "✗ ctl: "+err.Error())
	}
}

// lookupCtlBinary resolves the milliwaysctl path via env override or
// $PATH. Tested via env-injection from chat_test.go.
func lookupCtlBinary() string {
	if env := strings.TrimSpace(os.Getenv("MILLIWAYSCTL_BIN")); env != "" {
		return env
	}
	if p, err := exec.LookPath("milliwaysctl"); err == nil {
		return p
	}
	return ""
}

// switchAgent closes the current session (if any) and opens a new one
// for newID. From the landing zone (no current session), this is the
// "first switch" that drops the user into a client.
//
// Memory bridge (v0.7.0): if the user is switching from one runner to
// another and there are recent turns in the log, build a briefing and
// send it as the new runner's first prompt. The new runner sees the
// recent exchange + an instruction to wait for the user before taking
// any action. From the landing zone (no prior session), no briefing is
// sent.
func (l *chatLoop) switchAgent(newID string) {
	if l.client == nil {
		fmt.Fprintln(l.errw, "✗ daemon not connected — start milliwaysd first")
		return
	}
	if l.sess != nil && newID == l.sess.agentID {
		fmt.Fprintln(l.errw, "(already on "+newID+")")
		return
	}
	known := false
	for _, a := range chatSwitchableAgents {
		if a == newID {
			known = true
			break
		}
	}
	if !known {
		fmt.Fprintln(l.errw, "✗ unknown agent: "+newID+"  (see /agents)")
		return
	}

	var fromID string
	if l.sess != nil {
		fromID = l.sess.agentID
		if err := l.sess.close(); err != nil {
			fmt.Fprintln(l.errw, "warn: closing previous session: "+err.Error())
		}
		<-l.sess.done
	}

	newSess, err := openAgentForChat(l.client, newID)
	if err != nil {
		fmt.Fprintln(l.errw, "✗ open "+newID+": "+err.Error())
		return
	}
	l.sess = newSess
	go l.drainStream()
	l.rl.SetPrompt(chatPrompt(newID))

	// Memory bridge — only when there's actual prior conversation to carry.
	if fromID != "" && fromID != newID {
		if briefing, ok := l.buildBriefing(fromID, newID); ok {
			m, ep := runnerModelInfo(newID)
			fmt.Fprintf(l.out, "→ %s  model: %s  (%s)  [briefing from %s]\n", newID, m, ep, fromID)
			if err := newSess.send(briefing); err != nil {
				fmt.Fprintln(l.errw, "warn: send briefing: "+err.Error())
			}
			return
		}
	}
	// Print the live model + endpoint so the user knows exactly what's active.
	m, ep := runnerModelInfo(newID)
	fmt.Fprintf(l.out, "→ %s  model: %s  (%s)\n", newID, m, ep)
}

// buildBriefing assembles a handoff message summarising the recent
// exchange so the new runner can continue the conversation. Returns
// (briefing, true) when there's at least one user turn to carry, or
// ("", false) for a clean landing-zone entry / no-history switch.
//
// Format: a short framing header, the recent turns rendered in role
// blocks, and an instruction to wait for the next user prompt before
// taking action. Capped at chatBriefingMaxBytes total — long assistant
// responses get a "[…truncated]" marker so a single fat turn doesn't
// blow the new runner's context window.
func (l *chatLoop) buildBriefing(fromID, newID string) (string, bool) {
	turns := l.snapshotTurns()
	if len(turns) == 0 {
		return "", false
	}
	// Did the user actually say anything? Without a user turn the
	// briefing has no semantic content.
	hasUser := false
	for _, t := range turns {
		if t.Role == "user" {
			hasUser = true
			break
		}
	}
	if !hasUser {
		return "", false
	}

	var b strings.Builder
	fmt.Fprintln(&b, "[Context handoff]")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "You are taking over a conversation that the user was having with `"+fromID+"`. Below is the recent exchange (most-recent last). The user has just switched to you (`"+newID+"`).")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "INSTRUCTIONS:")
	fmt.Fprintln(&b, "- Treat this handoff as context only — do not invoke tools or take destructive actions based on it alone.")
	fmt.Fprintln(&b, "- Wait for the user's next message before acting.")
	fmt.Fprintln(&b, "- If the user asks 'where were we', summarise from this exchange.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "RECENT EXCHANGE")
	fmt.Fprintln(&b, "===============")

	header := b.Len()
	budget := chatBriefingMaxBytes - header - 64 // leave headroom for footer

	rendered := renderTurnsWithBudget(turns, budget)
	b.WriteString(rendered)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "===============")
	fmt.Fprintln(&b, "(End of handoff. Reply briefly to acknowledge, then await the user's next prompt.)")
	return b.String(), true
}

// renderTurnsWithBudget renders turns into the briefing body, capping
// the cumulative byte count at `budget`. Earlier turns are dropped
// (most-recent kept); within a kept turn that's individually too long,
// the body is truncated with a marker.
func renderTurnsWithBudget(turns []chatTurn, budget int) string {
	// Render newest-first so we can count budget greedily, then reverse.
	type rendered struct {
		text string
		size int
	}
	var blocks []rendered
	used := 0
	for i := len(turns) - 1; i >= 0; i-- {
		t := turns[i]
		text := renderOneTurn(t)
		if used+len(text) > budget {
			// Try to fit a truncated version.
			room := budget - used
			if room < 80 {
				break // not worth it
			}
			text = renderOneTurnTruncated(t, room)
		}
		blocks = append(blocks, rendered{text: text, size: len(text)})
		used += len(text)
		if used >= budget {
			break
		}
	}
	// Reverse to chronological order.
	var b strings.Builder
	for i := len(blocks) - 1; i >= 0; i-- {
		b.WriteString(blocks[i].text)
	}
	return b.String()
}

func renderOneTurn(t chatTurn) string {
	prefix := "User"
	if t.Role == "assistant" {
		prefix = t.AgentID
	}
	return "\n[" + prefix + "]\n" + t.Text + "\n"
}

func renderOneTurnTruncated(t chatTurn, max int) string {
	const marker = "\n[…truncated]\n"
	prefix := "User"
	if t.Role == "assistant" {
		prefix = t.AgentID
	}
	header := "\n[" + prefix + "]\n"
	footer := marker
	bodyBudget := max - len(header) - len(footer)
	if bodyBudget < 1 {
		return header + footer
	}
	body := t.Text
	if len(body) > bodyBudget {
		body = body[:bodyBudget]
	}
	return header + body + footer
}

// handleBang runs an arbitrary shell command via $SHELL -c "<cmd>".
// stdin/stdout/stderr passthrough so interactive tools (less, vim)
// behave reasonably.
func (l *chatLoop) handleBang(cmd string) {
	if cmd == "" {
		fmt.Fprintln(l.errw, "usage: !<command>")
		return
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	c := exec.Command(shell, "-c", cmd)
	c.Stdin = os.Stdin
	c.Stdout = l.out
	c.Stderr = l.errw
	if err := c.Run(); err != nil {
		fmt.Fprintln(l.errw, "✗ shell: "+err.Error())
	}
}

// enrichWithPalace prepends relevant project memory to prompt. Returns
// the original prompt unchanged if palace is unavailable or yields no hits.
func (l *chatLoop) enrichWithPalace(ctx context.Context, prompt string) string {
	if l.palace == nil {
		return prompt
	}
	results, err := l.palace.Search(ctx, prompt, 3)
	if err != nil || len(results) == 0 {
		return prompt
	}
	var sb strings.Builder
	sb.WriteString("[Project memory:\n")
	for _, r := range results {
		summary := r.FactSummary
		if summary == "" {
			summary = r.Content
		}
		if len(summary) > 200 {
			summary = summary[:200] + "…"
		}
		fmt.Fprintf(&sb, "- %s/%s: %s\n", r.Wing, r.Room, summary)
	}
	sb.WriteString("]\n\n")
	sb.WriteString(prompt)
	return sb.String()
}

// handlePrompt sends a typed line to the active runner. From the
// landing zone (no agent active), prints a hint instead of dispatching.
// Records the user turn in turnLog so /switch can hand the briefing to
// the next runner.
func (l *chatLoop) handlePrompt(prompt string) {
	if l.sess == nil {
		fmt.Fprintln(l.errw, "✗ no client picked yet — type /1 (claude), /2 (codex), /4 (minimax), /6 (local) etc, or /help for the full list")
		return
	}
	l.appendTurn(chatTurn{Role: "user", Text: prompt})
	enriched := l.enrichWithPalace(context.Background(), prompt)
	if err := l.sess.send(enriched); err != nil {
		fmt.Fprintln(l.errw, "✗ send: "+err.Error())
		return
	}
	// We don't block here — the response streams async. The next
	// readline cycle starts right after, but the user typically waits
	// for the response visually before typing the next prompt.
}

// appendTurn pushes a turn onto the rolling log. Caps at chatTurnLogCap;
// older entries fall off the front. Safe under the streaming goroutine.
func (l *chatLoop) appendTurn(t chatTurn) {
	l.turnMu.Lock()
	defer l.turnMu.Unlock()
	l.turnLog = append(l.turnLog, t)
	if over := len(l.turnLog) - chatTurnLogCap; over > 0 {
		l.turnLog = l.turnLog[over:]
	}
}

// snapshotTurns returns a defensive copy of the current turn log.
func (l *chatLoop) snapshotTurns() []chatTurn {
	l.turnMu.Lock()
	defer l.turnMu.Unlock()
	out := make([]chatTurn, len(l.turnLog))
	copy(out, l.turnLog)
	return out
}

// printAgents queries the daemon's agent.list and prints a numbered
// table with auth marks. The leading column is /N (the chat numeric
// shortcut) so the user can pick by number directly from the table.
func (l *chatLoop) printAgents() {
	statuses := l.fetchAgentStatuses()
	for i, name := range chatSwitchableAgents {
		s := statuses[name]
		current := "  "
		if l.sess != nil && l.sess.agentID == name {
			current = "● "
		}
		fmt.Fprintf(l.out, "%s/%d  %-10s %s  %s\n", current, i+1, name, s.mark, s.model)
	}
}

// agentStatus is the per-runner row for the landing zone / /agents output.
type agentStatus struct {
	mark  string // ✓ / ✗ / ?
	model string
}

// fetchAgentStatuses queries agent.list and returns a map keyed by
// runner name, falling back to "?" / "" if the call fails so the
// landing zone always renders something rather than blocking on the
// daemon.
func (l *chatLoop) fetchAgentStatuses() map[string]agentStatus {
	out := map[string]agentStatus{}
	for _, name := range chatSwitchableAgents {
		out[name] = agentStatus{mark: "?", model: ""}
	}
	if l.client == nil {
		return out
	}
	var resp struct {
		Agents []struct {
			ID         string `json:"id"`
			Available  bool   `json:"available"`
			AuthStatus string `json:"auth_status"`
			Model      string `json:"model"`
		} `json:"agents"`
	}
	if err := l.client.Call("agent.list", nil, &resp); err != nil {
		return out
	}
	for _, a := range resp.Agents {
		mark := "✗"
		switch a.AuthStatus {
		case "ok":
			mark = "✓"
		case "unknown":
			mark = "?"
		}
		out[a.ID] = agentStatus{mark: mark, model: a.Model}
	}
	return out
}

// agentColor returns a 256-colour ANSI escape for a runner name.
// Each runner has a stable identity colour so they're visually distinct
// in the landing zone and in the prompt header.
func agentColor(name string) string {
	switch name {
	case "claude":
		return "\033[97m" // pearl white (bright white)
	case "codex":
		return "\033[38;5;214m" // amber
	case "copilot":
		return "\033[38;5;69m" // cornflower blue
	case "minimax":
		return "\033[38;5;141m" // soft purple
	case "gemini":
		return "\033[38;5;208m" // orange
	case "local":
		return "\033[38;5;160m" // red
	case "pool":
		return "\033[38;5;117m" // light blue
	}
	return ""
}

// printLanding is the chat-startup banner: header + dynamic daemon /
// agent state + a curated slash command map. Mirrors what the user
// would have seen as the REPL welcome — but every command listed here
// works directly in this same chat (numeric or named runner switch,
// local-bootstrap, opsx, !cmd shell, /help, /exit).
func (l *chatLoop) printLanding() {
	fmt.Fprintln(l.out, "milliways "+welcomeVersion()+" — chat")
	fmt.Fprintln(l.out)

	state := probeDaemonForWelcome(700 * time.Millisecond)
	fmt.Fprintln(l.out, "  daemon  "+state.daemonLine)
	fmt.Fprintln(l.out)

	fmt.Fprintln(l.out, "Pick a client:")
	statuses := l.fetchAgentStatuses()
	for i, name := range chatSwitchableAgents {
		s := statuses[name]
		model := s.model
		if model == "" {
			model = "—"
		}
		color := agentColor(name)
		reset := "\033[0m"
		fmt.Fprintf(l.out, "  /%d  %s/%-10s%s %s  %s\n", i+1, color, name, reset, s.mark, model)
	}
	fmt.Fprintln(l.out)

	fmt.Fprintln(l.out, "  /login [client]   auth setup    /help  all commands   /exit  quit")
	fmt.Fprintln(l.out)
}

// printQuota queries the daemon's quota.get.
func (l *chatLoop) printQuota() {
	if l.client == nil {
		fmt.Fprintln(l.errw, "✗ daemon not connected")
		return
	}
	var resp any
	if err := l.client.Call("quota.get", nil, &resp); err != nil {
		fmt.Fprintln(l.errw, "✗ quota.get: "+err.Error())
		return
	}
	enc := json.NewEncoder(l.out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}

// printHelp shows the full command reference. Kept separate from
// printLanding so the startup banner stays minimal.
func (l *chatLoop) printHelp() {
	l.printLanding() // client list + daemon status

	fmt.Fprintln(l.out, "Client install:")
	fmt.Fprintln(l.out, "  /install <client>             claude | codex | copilot | gemini | local")
	fmt.Fprintln(l.out, "  /install                      list supported install routes")
	fmt.Fprintln(l.out)

	fmt.Fprintln(l.out, "Local-model bootstrap:")
	fmt.Fprintln(l.out, "  /install-local-server         install llama.cpp + default coder model")
	fmt.Fprintln(l.out, "  /install-local-swap           install llama-swap (hot model swap)")
	fmt.Fprintln(l.out, "  /list-local-models            show models the active backend serves")
	fmt.Fprintln(l.out, "  /switch-local-server <kind>   llama-server | llama-swap | ollama | vllm | lmstudio")
	fmt.Fprintln(l.out, "  /download-local-model <repo>  fetch a GGUF from HuggingFace")
	fmt.Fprintln(l.out, "  /setup-local-model <repo>     download + register in llama-swap.yaml")
	fmt.Fprintln(l.out)

	fmt.Fprintln(l.out, "OpenSpec:")
	fmt.Fprintln(l.out, "  /opsx-list                    list openspec changes")
	fmt.Fprintln(l.out, "  /opsx-status <change>         show change progress")
	fmt.Fprintln(l.out, "  /opsx-show <change>           show full change detail")
	fmt.Fprintln(l.out)

	fmt.Fprintln(l.out, "Session:")
	fmt.Fprintln(l.out, "  /model                        list models for active runner + switch instructions")
	fmt.Fprintln(l.out, "  /model <name>                 switch model live (minimax / local only)")
	fmt.Fprintln(l.out, "  /agents                       list clients with live auth status")
	fmt.Fprintln(l.out, "  /quota                        current quota snapshot")
	fmt.Fprintln(l.out, "  /switch <runner>              same as /<runner>")
	fmt.Fprintln(l.out, "  /login [client]               auth setup — API key prompt or CLI steps")
	fmt.Fprintln(l.out, "  /exit                         exit (Ctrl+D also works)")
	fmt.Fprintln(l.out, "  !<cmd>                        run a shell command inline")
	fmt.Fprintln(l.out)
}

// modelSpec describes a runner's model configuration.
type modelSpec struct {
	envKey   string   // env var the daemon reads per-request
	current  string   // default when envKey is unset
	endpoint string   // live endpoint (or CLI name)
	choices  []string // known model names for this runner
}

// runnerModelSpec returns the full model spec for a runner.
func runnerModelSpec(agentID string) modelSpec {
	switch agentID {
	case "minimax":
		cur := os.Getenv("MINIMAX_MODEL")
		if cur == "" {
			cur = "MiniMax-M2.7"
		}
		ep := os.Getenv("MINIMAX_API_URL")
		if ep == "" {
			ep = "https://api.minimax.io/v1"
		}
		return modelSpec{
			envKey:  "MINIMAX_MODEL",
			current: cur,
			endpoint: ep,
			choices: []string{
				"MiniMax-M2.7",
				"MiniMax-Text-01",
				"abab6.5s-chat",
				"abab6.5g-chat",
				"abab5.5-chat",
			},
		}
	case "local":
		cur := os.Getenv("MILLIWAYS_LOCAL_MODEL")
		if cur == "" {
			cur = "qwen2.5-coder-1.5b"
		}
		ep := os.Getenv("MILLIWAYS_LOCAL_ENDPOINT")
		if ep == "" {
			ep = "http://localhost:8765/v1"
		}
		return modelSpec{
			envKey:   "MILLIWAYS_LOCAL_MODEL",
			current:  cur,
			endpoint: ep,
			choices:  []string{"(use /list-local-models to see what's loaded)"},
		}
	case "claude":
		cur := os.Getenv("ANTHROPIC_MODEL")
		if cur == "" {
			cur = os.Getenv("CLAUDE_MODEL")
		}
		if cur == "" {
			cur = "claude-opus-4-5"
		}
		return modelSpec{
			envKey:   "ANTHROPIC_MODEL",
			current:  cur,
			endpoint: "claude CLI",
			choices: []string{
				"claude-opus-4-5",
				"claude-opus-4-7",
				"claude-sonnet-4-6",
				"claude-haiku-4-5-20251001",
				"claude-3-5-sonnet-20241022",
				"claude-3-5-haiku-20241022",
			},
		}
	case "codex":
		cur := os.Getenv("CODEX_MODEL")
		if cur == "" {
			cur = os.Getenv("OPENAI_MODEL")
		}
		if cur == "" {
			cur = "o4-mini"
		}
		return modelSpec{
			envKey:   "CODEX_MODEL",
			current:  cur,
			endpoint: "codex CLI",
			choices: []string{
				"o4-mini",
				"o3",
				"o3-mini",
				"gpt-4.1",
				"gpt-4.1-mini",
				"gpt-4o",
			},
		}
	case "copilot":
		return modelSpec{
			current:  "copilot (gh CLI managed)",
			endpoint: "gh copilot CLI",
			choices:  []string{"(model selection managed by GitHub Copilot CLI — use `gh copilot` flags)"},
		}
	case "gemini":
		cur := os.Getenv("GEMINI_MODEL")
		if cur == "" {
			cur = os.Getenv("GOOGLE_MODEL")
		}
		if cur == "" {
			cur = "gemini-2.5-pro"
		}
		return modelSpec{
			envKey:   "GEMINI_MODEL",
			current:  cur,
			endpoint: "gemini CLI",
			choices: []string{
				"gemini-2.5-pro",
				"gemini-2.5-flash",
				"gemini-2.0-flash",
				"gemini-2.0-flash-lite",
				"gemini-1.5-pro",
				"gemini-1.5-flash",
			},
		}
	case "pool":
		return modelSpec{current: "routes across all runners", endpoint: "internal"}
	}
	return modelSpec{current: "unknown", endpoint: "unknown"}
}

// runnerModelInfo is a convenience wrapper returning (model, endpoint).
func runnerModelInfo(agentID string) (model, endpoint string) {
	s := runnerModelSpec(agentID)
	return s.current, s.endpoint
}

// printModel shows the active model, endpoint, and switchable choices.
// With no agentID (landing zone) it shows a summary table.
func (l *chatLoop) printModel(agentID string) {
	if agentID == "" {
		fmt.Fprintln(l.out, "Active models:")
		for _, name := range chatSwitchableAgents {
			s := runnerModelSpec(name)
			color := agentColor(name)
			reset := "\033[0m"
			fmt.Fprintf(l.out, "  %s%-8s%s  %s\n", color, name, reset, s.current)
		}
		fmt.Fprintln(l.out, "Switch into a runner first, then /model to list its models.")
		return
	}

	s := runnerModelSpec(agentID)
	color := agentColor(agentID)
	reset := "\033[0m"
	fmt.Fprintf(l.out, "%s%s%s  current: %s\n", color, agentID, reset, s.current)
	fmt.Fprintf(l.out, "          endpoint: %s\n", s.endpoint)
	if len(s.choices) > 0 {
		fmt.Fprintln(l.out, "  available models:")
		for _, c := range s.choices {
			marker := "  "
			if c == s.current {
				marker = "▶ "
			}
			fmt.Fprintf(l.out, "    %s%s\n", marker, c)
		}
		if s.envKey != "" {
			fmt.Fprintf(l.out, "  /model <name>  to switch\n")
		}
	}
}

// setModel switches the active model for the current runner by injecting
// the runner's model env var into the daemon live via config.setenv.
// The daemon reads the env var per-request so the next prompt uses it.
func (l *chatLoop) setModel(agentID, newModel string) {
	if agentID == "" {
		fmt.Fprintln(l.errw, "✗ switch to a runner first (e.g. /minimax), then /model <name>")
		return
	}
	s := runnerModelSpec(agentID)
	if s.envKey == "" {
		fmt.Fprintf(l.errw, "✗ %s model is managed externally — no env var to switch\n", agentID)
		return
	}
	if l.client == nil {
		fmt.Fprintln(l.errw, "✗ daemon not connected")
		return
	}
	var result map[string]any
	if err := l.client.Call("config.setenv", map[string]any{
		"key":   s.envKey,
		"value": newModel,
	}, &result); err != nil {
		fmt.Fprintln(l.errw, "✗ could not set model: "+err.Error())
		return
	}
	color := agentColor(agentID)
	reset := "\033[0m"
	fmt.Fprintf(l.out, "✓ %s%s%s model → %s  (next prompt uses it)\n", color, agentID, reset, newModel)
}

// loginSpec describes how a runner is authenticated.
type loginSpec struct {
	// envKey is set for runners that authenticate via an API key.
	// An interactive key prompt is shown and the key is injected live
	// into the daemon via config.setenv (no restart needed).
	envKey string
	// cliSteps lists manual steps for CLI-auth runners (claude, codex,
	// copilot, gemini) that handle auth in their own OAuth/browser flow.
	cliSteps []string
}

var loginSpecs = map[string]loginSpec{
	"claude":  {cliSteps: []string{"run `claude` once to authenticate (browser flow)"}},
	"codex":   {cliSteps: []string{"run `codex login` (browser flow) or set OPENAI_API_KEY"}},
	"copilot": {cliSteps: []string{"run `gh auth login` (browser flow)"}},
	"gemini":  {cliSteps: []string{"run `gemini auth login` (browser flow) or set GEMINI_API_KEY"}},
	"minimax": {envKey: "MINIMAX_API_KEY"},
	"local":   {cliSteps: []string{"run /install-local-server, or set MILLIWAYS_LOCAL_ENDPOINT"}},
}

// printLogin handles /login [agent]. For API-key runners it prompts
// interactively and injects the key live into the daemon (no restart).
// For CLI-auth runners it prints the manual steps.
func (l *chatLoop) printLogin(agent string) {
	if agent == "" {
		// No agent specified — list all.
		fmt.Fprintln(l.out, "Auth setup per runner (use /login <runner> to configure):")
		for _, name := range chatSwitchableAgents {
			spec, ok := loginSpecs[name]
			if !ok {
				continue
			}
			if spec.envKey != "" {
				fmt.Fprintf(l.out, "  %-8s  → /login %s  (API key prompt)\n", name, name)
			} else {
				fmt.Fprintf(l.out, "  %-8s  → %s\n", name, spec.cliSteps[0])
			}
		}
		return
	}

	spec, ok := loginSpecs[agent]
	if !ok {
		fmt.Fprintf(l.errw, "no auth info for %q\n", agent)
		return
	}

	// CLI-auth runner — can't prompt interactively, show steps.
	if spec.envKey == "" {
		fmt.Fprintf(l.out, "%s auth:\n", agent)
		for _, s := range spec.cliSteps {
			fmt.Fprintf(l.out, "  → %s\n", s)
		}
		fmt.Fprintln(l.out, "  After authenticating, the runner will work immediately.")
		return
	}

	// API-key runner — prompt interactively then inject live via RPC.
	// Use term.ReadPassword on the raw stdin fd rather than readline so
	// we don't recurse into the readline event loop (which hangs).
	fmt.Fprintf(l.out, "%s API key: ", agent)
	key, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(l.out) // newline after the hidden input
	if err != nil || strings.TrimSpace(string(key)) == "" {
		fmt.Fprintln(l.errw, "✗ cancelled")
		return
	}
	trimmed := strings.TrimSpace(string(key))

	var result map[string]any
	if err := l.client.Call("config.setenv", map[string]any{
		"key":   spec.envKey,
		"value": trimmed,
	}, &result); err != nil {
		fmt.Fprintln(l.errw, "✗ could not set key in daemon: "+err.Error())
		fmt.Fprintf(l.errw, "  Fallback: export %s=<key> and restart the daemon.\n", spec.envKey)
		return
	}
	fmt.Fprintf(l.out, "✓ %s set — try /%s now\n", spec.envKey, agent)
}
