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

// GeminiRunner wraps the Gemini CLI using headless (-p) mode.
type GeminiRunner struct {
	binary string
	model  string
}

func NewGeminiRunner() *GeminiRunner {
	return &GeminiRunner{
		binary: "gemini",
	}
}

func (r *GeminiRunner) Name() string { return "gemini" }

func (r *GeminiRunner) Execute(ctx context.Context, req DispatchRequest, out io.Writer) error {
	if len(req.Attachments) > 0 {
		slog.Warn("gemini: image attachments not supported, proceeding with text only",
			"count", len(req.Attachments))
	}
	cwd, _ := os.Getwd()

	args := []string{
		"-p", buildTextPrompt(req),
		"-y",
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = cwd
	return runGeminiCmd(ctx, cmd, out)
}

func (r *GeminiRunner) SetModel(model string) {
	r.model = strings.TrimSpace(model)
}

// runGeminiCmd runs gemini in headless mode, streams stdout to out, and
// captures stderr to detect session-limit signals.
func runGeminiCmd(ctx context.Context, cmd *exec.Cmd, out io.Writer) error {
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

	_, copyErr := io.Copy(out, pr)
	_ = pr.Close()

	stderrWg.Wait()
	waitErr := <-waitDone

	if geminiStderrSignalsLimit(stderrLines) {
		if waitErr != nil {
			return fmt.Errorf("%w: %w", ErrSessionLimit, waitErr)
		}
		return ErrSessionLimit
	}
	if copyErr != nil {
		if waitErr != nil {
			return fmt.Errorf("gemini stdout read error: %v: %w", copyErr, waitErr)
		}
		return fmt.Errorf("gemini stdout read error: %w", copyErr)
	}
	return waitErr
}

// geminiStderrSignalsLimit returns true when any stderr line from gemini
// indicates a quota or context-window exhaustion.
func geminiStderrSignalsLimit(lines []string) bool {
	for _, l := range lines {
		lower := strings.ToLower(l)
		if strings.Contains(lower, "context window") ||
			strings.Contains(lower, "context_length") ||
			strings.Contains(lower, "quota") ||
			strings.Contains(lower, "rate limit") ||
			strings.Contains(lower, "resource_exhausted") ||
			strings.Contains(lower, "token limit") {
			return true
		}
	}
	return false
}

func (r *GeminiRunner) AuthStatus() (bool, error) {
	return true, nil
}

func (r *GeminiRunner) Login() error {
	cmd := exec.Command("gcloud", "auth", "login")
	_, err := runPTY(cmd)
	return err
}

func (r *GeminiRunner) Logout() error {
	cmd := exec.Command("gcloud", "auth", "revoke")
	_, err := runPTY(cmd)
	return err
}

func (r *GeminiRunner) Quota() (*QuotaInfo, error) {
	return nil, nil
}
