package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/mwigge/milliways/internal/kitchen"
)

// CodexAdapter speaks Codex CLI's --json JSONL event protocol.
type CodexAdapter struct {
	kitchen   *kitchen.GenericKitchen
	opts      AdapterOpts
	mu        sync.Mutex
	stdinPipe io.WriteCloser
	processID int
}

// NewCodexAdapter creates an adapter for the codex kitchen.
func NewCodexAdapter(k *kitchen.GenericKitchen, opts AdapterOpts) *CodexAdapter {
	return &CodexAdapter{kitchen: k, opts: opts}
}

// codexEvent represents a raw JSONL event from codex exec --json.
type codexEvent struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Message string `json:"message,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Status  string `json:"status,omitempty"`
}

// Exec starts codex with --json and returns an event channel.
func (a *CodexAdapter) Exec(ctx context.Context, task kitchen.Task) (<-chan Event, error) {
	cfg := a.kitchen.Config()

	if !kitchen.IsCmdAllowed(cfg.Cmd) && !kitchen.IsCmdAllowed(cfg.Name) {
		return nil, fmt.Errorf("command %q not in allowed list", cfg.Cmd)
	}

	args := []string{"exec", "--json", task.Prompt}

	cmd := exec.CommandContext(ctx, cfg.Cmd, args...)
	if task.Dir != "" {
		cmd.Dir = task.Dir
	}
	cmd.Env = safeEnv(task.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting codex: %w", err)
	}

	a.mu.Lock()
	a.stdinPipe = stdinPipe
	a.processID = 0
	if cmd.Process != nil {
		a.processID = cmd.Process.Pid
	}
	a.mu.Unlock()

	ch := make(chan Event, 64)
	name := a.kitchen.Name()

	go func() {
		defer close(ch)
		defer func() {
			a.mu.Lock()
			if a.stdinPipe != nil {
				a.stdinPipe.Close()
				a.stdinPipe = nil
			}
			a.processID = 0
			a.mu.Unlock()
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

		sawDone := false
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var evt codexEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				if rl := parseGenericExhaustionText(name, line, "stdout_text"); rl != nil {
					ch <- *rl
				} else {
					ch <- Event{Type: EventText, Kitchen: name, Text: line}
				}
				continue
			}

			text := evt.Content
			if text == "" {
				text = evt.Message
			}

			switch evt.Type {
			case "message", "assistant", "text":
				parsed := ParseTextToEvents(name, text)
				for _, e := range parsed {
					ch <- e
				}
			case "tool_use", "tool_call":
				ch <- Event{
					Type:       EventToolUse,
					Kitchen:    name,
					ToolName:   evt.Tool,
					ToolStatus: evt.Status,
				}
			case "error":
				if rl := parseGenericExhaustionText(name, text, "stdout_text"); rl != nil {
					ch <- *rl
				} else {
					ch <- Event{Type: EventError, Kitchen: name, Text: text}
				}
			case "result", "done":
				sawDone = true
				ch <- Event{Type: EventDone, Kitchen: name, ExitCode: 0}
			default:
				if text != "" {
					ch <- Event{Type: EventText, Kitchen: name, Text: text}
				}
			}
		}

		if scanErr := scanner.Err(); scanErr != nil {
			ch <- Event{Type: EventError, Kitchen: name, Text: fmt.Sprintf("scanner: %v", scanErr)}
		}

		waitErr := cmd.Wait()
		if !sawDone {
			exitCode := 0
			if waitErr != nil {
				var exitErr *exec.ExitError
				if errors.As(waitErr, &exitErr) {
					exitCode = exitErr.ExitCode()
				}
			}
			ch <- Event{Type: EventDone, Kitchen: name, ExitCode: exitCode}
		}
	}()

	return ch, nil
}

// Send writes a text line to codex's stdin pipe.
func (a *CodexAdapter) Send(ctx context.Context, msg string) error {
	a.mu.Lock()
	pipe := a.stdinPipe
	a.mu.Unlock()

	if pipe == nil {
		return ErrNotInteractive
	}

	done := make(chan error, 1)
	go func() { _, err := fmt.Fprintln(pipe, msg); done <- err }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SupportsResume returns false — codex exec is one-shot.
func (a *CodexAdapter) SupportsResume() bool { return false }

// SessionID returns "" for codex.
func (a *CodexAdapter) SessionID() string { return "" }

// ProcessID returns the running subprocess pid when available.
func (a *CodexAdapter) ProcessID() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.processID
}

// Capabilities returns Codex continuity features.
func (a *CodexAdapter) Capabilities() Capabilities {
	return Capabilities{
		NativeResume:        false,
		InteractiveSend:     true,
		StructuredEvents:    true,
		ExhaustionDetection: "stdout",
	}
}
