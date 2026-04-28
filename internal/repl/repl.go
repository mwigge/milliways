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

package repl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/substrate"
)

// renderQuotaBar renders a compact quota bar: "5h:████░░ 50%" or "5h:12" (no limit).
func renderQuotaBar(label string, period *QuotaPeriod, scheme ColorScheme) string {
	if period == nil || (period.Used == 0 && period.Limit == 0) {
		return ""
	}
	if period.Limit <= 0 {
		return AccentColorText(scheme, fmt.Sprintf("%s:%d", label, period.Used))
	}
	const width = 8
	filled := int(period.Ratio * float64(width))
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	pct := int(period.Ratio * 100)
	return AccentColorText(scheme, fmt.Sprintf("%s:%s %d%%", label, bar, pct))
}

// renderSessionUsage renders a compact session cost/token summary.
func renderSessionUsage(session *SessionUsage, scheme ColorScheme) string {
	if session == nil || session.Dispatches == 0 {
		return ""
	}
	var parts []string
	if session.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", session.CostUSD))
	}
	if session.InputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%s↑", compactTokens(session.InputTokens)))
	}
	if session.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%s↓", compactTokens(session.OutputTokens)))
	}
	if len(parts) == 0 {
		return ""
	}
	return AccentColorText(scheme, strings.Join(parts, " "))
}

// compactTokens formats a token count as a short string (e.g. 1200 -> "1.2k").
func compactTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

type InputKind string

const (
	InputPrompt  InputKind = "prompt"
	InputCommand InputKind = "command"
	InputShell   InputKind = "shell"
)

type Input struct {
	Kind    InputKind
	Raw     string
	Command string
	Content string
}

func ParseInput(input string) Input {
	raw := input
	trimmed := strings.TrimSpace(input)
	parsed := Input{
		Kind:    InputPrompt,
		Raw:     raw,
		Content: trimmed,
	}

	if len(trimmed) <= 1 {
		if trimmed != "" {
			parsed.Content = trimmed
		}
		return parsed
	}

	if strings.HasPrefix(trimmed, "/") {
		body := strings.TrimSpace(trimmed[1:])
		if body == "" {
			return parsed
		}

		command, content := splitHead(body)
		if content == "" {
			if command == "claude" || command == "codex" || command == "minimax" || command == "copilot" {
				content = command
			}
		}
		return Input{
			Kind:    InputCommand,
			Raw:     raw,
			Command: command,
			Content: content,
		}
	}

	if strings.HasPrefix(trimmed, "!") {
		body := strings.TrimSpace(trimmed[1:])
		if body == "" {
			return parsed
		}

		return Input{
			Kind:    InputShell,
			Raw:     raw,
			Content: body,
		}
	}

	return parsed
}

func splitHead(input string) (string, string) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return "", ""
	}

	command := parts[0]
	if len(parts) == 1 {
		return command, ""
	}

	return command, strings.Join(parts[1:], " ")
}

type RunnerState struct {
	mu       sync.RWMutex
	running  bool
	complete chan struct{}
	cancel   context.CancelFunc
}

func (s *RunnerState) SetRunning(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = true
	s.complete = make(chan struct{})
	s.cancel = cancel
}

func (s *RunnerState) SetDone() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.complete != nil {
		close(s.complete)
	}
	s.running = false
	s.cancel = nil
}

func (s *RunnerState) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *RunnerState) Cancel() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cancel != nil {
		s.cancel()
	}
}

type REPL struct {
	input              InputLine
	runner             Runner
	runners            map[string]Runner
	prev               Runner
	history            []string
	stdout             io.Writer
	runnerState        RunnerState
	substrate          *substrate.Client
	session            *replSession
	getQuota           func(name string) (*QuotaInfo, error)
	currentChange      string
	scheme             ColorScheme
	version            string
	turnBuffer         []ConversationTurn
	rules              string
	logHandler         *ReplLogHandler
	sessionStore       *SessionStore
	noRestore          bool
	shellBuf           *ShellOutputBuffer
	pendingAttachments []Attachment
	lastCtrlC          time.Time // for double-Ctrl-C exit detection
	statusBar          *StatusBar
	ring               *RingConfig // nil = no ring configured
}

