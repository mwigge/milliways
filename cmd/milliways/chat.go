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

// chatDefaultAgent is the runner the chat loop opens on first start.
// Future work: persist the user's last choice in
// $XDG_STATE_HOME/milliways/active.
const chatDefaultAgent = "claude"

// chatSwitchableAgents is the set of runner IDs the user can switch to
// via the /<name> shorthand or /switch <name>. Mirrors the daemon's
// dispatch table in internal/daemon/agents.go.
var chatSwitchableAgents = []string{
	"claude", "codex", "copilot", "gemini", "local", "minimax", "pool",
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

	sess, err := openAgentForChat(client, chatDefaultAgent)
	if err != nil {
		return err
	}

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          chatPrompt(sess.agentID),
		HistoryFile:     chatHistoryFile(),
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		_ = sess.close()
		return fmt.Errorf("readline init: %w", err)
	}
	defer rl.Close()

	loop := &chatLoop{
		client: client,
		sess:   sess,
		rl:     rl,
		out:    os.Stdout,
		errw:   os.Stderr,
	}
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
func chatPrompt(agentID string) string { return "[" + agentID + "] ▶ " }

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

	// Switch shorthand: /<runner>
	for _, name := range chatSwitchableAgents {
		if verb == name {
			l.switchAgent(name)
			return
		}
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
		// Close session + force readline to return EOF on next read.
		_ = l.sess.close()
		_ = l.rl.Close()
		// Re-init the readline so the next Readline() returns EOF.
		// Simpler: just force-close the underlying terminal.
		os.Exit(0)
	case "":
		// Bare "/" — show help.
		l.printHelp()
	default:
		fmt.Fprintln(l.errw, "✗ unknown slash command: /"+verb+"  (try /help)")
	}
}

// switchAgent closes the current session and opens a new one for newID.
func (l *chatLoop) switchAgent(newID string) {
	if newID == l.sess.agentID {
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

	if err := l.sess.close(); err != nil {
		fmt.Fprintln(l.errw, "warn: closing previous session: "+err.Error())
	}
	<-l.sess.done

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

// handlePrompt sends a typed line to the active runner.
func (l *chatLoop) handlePrompt(prompt string) {
	if err := l.sess.send(prompt); err != nil {
		fmt.Fprintln(l.errw, "✗ send: "+err.Error())
		return
	}
	// We don't block here — the response streams async. The next
	// readline cycle starts right after, but the user typically waits
	// for the response visually before typing the next prompt.
}

// printAgents queries the daemon's agent.list and prints names + auth.
func (l *chatLoop) printAgents() {
	var resp struct {
		Agents []struct {
			ID         string `json:"id"`
			Available  bool   `json:"available"`
			AuthStatus string `json:"auth_status"`
			Model      string `json:"model"`
		} `json:"agents"`
	}
	if err := l.client.Call("agent.list", nil, &resp); err != nil {
		fmt.Fprintln(l.errw, "✗ agent.list: "+err.Error())
		return
	}
	for _, a := range resp.Agents {
		mark := "✗"
		switch a.AuthStatus {
		case "ok":
			mark = "✓"
		case "unknown":
			mark = "?"
		}
		current := "  "
		if a.ID == l.sess.agentID {
			current = "● "
		}
		fmt.Fprintf(l.out, "%s%-10s %s  %s\n", current, a.ID, mark, a.Model)
	}
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

// printHelp lists slash commands + the ! escape.
func (l *chatLoop) printHelp() {
	const help = `Slash commands (chat):
  /<runner>          switch active runner — claude / codex / copilot / gemini / local / minimax / pool
  /switch <runner>   same
  /agents            list runners with auth status
  /quota             current quota snapshot
  /help              this list
  /exit  /quit       exit chat (Ctrl+D also works)

Shell escape:
  !<command>         run <command> via $SHELL -c "..."  (interactive tools work)

Anything else you type is sent to the active runner as a prompt.
`
	fmt.Fprint(l.out, help)
}
