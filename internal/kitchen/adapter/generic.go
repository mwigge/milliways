package adapter

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

// GenericAdapter is the fallback adapter for kitchens without structured output.
// It reads stdout line-by-line and emits EventText for each line.
type GenericAdapter struct {
	kitchen *kitchen.GenericKitchen
	opts    AdapterOpts
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

	ch := make(chan Event, 64)

	go func() {
		defer close(ch)
		defer stdinPipe.Close()

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

// Send returns ErrNotInteractive — generic kitchens don't support dialogue.
func (a *GenericAdapter) Send(_ context.Context, _ string) error {
	return ErrNotInteractive
}

// SupportsResume returns false for generic kitchens.
func (a *GenericAdapter) SupportsResume() bool { return false }

// SessionID returns "" for generic kitchens.
func (a *GenericAdapter) SessionID() string { return "" }

// Capabilities returns generic fallback continuity features.
func (a *GenericAdapter) Capabilities() Capabilities {
	return Capabilities{
		NativeResume:        false,
		InteractiveSend:     false,
		StructuredEvents:    false,
		ExhaustionDetection: "none",
	}
}
