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

package runners

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// geminiBinary is the executable name; var (not const) so tests can swap it.
var geminiBinary = "gemini"

// geminiArgsBuilder constructs the argv passed to the gemini CLI for a given
// prompt. Default builds the headless invocation `gemini -p <prompt> -y`.
// Tests can swap it to rewrite the command (e.g. point at /bin/sh).
var geminiArgsBuilder = func(prompt string) []string {
	return []string{"-p", prompt, "-y"}
}

// geminiTimeout caps a single agent.send call's subprocess lifetime.
const geminiTimeout = 5 * time.Minute

// geminiChunkSize is the raw stdout buffer size; each Read up to this size
// becomes one {"t":"data","b64":...} event.
const geminiChunkSize = 4 * 1024

// RunGemini drains the input channel, spawning one `gemini -p <prompt> -y`
// subprocess per prompt. Stdout streams as {"t":"data","b64":...} events;
// stderr is consumed in parallel and inspected for session-limit signals
// (quota/rate-limit/context-window exhaustion). On subprocess exit, a
// {"t":"chunk_end","cost_usd":0} event marks end-of-response. Closing the
// input channel pushes a final {"t":"end"}.
//
// Gemini CLI does not surface token usage in its plain-text output, so
// only error_count is observed (token / cost metrics will land when a
// future gemini CLI release exposes them).
//
// Lifecycle:
//   - One subprocess per prompt; the session stays alive across prompts.
//   - When `input` is closed, RunGemini pushes {"t":"end"} and returns.
//   - The caller (AgentRegistry) is responsible for Close()ing the stream.
func RunGemini(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
	for prompt := range input {
		if stream == nil {
			continue
		}
		runGeminiOnce(ctx, prompt, stream, metrics)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

func runGeminiOnce(parent context.Context, prompt []byte, stream Pusher, metrics MetricsObserver) {
	ctx, cancel := context.WithTimeout(parent, geminiTimeout)
	defer cancel()

	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		return
	}

	cwd, _ := os.Getwd()
	cmd := exec.CommandContext(ctx, geminiBinary, geminiArgsBuilder(text)...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		observeError(metrics, AgentIDGemini)
		stream.Push(map[string]any{"t": "err", "msg": "gemini stdout pipe: " + err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		observeError(metrics, AgentIDGemini)
		stream.Push(map[string]any{"t": "err", "msg": "gemini stderr pipe: " + err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		observeError(metrics, AgentIDGemini)
		stream.Push(map[string]any{"t": "err", "msg": "gemini start: " + err.Error()})
		return
	}

	// Capture stderr lines so we can inspect for session-limit signals
	// once the subprocess exits.
	var (
		stderrLines []string
		stderrMu    sync.Mutex
		stderrWg    sync.WaitGroup
	)
	stderrWg.Add(1)
	go func() {
		defer stderrWg.Done()
		s := bufio.NewScanner(stderr)
		s.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if line == "" {
				continue
			}
			stderrMu.Lock()
			stderrLines = append(stderrLines, line)
			stderrMu.Unlock()
			slog.Debug("gemini stderr", "line", line)
		}
	}()

	streamGeminiStdout(stdout, stream)

	waitErr := cmd.Wait()
	stderrWg.Wait()

	stderrMu.Lock()
	lines := append([]string(nil), stderrLines...)
	stderrMu.Unlock()

	if geminiStderrSignalsLimit(lines) {
		observeError(metrics, AgentIDGemini)
		stream.Push(map[string]any{"t": "err", "msg": "gemini: session limit reached"})
		stream.Push(map[string]any{"t": "chunk_end", "cost_usd": 0.0})
		return
	}

	if waitErr != nil {
		observeError(metrics, AgentIDGemini)
		stream.Push(map[string]any{"t": "err", "msg": "gemini exited: " + waitErr.Error()})
	}
	stream.Push(map[string]any{"t": "chunk_end", "cost_usd": 0.0})
}

// streamGeminiStdout reads stdout in geminiChunkSize chunks and pushes
// each non-empty chunk as {"t":"data","b64":...}. Plain text — no JSON
// parsing — gemini's `-p -y` headless output is human-readable.
func streamGeminiStdout(r io.Reader, stream Pusher) {
	buf := make([]byte, geminiChunkSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			stream.Push(encodeData(string(buf[:n])))
		}
		if err != nil {
			return
		}
	}
}

// geminiStderrSignalsLimit returns true when any captured stderr line
// indicates a quota / context-window / rate-limit exhaustion. Mirrors
// the comprehensive set used by REPL's runner_gemini.go so the daemon
// surfaces the same set of conditions.
func geminiStderrSignalsLimit(lines []string) bool {
	for _, l := range lines {
		lower := strings.ToLower(l)
		if strings.Contains(lower, "context window") ||
			strings.Contains(lower, "context_length") ||
			strings.Contains(lower, "context_length_exceeded") ||
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
			strings.Contains(lower, "limit reached") {
			return true
		}
	}
	return false
}