func (r *REPL) SetVersion(v string) {
	r.version = v
}

// SetStatusBar wires a persistent status bar to the terminal.
// When set, renderStatusBar delegates to the bar instead of printing inline.
func (r *REPL) SetStatusBar(sb *StatusBar) { r.statusBar = sb }

// SetLogHandler wires up the in-memory log buffer so /logs can read entries.
func (r *REPL) SetLogHandler(h *ReplLogHandler) { r.logHandler = h }

func (r *REPL) loadRules() {
	paths := rulesSearchPaths()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			r.rules = string(data)
			slog.Info("rules loaded", "path", p, "bytes", len(data))
			return
		}
	}
	slog.Warn("rules file not found", "tried", paths)
	r.rules = ""
}

func rulesSearchPaths() []string {
	var paths []string
	// Allow explicit override: colon/semi-colon separated list
	if v := os.Getenv("MILLIWAYS_RULES_PATHS"); v != "" {
		parts := strings.Split(v, string(os.PathListSeparator))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				paths = append(paths, p)
			}
		}
		return paths
	}
	if env := os.Getenv("AI_LOCAL"); env != "" {
		paths = append(paths, filepath.Join(env, "CLAUDE.md"))
	}
	// Prefer XDG-style config location
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "milliways", "CLAUDE.md"))
	}
	// Optional developer fallback behind explicit flag
	if os.Getenv("MILLIWAYS_DEV_FALLBACK") != "" {
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, filepath.Join(home, "dev", "src", "ai_local", "CLAUDE.md"))
		}
	}
	return paths
}

type replSession struct {
	conversationID string
	runnerName     string
	outputBuf      *bytes.Buffer
}

func NewREPL(stdout io.Writer) *REPL {
	r := &REPL{
		input:    newDefaultInput(),
		runners:  make(map[string]Runner),
		stdout:   stdout,
		scheme:   DefaultScheme(),
		shellBuf: NewShellOutputBuffer(32 * 1024),
	}

	store, err := NewSessionStore()
	if err != nil {
		slog.Warn("session store unavailable", "err", err)
	} else {
		r.sessionStore = store
	}

	return r
}

// SetNoRestore disables auto-restore of the last session on startup when v is true.
func (r *REPL) SetNoRestore(v bool) {
	r.noRestore = v
}

func NewREPLWithSubstrate(stdout io.Writer, sc *substrate.Client) *REPL {
	r := NewREPL(stdout)
	r.substrate = sc
	return r
}

func NewREPLWithQuotaFunc(stdout io.Writer, qf func(name string) (*QuotaInfo, error)) *REPL {
	r := NewREPL(stdout)
	r.getQuota = qf
	return r
}

func (r *REPL) SetScheme(scheme ColorScheme) {
	r.scheme = scheme
}

func (r *REPL) SetQuotaFunc(qf func(name string) (*QuotaInfo, error)) {
	r.getQuota = qf
}

func (r *REPL) Register(name string, runner Runner) {
	r.runners[name] = runner
}

func (r *REPL) SetRunner(name string) error {
	runner, ok := r.runners[name]
	if !ok {
		return fmt.Errorf("unknown runner: %s", name)
	}
	r.prev = r.runner
	r.runner = runner
	r.scheme = SchemeForRunner(name)
	return nil
}

func (r *REPL) CurrentRunner() Runner {
	return r.runner
}

