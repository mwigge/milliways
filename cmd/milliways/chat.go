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
	"github.com/mwigge/milliways/internal/rpc"
	"github.com/spf13/cobra"
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
// The empty-string case is the landing-zone prompt (no client picked yet).
func chatPrompt(agentID string) string {
	if agentID == "" {
		return "[no client — pick one with /1../7 or /<name>] ▶ "
	}
	return "[" + agentID + "] ▶ "
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
type chatLoop struct {
	client *rpc.Client
	sess   *chatSession
	rl     *readline.Instance
	out    io.Writer
	errw   io.Writer
}

func (l *chatLoop) run(ctx context.Context) error {
	// Drain the agent stream in a goroutine; deltas land on stdout
	// immediately, terminal events (chunk_end / err / end) clear the
	// busy flag so the next prompt can be issued.
	go l.drainStream()

	defer func() {
		_ = l.sess.close()
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
				}
			}
		case "chunk_end":
			fmt.Fprintln(l.out)
			l.sess.busyMu.Lock()
			l.sess.busy = false
			l.sess.busyMu.Unlock()
			l.refreshPromptHint(ev)
			// Refresh the readline prompt so the user sees ▶ ready to type.
			l.rl.Refresh()
		case "err":
			msg, _ := ev["msg"].(string)
			fmt.Fprintln(l.errw, "✗ "+msg)
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
func (l *chatLoop) switchAgent(newID string) {
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

	if l.sess != nil {
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
	fmt.Fprintln(l.out, "→ "+newID)
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

// handlePrompt sends a typed line to the active runner. From the
// landing zone (no agent active), prints a hint instead of dispatching.
func (l *chatLoop) handlePrompt(prompt string) {
	if l.sess == nil {
		fmt.Fprintln(l.errw, "✗ no client picked yet — type /1 (claude), /2 (codex), /4 (minimax), /6 (local) etc, or /help for the full list")
		return
	}
	if err := l.sess.send(prompt); err != nil {
		fmt.Fprintln(l.errw, "✗ send: "+err.Error())
		return
	}
	// We don't block here — the response streams async. The next
	// readline cycle starts right after, but the user typically waits
	// for the response visually before typing the next prompt.
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
		fmt.Fprintf(l.out, "  /%d  /%-10s %s  %s\n", i+1, name, s.mark, model)
	}
	fmt.Fprintln(l.out)

	fmt.Fprintln(l.out, "Local-model bootstrap (no leaving chat):")
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

	fmt.Fprintln(l.out, "Other:")
	fmt.Fprintln(l.out, "  /agents       list clients with auth status")
	fmt.Fprintln(l.out, "  /quota        current quota snapshot")
	fmt.Fprintln(l.out, "  /switch <n>   same as /<runner>")
	fmt.Fprintln(l.out, "  /help         all slash commands")
	fmt.Fprintln(l.out, "  /exit         exit chat (Ctrl+D also works)")
	fmt.Fprintln(l.out, "  !<cmd>        run shell command (vim, less, htop work)")
	fmt.Fprintln(l.out)
	fmt.Fprintln(l.out, "Type a slash command to enter a client; once inside, anything you type goes to that runner.")
	fmt.Fprintln(l.out)
}

// printQuota queries the daemon's quota.get.
func (l *chatLoop) printQuota() {
	var resp any
	if err := l.client.Call("quota.get", nil, &resp); err != nil {
		fmt.Fprintln(l.errw, "✗ quota.get: "+err.Error())
		return
	}
	enc := json.NewEncoder(l.out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}

// printHelp re-runs the landing-zone banner so /help and the startup
// banner stay in sync. (Single source of truth = printLanding.)
func (l *chatLoop) printHelp() { l.printLanding() }
