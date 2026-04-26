package repl

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/substrate"
	"github.com/peterh/liner"
)

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
	liner         *liner.State
	runner        Runner
	runners       map[string]Runner
	prev          Runner
	history       []string
	stdout        io.Writer
	runnerState   RunnerState
	substrate     *substrate.Client
	session       *replSession
	getQuota      func(name string) (*QuotaInfo, error)
	currentChange string
	scheme        ColorScheme
	version       string
}

func (r *REPL) SetVersion(v string) {
	r.version = v
}

type replSession struct {
	conversationID string
	runnerName     string
	outputBuf      *bytes.Buffer
}

func NewREPL(stdout io.Writer) *REPL {
	liner := liner.NewLiner()
	liner.SetCtrlCAborts(true)

	return &REPL{
		liner:   liner,
		runners: make(map[string]Runner),
		stdout:  stdout,
		scheme:  DefaultScheme(),
	}
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
	defer r.liner.Close()

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

	defer func() {
		r.println(ResetColor + BlackBackground)
		if r.substrate != nil && r.session != nil {
			_ = r.substrate.ConversationEnd(ctx, substrate.EndRequest{
				ConversationID: r.session.conversationID,
				Status:         "done",
				Reason:         "repl_exit",
			})
		}
	}()

	r.liner.SetCompleter(func(line string) []string {
		var completions []string
		if strings.HasPrefix(line, "/") {
			prefix := strings.TrimPrefix(line, "/")
			for cmd := range commandHandlers {
				if strings.HasPrefix(cmd, prefix) {
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
	r.println(PhosphorText("  REPL  |  type /help for commands"))
	if r.session != nil {
		r.println(MutedText(fmt.Sprintf("  session: %s", r.session.conversationID)))
	} else {
		r.println(MutedText("  no session"))
	}

	var runners []string
	for name := range r.runners {
		runners = append(runners, name)
	}
	r.println(PhosphorText("  runners: " + strings.Join(runners, " | ")))
	r.println("")
	r.println("")

	r.renderStatusBar(ctx)
	r.println("")

	for {
		line, err := r.liner.Prompt("▶ ")
		if err != nil {
			if err == liner.ErrPromptAborted {
				if r.runnerState.IsRunning() {
					r.runnerState.Cancel()
					r.println(MutedText("^C"))
					r.println(MutedText("interrupted — type a new prompt or /switch"))
					r.runnerState.SetDone()
					continue
				}
				r.println("")
				return nil
			}
			if err == liner.ErrNotTerminalOutput || err == io.EOF {
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		r.liner.AppendHistory(line)
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
			handlerErr = handler(ctx, r, input.Content)

		case InputShell:
			handlerErr = r.handleBash(input.Content)

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
	prompt := fmt.Sprintf("milliways REPL session started at %s", time.Now().Format(time.RFC3339))
	if repoPath != "" {
		prompt = fmt.Sprintf("milliways REPL session in %s at %s", repoPath, time.Now().Format(time.RFC3339))
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

	runCtx, cancel := context.WithCancel(ctx)
	r.runnerState.SetRunning(cancel)
	defer r.runnerState.SetDone()

	outputBuf := new(bytes.Buffer)
	tee := &teeWriter{w: r.stdout, buf: outputBuf, scheme: r.scheme}

	done := make(chan error, 1)
	go func() {
		r.println(fmt.Sprintf("[%s] %s", ColorText(r.scheme, r.runner.Name()), ColorText(r.scheme, "Thinking...")))
		start := time.Now()
		err := r.runner.Execute(runCtx, prompt, tee)
		tee.Flush()
		dur := time.Since(start)

		if r.session != nil && outputBuf.Len() > 0 {
			r.appendAssistantTurn(ctx, r.runner.Name(), outputBuf.String())
		}

		if err != nil {
			if ctx.Err() != nil {
				r.println(fmt.Sprintf("%s %s  %s",
					ColorText(r.scheme, "✗"),
					ColorText(r.scheme, r.runner.Name()),
					MutedText("interrupted")))
			} else {
				r.println(fmt.Sprintf("%s %s  %.1fs  %v",
					ColorText(r.scheme, "✗"),
					ColorText(r.scheme, r.runner.Name()),
					dur.Seconds(), err))
			}
			done <- err
			return
		}
		r.println(fmt.Sprintf("%s %s  %.1fs",
			ColorText(r.scheme, "✓"),
			ColorText(r.scheme, r.runner.Name()),
			dur.Seconds()))
		done <- nil
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		cancel()
		<-done
		return ctx.Err()
	}
}

func (r *REPL) handleBash(cmd string) error {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil
	}

	execCmd := exec.Command(parts[0], parts[1:]...)
	execCmd.Stdout = r.stdout
	execCmd.Stderr = os.Stderr

	return execCmd.Run()
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
			sw.w.Write(sw.lineBuf)
			sw.w.Write([]byte{'\n'})
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
		sw.w.Write(sw.lineBuf)
		sw.lineBuf = sw.lineBuf[:0]
	}
}

func streamCmdOutput(ctx context.Context, cmd *exec.Cmd, out io.Writer) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 1)

	wg.Add(2)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024), 1024*1024)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				out.Write(scanner.Bytes())
				out.Write([]byte{'\n'})
			}
		}
	}()

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 1024), 1024*1024)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				out.Write(scanner.Bytes())
				out.Write([]byte{'\n'})
			}
		}
	}()

	go func() {
		errChan <- cmd.Wait()
	}()

	wg.Wait()
	return <-errChan
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
			t.buf.WriteString(string(t.line))
			t.w.Write([]byte(colored + "\n"))
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
		t.buf.WriteString(string(t.line))
		t.w.Write([]byte(colored))
		t.line = nil
	}
}

func (r *REPL) renderStatusBar(ctx context.Context) {
	fmt.Fprint(r.stdout, "\x1b[s")

	var parts []string

	if r.runner != nil {
		name := r.runner.Name()
		dot := ""
		if r.runnerState.IsRunning() {
			dot = "●"
		}
		parts = append(parts, RunnerAccentText(name, dot+name))
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

	if len(parts) == 0 {
		parts = append(parts, MutedText("no session"))
	}

	fmt.Fprint(r.stdout, "\x1b[2;1H")
	fmt.Fprint(r.stdout, "\x1b[2K")
	r.print(BlackBackground)
	for _, p := range parts {
		r.print(p)
		r.print(MutedText(" | "))
	}
	fmt.Fprint(r.stdout, "\x1b[0m")
	fmt.Fprint(r.stdout, "\x1b[u")
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