func (r *REPL) Run(ctx context.Context) error {
	defer r.input.Close()

	if r.substrate != nil {
		if err := r.substrate.Ping(ctx); err == nil {
			if err := r.startConversation(ctx); err != nil {
				r.println(MutedText(fmt.Sprintf("session: could not start: %v", err)))
			}
		} else {
			r.println(MutedText("session: mempalace unavailable (continuing without persistence)"))
			r.substrate = nil
		}
	}

	if r.session != nil && r.runner != nil {
		r.session.runnerName = r.runner.Name()
	}

	r.loadRules()

	if r.sessionStore != nil && !r.noRestore {
		cwd, _ := os.Getwd()
		if sess, ok := r.sessionStore.FindLatestForCwd(cwd); ok {
			r.turnBuffer = sess.Turns
			r.ring = sess.Ring
			fmt.Fprintf(r.stdout, "restored %d turns from %s\n",
				len(sess.Turns), sess.SavedAt.Format("2006-01-02 15:04"))
			r.replayTurnsToSubstrate(ctx)
		}
	}

	defer func() {
		r.println(ResetColor + BlackBackground)
		if r.substrate != nil && r.session != nil {
			_ = r.substrate.ConversationEnd(ctx, substrate.EndRequest{
				ConversationID: r.session.conversationID,
				Status:         "done",
				Reason:         "terminal_exit",
			})
		}
		if r.sessionStore != nil {
			cwd, _ := os.Getwd()
			runnerName := ""
			if r.runner != nil {
				runnerName = r.runner.Name()
			}
			sess := PersistedSession{
				Version:    sessionVersion,
				SavedAt:    time.Now(),
				RunnerName: runnerName,
				RulesHash:  rulesHash(r.rules),
				WorkDir:    cwd,
				Turns:      r.turnBuffer,
				Ring:       r.ring,
			}
			if err := r.sessionStore.Save("", sess); err != nil {
				slog.Warn("auto-save session failed", "err", err)
			}
		}
	}()

	r.input.SetCompleter(func(line string) []string {
		var completions []string
		if strings.HasPrefix(line, "/") {
			// Model-ID completion for runner-specific model commands.
			if ids, partial, prefix := modelCompletionContext(r, line); ids != nil {
				for _, id := range ids {
					if strings.HasPrefix(id, partial) {
						completions = append(completions, prefix+id)
					}
				}
				if len(completions) > 0 {
					return completions
				}
			}

			// Default: complete command names.
			cmdPrefix := strings.TrimPrefix(line, "/")
			for cmd := range commandHandlers {
				if strings.HasPrefix(cmd, cmdPrefix) {
					completions = append(completions, "/"+cmd)
				}
			}
		} else {
			for name := range r.runners {
				if strings.HasPrefix(name, line) {
					completions = append(completions, name)
				}
			}
		}
		return completions
	})

	fmt.Fprint(r.stdout, "\x1b[2J\x1b[H]")
	header := " milliways "
	if r.version != "" {
		header = fmt.Sprintf(" milliways %s ", r.version)
	}
	r.println(PhosphorHeader(header))
	r.println(PhosphorText("  type /help for commands"))
	if r.session != nil {
		r.println(PhosphorText(fmt.Sprintf("  session: %s", r.session.conversationID)))
	} else {
		r.println(PhosphorText("  no session"))
	}

	var runnerNames []string
	for name := range r.runners {
		runnerNames = append(runnerNames, name)
	}
	sort.Strings(runnerNames)
	var coloredRunners []string
	for _, name := range runnerNames {
		scheme := SchemeForRunner(name)
		coloredRunners = append(coloredRunners, BlackBackground+scheme.FG+name+ResetColor)
	}
	r.println("  " + PhosphorText("runners: ") + strings.Join(coloredRunners, PhosphorText(" | ")))
	r.println("")
	r.println("")

	r.renderStatusBar(ctx)
	r.println("")

	for {
		line, err := r.input.Prompt("▶ ")
		if err != nil {
			if err == ErrInputAborted {
				if r.runnerState.IsRunning() {
					r.runnerState.Cancel()
					r.println(MutedText("^C  interrupted"))
					r.runnerState.SetDone()
					continue
				}
				// Double Ctrl-C within 1 second exits; first tap shows hint.
				if !r.lastCtrlC.IsZero() && time.Since(r.lastCtrlC) < time.Second {
					r.println("")
					return nil
				}
				r.lastCtrlC = time.Now()
				r.println(MutedText("^C  (ctrl-c again to exit)"))
				continue
			}
			if err == io.EOF {
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		r.input.AppendHistory(line)
		r.history = append(r.history, line)

		input := ParseInput(line)

		var handlerErr error
		switch input.Kind {
		case InputCommand:
			handler, ok := commandHandlers[input.Command]
			if !ok {
				r.println(ErrorText(fmt.Sprintf("unknown command: /%s", input.Command)))
				continue
			}
			if input.Command == "exit" {
				if r.runnerState.IsRunning() {
					r.runnerState.Cancel()
					r.runnerState.SetDone()
				}
				r.println("Goodbye!")
				return nil
			}
			cmdCtx, cmdCancel := context.WithCancel(ctx)
			r.runnerState.SetRunning(cmdCancel)
			handlerErr = handler(cmdCtx, r, input.Content)
			cmdCancel()
			r.runnerState.SetDone()

		case InputShell:
			if recalled, ok := r.recallHistory(input.Content); ok {
				r.println(MutedText("▶ " + recalled))
				ri := ParseInput(recalled)
				recallCtx, recallCancel := context.WithCancel(ctx)
				r.runnerState.SetRunning(recallCancel)
				switch ri.Kind {
				case InputCommand:
					if h, hok := commandHandlers[ri.Command]; hok {
						handlerErr = h(recallCtx, r, ri.Content)
					}
				case InputShell:
					handlerErr = r.handleBash(recallCtx, ri.Content)
				case InputPrompt:
					handlerErr = r.handlePrompt(recallCtx, ri.Content)
				}
				recallCancel()
				r.runnerState.SetDone()
			} else {
				cmdCtx, cmdCancel := context.WithCancel(ctx)
				r.runnerState.SetRunning(cmdCancel)
				handlerErr = r.handleBash(cmdCtx, input.Content)
				cmdCancel()
				r.runnerState.SetDone()
			}

		case InputPrompt:
			handlerErr = r.handlePrompt(ctx, input.Content)
		}

		if handlerErr != nil && handlerErr != io.EOF {
			r.println(ErrorText(fmt.Sprintf("error: %v", handlerErr)))
		}

		r.println("")
		r.renderStatusBar(ctx)
		r.println("")
	}
}

func (r *REPL) startConversation(ctx context.Context) error {
	if r.substrate == nil {
		return fmt.Errorf("no substrate client")
	}

	convID := fmt.Sprintf("milliways-%d", time.Now().Unix())

	repoPath := findGitRoot(cwd())
	prompt := fmt.Sprintf("milliways session started at %s", time.Now().Format(time.RFC3339))
	if repoPath != "" {
		prompt = fmt.Sprintf("milliways session in %s at %s", repoPath, time.Now().Format(time.RFC3339))
	}

	_, err := r.substrate.ConversationStart(ctx, substrate.StartRequest{
		ConversationID: convID,
		BlockID:        "main",
		Prompt:         prompt,
	})
	if err != nil {
		return err
	}

	r.session = &replSession{
		conversationID: convID,
		runnerName:     "",
		outputBuf:      new(bytes.Buffer),
	}
	return nil
}

func (r *REPL) appendUserTurn(ctx context.Context, prompt string) {
	if r.substrate == nil || r.session == nil {
		return
	}
	_ = r.substrate.ConversationAppendTurn(ctx, substrate.AppendTurnRequest{
		ConversationID: r.session.conversationID,
		Turn: conversation.Turn{
			Role:     conversation.RoleUser,
			Provider: "milliways",
			Text:     prompt,
			At:       time.Now(),
		},
	})
}

func (r *REPL) appendAssistantTurn(ctx context.Context, provider, output string) {
	if r.substrate == nil || r.session == nil {
		return
	}
	_ = r.substrate.ConversationAppendTurn(ctx, substrate.AppendTurnRequest{
		ConversationID: r.session.conversationID,
		Turn: conversation.Turn{
			Role:     conversation.RoleAssistant,
			Provider: provider,
			Text:     output,
			At:       time.Now(),
		},
	})
	r.println(MutedText("  [memory saved]"))
}

func (r *REPL) checkpointConversation(ctx context.Context, reason string) {
	if r.substrate == nil || r.session == nil {
		return
	}
	_, _ = r.substrate.ConversationCheckpoint(ctx, substrate.CheckpointRequest{
		ConversationID: r.session.conversationID,
		Reason:         reason,
	})
}

func (r *REPL) handlePrompt(ctx context.Context, prompt string) error {
	if r.runner == nil {
		r.println(WarningText("No runner selected. Use /switch <runner>"))
		return nil
	}

	if r.session != nil {
		r.appendUserTurn(ctx, prompt)
	}

	r.turnBuffer = appendTurn(r.turnBuffer, ConversationTurn{
		Role:   "user",
		Text:   prompt,
		Runner: r.runner.Name(),
		At:     time.Now(),
	})

	var usageBefore *QuotaInfo
	if q, err := r.runner.Quota(); err == nil {
		usageBefore = q
	}

	enriched, _ := ResolveContext(prompt, r.shellBuf)

	req := DispatchRequest{
		Prompt:      enriched.Text,
		Context:     enriched.Fragments,
		History:     historyWithoutLast(r.turnBuffer),
		Rules:       r.rules,
		ClientID:    "repl/" + r.runner.Name(),
		Attachments: r.pendingAttachments,
	}
	r.pendingAttachments = nil

	runCtx, cancel := context.WithCancel(ctx)
	r.runnerState.SetRunning(cancel)
	defer r.runnerState.SetDone()

	// Repaint the persistent status bar while the runner is active so the
	// running indicator (●) stays current. Goroutine exits when runCtx is done.
	if r.statusBar != nil {
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					r.renderStatusBar(runCtx)
				case <-runCtx.Done():
					return
				}
			}
		}()
	}

	outputBuf := new(bytes.Buffer)
	tee := &teeWriter{w: r.stdout, buf: outputBuf, scheme: r.scheme}

	// Capture runner and session by value before the goroutine to avoid a data
	// race if the user issues /switch while dispatch is in flight.
	dispatchRunner := r.runner
	dispatchSession := r.session
	dispatchScheme := r.scheme

	done := make(chan error, 1)
	go func() {
		r.println(fmt.Sprintf("[%s] %s", ColorText(dispatchScheme, dispatchRunner.Name()), ColorText(dispatchScheme, "Thinking...")))
		start := time.Now()
		slog.Info("dispatch start", "runner", dispatchRunner.Name(), "prompt_len", len(prompt), "history_turns", len(req.History), "rules_loaded", req.Rules != "")
		err := dispatchRunner.Execute(runCtx, req, tee)
		tee.Flush()
		dur := time.Since(start)

		if dispatchSession != nil && outputBuf.Len() > 0 {
			r.appendAssistantTurn(ctx, dispatchRunner.Name(), outputBuf.String())
		}

		if err != nil {
			slog.Warn("dispatch error", "runner", dispatchRunner.Name(), "err", err, "duration_ms", dur.Milliseconds())
			if ctx.Err() != nil {
				r.println(fmt.Sprintf("%s %s  %s",
					ColorText(dispatchScheme, "✗"),
					ColorText(dispatchScheme, dispatchRunner.Name()),
					MutedText("interrupted")))
			} else {
				r.println(fmt.Sprintf("%s %s  %.1fs  %v",
					ColorText(dispatchScheme, "✗"),
					ColorText(dispatchScheme, dispatchRunner.Name()),
					dur.Seconds(), err))
			}
			done <- err
			return
		}

		slog.Info("dispatch end", "runner", dispatchRunner.Name(), "duration_ms", dur.Milliseconds())

		if outputBuf.Len() > 0 {
			r.turnBuffer = appendTurn(r.turnBuffer, ConversationTurn{
				Role:   "assistant",
				Text:   outputBuf.String(),
				Runner: dispatchRunner.Name(),
				At:     time.Now(),
			})
		}

		if q, err := dispatchRunner.Quota(); err == nil && q != nil && q.Session != nil {
			var beforeIn, beforeOut int
			var beforeCost float64
			if usageBefore != nil && usageBefore.Session != nil {
				beforeIn = usageBefore.Session.InputTokens
				beforeOut = usageBefore.Session.OutputTokens
				beforeCost = usageBefore.Session.CostUSD
			}
			RecordDispatch(ctx, dispatchRunner.Name(),
				q.Session.CostUSD-beforeCost,
				q.Session.InputTokens-beforeIn,
				q.Session.OutputTokens-beforeOut,
			)
		}

		r.println(fmt.Sprintf("%s %s  %.1fs",
			ColorText(dispatchScheme, "✓"),
			ColorText(dispatchScheme, dispatchRunner.Name()),
			dur.Seconds()))
		done <- nil
	}()

	// Catch SIGINT (Ctrl-C) during dispatch so it cancels the runner rather
	// than terminating the process. The channel is buffered so a rapid second
	// Ctrl-C doesn't block after the first is handled.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT)
	defer signal.Stop(sigCh)

	select {
	case err := <-done:
		return err
	case <-sigCh:
		cancel()
		r.println(MutedText("^C  interrupted"))
		<-done
		return nil
	case <-ctx.Done():
		cancel()
		<-done
		return ctx.Err()
	}
}

