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

package kitchen

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IsCmdAllowed returns true if the command (or its basename) is in the allowlist.
func IsCmdAllowed(cmd string) bool {
	if allowedCmds[cmd] {
		return true
	}
	return allowedCmds[filepath.Base(cmd)]
}

// IsCmdAllowedKitchen returns true when either the command path/basename or the
// configured kitchen name is in the allowlist.
func IsCmdAllowedKitchen(cmd string, kitchenName string) bool {
	return IsCmdAllowed(cmd) || IsCmdAllowed(kitchenName)
}

// allowedCmds is the set of CLI tools Milliways will execute.
var allowedCmds = map[string]bool{
	"claude":                 true,
	"codex":                  true,
	"gpt":                    true,
	"opencode":               true,
	"gemini":                 true,
	"aider":                  true,
	"goose":                  true,
	"cline":                  true,
	"echo":                   true, // for testing
	"false":                  true, // for testing non-zero exit codes
	"true":                   true, // for testing
	"sleep":                  true, // for testing timeouts
	"fake-claude-web-search": true, // smoke testing
	"fake-claude-streaming":  true, // smoke testing
	"fake-kitchen-question":  true, // smoke testing
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

// Compile-time interface checks.
var (
	_ Kitchen   = (*GenericKitchen)(nil)
	_ Setupable = (*GenericKitchen)(nil)
)

// NewGeneric creates a kitchen adapter for any CLI tool.
func NewGeneric(cfg GenericConfig) *GenericKitchen {
	return &GenericKitchen{cfg: cfg}
}

// Config returns a copy of the kitchen's configuration.
func (k *GenericKitchen) Config() GenericConfig { return k.cfg }

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

	if !IsCmdAllowedKitchen(k.cfg.Cmd, k.cfg.Name) {
		return Result{ExitCode: 1}, fmt.Errorf("command %q not in allowed list", k.cfg.Cmd)
	}

	args := make([]string, len(k.cfg.Args))
	copy(args, k.cfg.Args)
	args = append(args, task.Prompt)

	cmd := exec.CommandContext(ctx, k.cfg.Cmd, args...)
	if task.Dir != "" {
		cmd.Dir = task.Dir
	}
	cmd.Env = safeEnvKitchen(task.Env)

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
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
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

// safeEnvKeys is the set of environment variables passed to subprocess execution.
var safeEnvKeys = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "SHELL": true,
	"TERM": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
	"TMPDIR": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true,
	"ANTHROPIC_API_KEY": true, "OPENAI_API_KEY": true,
	"GOOGLE_API_KEY": true, "GEMINI_API_KEY": true,
	"OLLAMA_HOST": true, "OPENCODE_MODEL": true,
}

// safeEnvKitchen returns a filtered environment for subprocess execution.
func safeEnvKitchen(extra map[string]string) []string {
	var env []string
	for _, e := range os.Environ() {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		if safeEnvKeys[key] {
			env = append(env, e)
		}
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
