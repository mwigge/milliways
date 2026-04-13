package kitchen

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// allowedCmds is the set of CLI tools Milliways will execute.
var allowedCmds = map[string]bool{
	"claude":   true,
	"opencode": true,
	"gemini":   true,
	"aider":    true,
	"goose":    true,
	"cline":    true,
	"echo":     true, // for testing
	"false":    true, // for testing non-zero exit codes
	"true":     true, // for testing
}

// GenericConfig holds configuration for a GenericKitchen.
type GenericConfig struct {
	Name       string
	Cmd        string
	Args       []string
	Stations   []string
	Tier       CostTier
	Enabled    bool
	InstallCmd string
	AuthCmd    string
}

// GenericKitchen is a reusable adapter for any CLI tool.
type GenericKitchen struct {
	cfg GenericConfig
}

// NewGeneric creates a kitchen adapter for any CLI tool.
func NewGeneric(cfg GenericConfig) *GenericKitchen {
	return &GenericKitchen{cfg: cfg}
}

func (k *GenericKitchen) Name() string       { return k.cfg.Name }
func (k *GenericKitchen) CostTier() CostTier { return k.cfg.Tier }
func (k *GenericKitchen) InstallCmd() string { return k.cfg.InstallCmd }
func (k *GenericKitchen) AuthCmd() string    { return k.cfg.AuthCmd }

// Stations returns a defensive copy of the stations slice.
func (k *GenericKitchen) Stations() []string {
	return append([]string(nil), k.cfg.Stations...)
}

// Status checks if the CLI binary is installed and allowed.
func (k *GenericKitchen) Status() Status {
	if !k.cfg.Enabled {
		return Disabled
	}
	if _, err := exec.LookPath(k.cfg.Cmd); err != nil {
		return NotInstalled
	}
	return Ready
}

// Exec runs a prompt through the CLI tool and streams output.
func (k *GenericKitchen) Exec(ctx context.Context, task Task) (Result, error) {
	if k.Status() != Ready {
		return Result{ExitCode: 1}, fmt.Errorf("%s kitchen not ready: %s", k.cfg.Name, k.Status())
	}

	if !allowedCmds[k.cfg.Cmd] {
		return Result{ExitCode: 1}, fmt.Errorf("command %q not in allowed list", k.cfg.Cmd)
	}

	args := make([]string, len(k.cfg.Args))
	copy(args, k.cfg.Args)
	args = append(args, task.Prompt)

	cmd := exec.CommandContext(ctx, k.cfg.Cmd, args...)
	if task.Dir != "" {
		cmd.Dir = task.Dir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{ExitCode: 1}, fmt.Errorf("creating stdout pipe: %w", err)
	}

	var output strings.Builder
	start := time.Now()

	if err := cmd.Start(); err != nil {
		return Result{ExitCode: 1}, fmt.Errorf("starting %s: %w", k.cfg.Name, err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		output.WriteString(line)
		output.WriteString("\n")
		if task.OnLine != nil {
			task.OnLine(line)
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return Result{ExitCode: 1}, fmt.Errorf("reading %s output: %w", k.cfg.Name, scanErr)
	}

	waitErr := cmd.Wait()
	duration := time.Since(start)

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return Result{ExitCode: 1, Duration: duration}, fmt.Errorf("waiting for %s: %w", k.cfg.Name, waitErr)
		}
	}

	return Result{
		ExitCode: exitCode,
		Output:   output.String(),
		Duration: duration,
	}, nil
}