func appendTurn(buf []ConversationTurn, t ConversationTurn) []ConversationTurn {
	buf = append(buf, t)
	if len(buf) > MaxHistoryTurns {
		buf = buf[len(buf)-MaxHistoryTurns:]
	}
	return buf
}

func historyWithoutLast(buf []ConversationTurn) []ConversationTurn {
	if len(buf) == 0 {
		return nil
	}
	out := make([]ConversationTurn, len(buf)-1)
	copy(out, buf[:len(buf)-1])
	return out
}

// recallHistory handles !N (line N) and !! (last line) from r.history.
// The current !N/!! entry is already at the end of r.history when this runs.
func (r *REPL) recallHistory(arg string) (string, bool) {
	switch arg {
	case "!", "":
		// !! — recall the command before this one
		if len(r.history) >= 2 {
			return r.history[len(r.history)-2], true
		}
		return "", false
	}
	n, err := strconv.Atoi(arg)
	if err != nil {
		return "", false
	}
	// 1-indexed; exclude the current !N at the tail
	if n >= 1 && n < len(r.history) {
		return r.history[n-1], true
	}
	return "", false
}

func (r *REPL) handleBash(ctx context.Context, cmd string) error {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil
	}

	execCmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	return streamCmdOutput(ctx, execCmd, io.MultiWriter(r.stdout, r.shellBuf))
}

