package adapter

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

// GenericAdapter is the fallback adapter for kitchens without structured output.
// It reads stdout line-by-line, detecting dialogue markers when present.
type GenericAdapter struct {
	kitchen   *kitchen.GenericKitchen
	opts      AdapterOpts
	mu        sync.Mutex
	stdinPipe io.WriteCloser
	processID int
}

// NewGenericAdapter creates a fallback line-by-line adapter.
func NewGenericAdapter(k *kitchen.GenericKitchen, opts AdapterOpts) *GenericAdapter {
	return &GenericAdapter{kitchen: k, opts: opts}
}

// Exec starts the kitchen process and returns an event channel.
func (a *GenericAdapter) Exec(ctx context.Context, task kitchen.Task) (<-chan Event, error) {
	cfg := a.kitchen.Config()

	if !kitchen.IsCmdAllowed(cfg.Cmd) && !kitchen.IsCmdAllowed(cfg.Name) {
		return nil, fmt.Errorf("command %q not in allowed list", cfg.Cmd)
	}

	args := make([]string, len(cfg.Args))
	copy(args, cfg.Args)
	args = append(args, task.Prompt)

	cmd := exec.CommandContext(ctx, cfg.Cmd, args...)
	if task.Dir != "" {
		cmd.Dir = task.Dir
	}
	cmd.Env = safeEnv(task.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	// Open stdin pipe — held open for process lifetime
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", a.kitchen.Name(), err)
	}

	a.mu.Lock()
	a.stdinPipe = stdinPipe
	a.processID = 0
	if cmd.Process != nil {
		a.processID = cmd.Process.Pid
	}
	a.mu.Unlock()

	ch := make(chan Event, 64)

	go func() {
		defer close(ch)
		defer func() {
			a.mu.Lock()
			if a.stdinPipe != nil {
				_ = a.stdinPipe.Close()
				a.stdinPipe = nil
			}
			a.processID = 0
			a.mu.Unlock()
		}()

		name := a.kitchen.Name()
		var output strings.Builder

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			output.WriteString(line)
			output.WriteString("\n")

			// Call legacy OnLine if set
			if task.OnLine != nil {
				task.OnLine(line)
			}

			if kitchen.IsQuestion(line) {
				question := kitchen.StripPrefix(line)
				ch <- Event{Type: EventQuestion, Kitchen: name, Text: question}
				if task.OnQuestion != nil {
					task.OnQuestion(question)
				} else if err := a.Send(ctx, ""); err != nil && !errors.Is(err, context.Canceled) {
					ch <- Event{Type: EventError, Kitchen: name, Text: fmt.Sprintf("sending question response: %v", err)}
					return
				}
				continue
			}

			if kitchen.IsConfirm(line) {
				question := kitchen.StripPrefix(line)
				ch <- Event{Type: EventConfirm, Kitchen: name, Text: question}
				if task.OnConfirm != nil {
					task.OnConfirm(question)
				} else if err := a.Send(ctx, ""); err != nil && !errors.Is(err, context.Canceled) {
					ch <- Event{Type: EventError, Kitchen: name, Text: fmt.Sprintf("sending confirm response: %v", err)}
					return
				}
				continue
			}

			ch <- Event{
				Type:    EventText,
				Kitchen: name,
				Text:    line,
			}
		}

		waitErr := cmd.Wait()
		duration := time.Since(start)

		exitCode := 0
		if waitErr != nil {
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
		}

		ch <- Event{
			Type:     EventDone,
			Kitchen:  name,
			ExitCode: exitCode,
			Cost: &CostInfo{
				DurationMs: int(duration.Milliseconds()),
			},
		}
	}()

	return ch, nil
}

// Send writes a text line to the kitchen's stdin.
func (a *GenericAdapter) Send(ctx context.Context, msg string) error {
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

// SupportsResume returns false for generic kitchens.
func (a *GenericAdapter) SupportsResume() bool { return false }

// SessionID returns "" for generic kitchens.
func (a *GenericAdapter) SessionID() string { return "" }

// ProcessID returns the running subprocess pid when available.
func (a *GenericAdapter) ProcessID() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.processID
}

// Capabilities returns generic fallback continuity features.
func (a *GenericAdapter) Capabilities() Capabilities {
	return Capabilities{
		NativeResume:        false,
		InteractiveSend:     true,
		StructuredEvents:    false,
		ExhaustionDetection: "none",
	}
}
