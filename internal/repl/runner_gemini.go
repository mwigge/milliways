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

	mu                sync.Mutex
	sessionIn         int
	sessionOut        int
	sessionCostUSD    float64
	sessionDispatches int
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

	usage, err := runGeminiCmd(ctx, cmd, out, req.Prompt)
	if usage != nil {
		r.mu.Lock()
		r.sessionIn += usage.inputTokens
		r.sessionOut += usage.outputTokens
		r.sessionCostUSD += usage.costUSD
		r.sessionDispatches++
		r.mu.Unlock()
	}
	return err
}

func (r *GeminiRunner) SetModel(model string) {
	r.model = strings.TrimSpace(model)
}

// runGeminiCmd runs gemini in headless mode, streams stdout to out, and
// captures stderr to detect session-limit signals. Returns session usage if available.
func runGeminiCmd(ctx context.Context, cmd *exec.Cmd, out io.Writer, prompt string) (*geminiSessionUsage, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var stderrLines []string
	var stderrMu sync.Mutex
	var stderrWg sync.WaitGroup

	stderrWg.Add(1)
	go func() {
		defer stderrWg.Done()
		scanner := bufio.NewScanner(stderr)
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

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	var mu sync.Mutex
	var outputLines []string

scanLoop:
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			break scanLoop
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		mu.Lock()
		outputLines = append(outputLines, line)
		mu.Unlock()

		// Write output with coloring
		colored := ColorText(GeminiScheme(), line)
		if _, werr := out.Write([]byte(colored + "\n")); werr != nil {
			break scanLoop
		}
	}
	scanErr := scanner.Err()

	_ = stdout.Close()
	waitErr := cmd.Wait()
	stderrWg.Wait()

	mu.Lock()
	var sessionUsage *geminiSessionUsage
	if len(outputLines) > 0 {
		// Estimate token usage from output length (rough approximation)
		outputText := strings.Join(outputLines, "\n")
		sessionUsage = &geminiSessionUsage{
			inputTokens:  len(prompt) / 4,
			outputTokens: len(outputText) / 4,
		}
	}
	mu.Unlock()

	stderrMu.Lock()
	lines := append([]string(nil), stderrLines...)
	stderrMu.Unlock()

	// Check session limit first
	if geminiStderrSignalsLimit(lines) {
		if waitErr != nil {
			return sessionUsage, fmt.Errorf("%w: %w", ErrSessionLimit, waitErr)
		}
		return sessionUsage, ErrSessionLimit
	}

	if scanErr != nil {
		if waitErr != nil {
			return sessionUsage, fmt.Errorf("gemini stdout read error: %v: %w", scanErr, waitErr)
		}
		return sessionUsage, fmt.Errorf("gemini stdout read error: %w", scanErr)
	}
	return sessionUsage, waitErr
}

type geminiSessionUsage struct {
	inputTokens  int
	outputTokens int
	costUSD      float64
}

// geminiStderrSignalsLimit returns true when any stderr line from gemini
// indicates a quota or context-window exhaustion.
// This is comprehensive detection matching ClaudeRunner's approach.
func geminiStderrSignalsLimit(lines []string) bool {
	for _, l := range lines {
		lower := strings.ToLower(l)
		// Comprehensive session limit detection for gemini CLI
		// Covers context window, quota, rate limits, token limits, and various error messages
		// Includes Gemini-specific signals like "resource_exhausted" from the API
		if strings.Contains(lower, "context window") ||
			strings.Contains(lower, "context_length") ||
			strings.Contains(lower, "quota") ||
			strings.Contains(lower, "rate limit") ||
			strings.Contains(lower, "resource_exhausted") ||
			strings.Contains(lower, "token limit") ||
			strings.Contains(lower, "session limit") ||
			strings.Contains(lower, "max turns") ||
			strings.Contains(lower, "turn limit") ||
			strings.Contains(lower, "too long") ||
			strings.Contains(lower, "exceeded") ||
			strings.Contains(lower, "daily limit") ||
			strings.Contains(lower, "limit reached") ||
			strings.Contains(lower, "context_length_exceeded") {
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessionDispatches == 0 {
		return nil, nil
	}
	return &QuotaInfo{
		Session: &SessionUsage{
			InputTokens:  r.sessionIn,
			OutputTokens: r.sessionOut,
			CostUSD:      r.sessionCostUSD,
			Dispatches:   r.sessionDispatches,
		},
	}, nil
}

// GeminiSettings holds the configurable settings for the gemini runner.
type GeminiSettings struct {
	Model string
}

// Settings returns the current runner configuration.
func (r *GeminiRunner) Settings() GeminiSettings {
	return GeminiSettings{
		Model: r.model,
	}
}

// GeminiScheme returns the color scheme for gemini output
func GeminiScheme() ColorScheme {
	return ColorScheme{
		FG:     "\x1b[38;5;75m", // Blue/green for Gemini
		Accent: "\x1b[38;5;75m",
		Runner: "gemini",
	}
}