func (r *REPL) println(s string) {
	fmt.Fprintln(r.stdout, s)
}

func (r *REPL) print(s string) {
	fmt.Fprint(r.stdout, s)
}

type streamingWriter struct {
	w       io.Writer
	mu      sync.Mutex
	lineBuf []byte
}

func newStreamingWriter(w io.Writer) *streamingWriter {
	return &streamingWriter{w: w}
}

func (sw *streamingWriter) Write(p []byte) (n int, err error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	for _, b := range p {
		if b == '\n' {
			if len(sw.lineBuf) > 0 {
				if _, werr := sw.w.Write(sw.lineBuf); werr != nil {
					sw.lineBuf = sw.lineBuf[:0]
					return 0, werr
				}
			}
			if _, werr := sw.w.Write([]byte{'\n'}); werr != nil {
				return 0, werr
			}
			sw.lineBuf = sw.lineBuf[:0]
		} else {
			sw.lineBuf = append(sw.lineBuf, b)
		}
	}
	if len(p) > 0 && p[len(p)-1] != '\n' {
		// partial write - flush on next call with more data or explicit flush
	}
	return len(p), nil
}

func (sw *streamingWriter) Flush() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if len(sw.lineBuf) > 0 {
		if _, err := sw.w.Write(sw.lineBuf); err != nil {
			slog.Warn("streamingWriter.Flush write error", "err", err)
		}
		sw.lineBuf = sw.lineBuf[:0]
	}
}

