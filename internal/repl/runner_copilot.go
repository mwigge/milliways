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
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type CopilotRunner struct {
	binary string
}

func NewCopilotRunner() *CopilotRunner {
	return &CopilotRunner{
		binary: "copilot",
	}
}

func (r *CopilotRunner) Name() string { return "copilot" }

func (r *CopilotRunner) Execute(ctx context.Context, req DispatchRequest, out io.Writer) error {
	if len(req.Attachments) > 0 {
		slog.Warn("copilot: image attachments not supported, proceeding with text only",
			"count", len(req.Attachments))
	}
	cwd, _ := os.Getwd()
	// --add-dir scopes file search to the project directory, avoiding macOS
	// system paths that produce permission errors when copilot searches broadly.
	args := []string{"-p", buildTextPrompt(req), "--allow-all-tools", "--add-dir", cwd}
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = cwd
	return runCopilotCmd(ctx, cmd, out)
}

// runCopilotCmd runs a copilot command, streams stdout to out, captures stderr
// separately to detect session-limit signals, and emits SessionLimitSentinel
// when a rate-limit or context-related error is detected.
func runCopilotCmd(ctx context.Context, cmd *exec.Cmd, out io.Writer) error {
	pr, pw := io.Pipe()
	cmd.Stdout = pw

	stderrPR, stderrPW := io.Pipe()
	cmd.Stderr = stderrPW

	if err := cmd.Start(); err != nil {
		_ = pw.CloseWithError(err)
		_ = pr.Close()
		_ = stderrPW.CloseWithError(err)
		_ = stderrPR.Close()
		return err
	}

	var stderrLines []string
	var stderrMu sync.Mutex
	var stderrWg sync.WaitGroup

	stderrWg.Add(1)
	go func() {
		defer stderrWg.Done()
		scanner := bufio.NewScanner(stderrPR)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			stderrMu.Lock()
			stderrLines = append(stderrLines, line)
			stderrMu.Unlock()
		}
	}()

	waitDone := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = pw.CloseWithError(err)
		_ = stderrPW.CloseWithError(err)
		waitDone <- err
	}()

	_, _ = io.Copy(out, pr)
	_ = pr.Close()

	stderrWg.Wait()
	waitErr := <-waitDone

	if copilotStderrSignalsLimit(stderrLines) {
		if waitErr != nil {
			return fmt.Errorf("%w: %w", ErrSessionLimit, waitErr)
		}
		return ErrSessionLimit
	}
	return waitErr
}

// copilotStderrSignalsLimit returns true when any stderr line from copilot
// indicates a rate-limit or context-window exhaustion.
func copilotStderrSignalsLimit(lines []string) bool {
	for _, l := range lines {
		lower := strings.ToLower(l)
		if strings.Contains(lower, "rate limit") ||
			strings.Contains(lower, "context window") ||
			strings.Contains(lower, "context_length") ||
			strings.Contains(lower, "token limit") {
			return true
		}
	}
	return false
}

func (r *CopilotRunner) AuthStatus() (bool, error) {
	return true, nil
}

func (r *CopilotRunner) Login() error {
	cmd := exec.Command("copilot", "auth", "login")
	_, err := runPTY(cmd)
	return err
}

func (r *CopilotRunner) Logout() error {
	cmd := exec.Command("copilot", "auth", "logout")
	_, err := runPTY(cmd)
	return err
}

func (r *CopilotRunner) Quota() (*QuotaInfo, error) {
	return nil, nil
}