func streamCmdOutput(ctx context.Context, cmd *exec.Cmd, out io.Writer) error {
	// Use io.Pipe so exec.Cmd's internal copy goroutines write to a pipe we
	// control. cmd.Wait() waits for those goroutines before returning, then we
	// close the write end, which signals EOF to the reader. This avoids the
	// "file already closed" race that occurs with StdoutPipe when cmd.Wait()
	// closes the read end while caller goroutines are still in Read.
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pw.CloseWithError(err)
		_ = pr.Close()
		return err
	}

	waitDone := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		// Close the write end so the reader below gets EOF.
		_ = pw.CloseWithError(err)
		waitDone <- err
	}()

	_, _ = io.Copy(out, pr)
	_ = pr.Close()

	return <-waitDone
}

var ansiStripper = regexp.MustCompile(`\x1b\[[0-9;]*m`)

type teeWriter struct {
	w      io.Writer
	buf    *bytes.Buffer
	scheme ColorScheme
	mu     sync.Mutex
	line   []byte
}

func (t *teeWriter) Write(p []byte) (n int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, b := range p {
		if b == '\n' {
			colored := ColorText(t.scheme, string(t.line))
			if _, werr := t.buf.WriteString(string(t.line)); werr != nil {
				return 0, werr
			}
			if _, werr := t.w.Write([]byte(colored + "\n")); werr != nil {
				return 0, werr
			}
			t.line = nil
		} else {
			t.line = append(t.line, b)
		}
	}
	return len(p), nil
}

func (t *teeWriter) Flush() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.line) > 0 {
		colored := ColorText(t.scheme, string(t.line))
		if _, werr := t.buf.WriteString(string(t.line)); werr != nil {
			slog.Warn("teeWriter.Flush buffer write error", "err", werr)
		}
		if _, werr := t.w.Write([]byte(colored)); werr != nil {
			slog.Warn("teeWriter.Flush writer write error", "err", werr)
		}
		t.line = nil
	}
}

// buildStatusContent assembles the colored status string from current terminal
// state. The returned string may contain ANSI escape codes but no trailing
// newline. sessionForTitle is the session usage used for the title bar update;
// it is nil when quota information is unavailable.
func (r *REPL) buildStatusContent(ctx context.Context) (content string, sessionForTitle *SessionUsage) {
	var parts []string

	if r.runner != nil {
		name := r.runner.Name()
		dot := ""
		if r.runnerState.IsRunning() {
			dot = "●"
		}
		runnerSeg := dot + name
		if r.ring != nil {
			runnerSeg += fmt.Sprintf(" %d/%d", r.ring.Pos+1, len(r.ring.Runners))
		}
		parts = append(parts, RunnerAccentText(name, runnerSeg))
	}

	if r.session != nil {
		sid := r.session.conversationID
		if len(sid) > 12 {
			sid = sid[:12]
		}
		parts = append(parts, MutedText(sid))
	}

	if r.substrate != nil {
		statsCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		stats, err := r.substrate.GetPalaceStats(statsCtx)
		cancel()
		if err == nil && stats.TotalDrawers > 0 {
			parts = append(parts, MutedText(fmt.Sprintf("palace:%dd/%dr", stats.TotalDrawers, stats.Rooms)))
		}
	}

	if r.substrate != nil {
		parts = append(parts, MutedText("MCP:✓"))
	}

	// Quota bars and session usage.
	if r.runner != nil && r.getQuota != nil {
		quotaCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		quota, _ := r.getQuota(r.runner.Name())
		cancel()
		_ = quotaCtx

		// Merge session usage from the runner itself.
		if runnerQuota, _ := r.runner.Quota(); runnerQuota != nil && runnerQuota.Session != nil {
			if quota == nil {
				quota = &QuotaInfo{}
			}
			quota.Session = runnerQuota.Session
		}

		if quota != nil {
			scheme := SchemeForRunner(r.runner.Name())
			sessionForTitle = quota.Session
			if s := renderSessionUsage(quota.Session, scheme); s != "" {
				parts = append(parts, s)
			}
			if s := renderQuotaBar("5h", quota.FiveHour, scheme); s != "" {
				parts = append(parts, s)
			}
			if s := renderQuotaBar("d", quota.Day, scheme); s != "" {
				parts = append(parts, s)
			}
			if s := renderQuotaBar("w", quota.Week, scheme); s != "" {
				parts = append(parts, s)
			}
			if s := renderQuotaBar("mo", quota.Month, scheme); s != "" {
				parts = append(parts, s)
			}
		}
	}

	if len(parts) == 0 {
		parts = append(parts, MutedText("no session"))
	}

	// Join parts with a separator that re-applies BlackBackground so the pipe
	// character does not appear on the terminal's default background colour.
	var sb strings.Builder
	for i, p := range parts {
		if i > 0 {
			sb.WriteString(BlackBackground + DimFG + " | ")
		}
		sb.WriteString(p)
	}
	sb.WriteString("\x1b[0m")
	return sb.String(), sessionForTitle
}

func (r *REPL) renderStatusBar(ctx context.Context) {
	content, sessionForTitle := r.buildStatusContent(ctx)

	if r.statusBar != nil {
		r.statusBar.SetContent(content)
	} else {
		// Fallback: inline rendering above the prompt (existing behaviour).
		fmt.Fprint(r.stdout, content)
		fmt.Fprint(r.stdout, "\n")
	}

	// Push key state into the terminal title bar. Write directly to /dev/tty so
	// the OSC sequence reaches the terminal regardless of how readline buffers
	// or repositions stdout.
	titleParts := make([]string, 0, 4)
	titleParts = append(titleParts, "milliways")
	if r.runner != nil {
		titleParts = append(titleParts, r.runner.Name())
	}
	if sessionForTitle != nil && sessionForTitle.CostUSD > 0 {
		titleParts = append(titleParts, fmt.Sprintf("$%.2f %s↑", sessionForTitle.CostUSD, compactTokens(sessionForTitle.InputTokens)))
	}
	if r.session != nil && r.session.conversationID != "" {
		sid := r.session.conversationID
		if len(sid) > 12 {
			sid = sid[:12]
		}
		titleParts = append(titleParts, sid)
	}
	title := "\x1b]0;" + strings.Join(titleParts, " | ") + "\x07"
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		fmt.Fprint(tty, title)
		tty.Close()
	} else {
		fmt.Fprint(r.stdout, title)
	}
}

// replayTurnsToSubstrate pushes turns that were restored from local disk into
// the active MemPalace conversation so the substrate has context going forward.
// It is a best-effort operation — errors are silently dropped.
func (r *REPL) replayTurnsToSubstrate(ctx context.Context) {
	if r.substrate == nil || r.session == nil || len(r.turnBuffer) == 0 {
		return
	}
	for _, t := range r.turnBuffer {
		_ = r.substrate.ConversationAppendTurn(ctx, substrate.AppendTurnRequest{
			ConversationID: r.session.conversationID,
			Turn: conversation.Turn{
				Role:     conversation.TurnRole(t.Role),
				Provider: t.Runner,
				Text:     t.Text,
				At:       t.At,
			},
		})
	}
}

// lastAssistantText returns the Text of the most recent assistant ConversationTurn,
// or "" if none exists.
func (r *REPL) lastAssistantText() string {
	for i := len(r.turnBuffer) - 1; i >= 0; i-- {
		if r.turnBuffer[i].Role == "assistant" {
			return r.turnBuffer[i].Text
		}
	}
	return ""
}

func (r *REPL) colorizeOutput(s string) string {
	if s == "" {
		return ""
	}
	stripped := ansiStripper.ReplaceAllString(s, "")
	lines := strings.Split(stripped, "\n")
	var out []string
	for _, line := range lines {
		if line != "" {
			out = append(out, ColorText(r.scheme, line))
		} else {
			out = append(out, "")
		}
	}
	return strings.Join(out, "\n") + "\n"
}
