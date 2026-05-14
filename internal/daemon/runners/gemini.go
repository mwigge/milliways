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
	"errors"
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
	return geminiDefaultArgs(prompt)
}

// geminiDefaultArgs builds the argv for a normal gemini invocation.
// The -y flag auto-approves all tool use (YOLO mode). Set
// MILLIWAYS_GEMINI_YOLO=off (or =false) to omit it — useful when gemini
// aggressively invokes tools for simple questions. Default is to include -y.
func geminiDefaultArgs(prompt string) []string {
	yolo := os.Getenv("MILLIWAYS_GEMINI_YOLO")
	yoloOff := strings.EqualFold(yolo, "off") || strings.EqualFold(yolo, "false")
	if yoloOff {
		return []string{"-p", prompt}
	}
	return []string{"-p", prompt, "-y"}
}

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
// Timeout override: Gemini has no milliways-imposed subprocess timeout by
// default. Set GEMINI_TIMEOUT to a Go duration ("10m") or seconds ("600")
// to cap each prompt dispatch. "off", "none", "0", empty, and invalid
// values mean no timeout. MILLIWAYS_GEMINI_TIMEOUT is also accepted as a
// namespaced alias for process-level configuration.
//
// Lifecycle:
//   - One subprocess per prompt; the session stays alive across prompts.
//   - When `input` is closed, RunGemini pushes {"t":"end"} and returns.
//   - The caller (AgentRegistry) is responsible for Close()ing the stream.
func RunGemini(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
	for {
		select {
		case <-ctx.Done():
			if stream != nil {
				stream.Push(map[string]any{"t": "end"})
			}
			return
		case prompt, ok := <-input:
			if !ok {
				if stream != nil {
					stream.Push(map[string]any{"t": "end"})
				}
				return
			}
			if stream == nil {
				continue
			}
			runGeminiOnce(ctx, prompt, stream, metrics)
		}
	}
}

func runGeminiOnce(parent context.Context, prompt []byte, stream Pusher, metrics MetricsObserver) {
	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		stream.Push(geminiChunkEndEvent())
		return
	}

	spanCtx, span := startDispatchSpan(parent, AgentIDGemini, "")
	ctx, cancel := contextWithOptionalTimeout(spanCtx, geminiRequestTimeout())
	defer cancel()

	spanErr := ""
	defer func() {
		endDispatchSpan(span, 0, 0, 0, spanErr)
		stream.Push(geminiChunkEndEvent())
	}()
	pushModel(stream, AgentIDGemini)

	cwd, _ := os.Getwd()
	if !runExternalCLIPreflight(ctx, AgentIDGemini, cwd, stream, metrics) {
		spanErr = "security profile blocked handoff"
		return
	}
	cmd := exec.CommandContext(ctx, geminiBinary, geminiArgsBuilder(text)...)
	cmd.Env = safeRunnerEnv()
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		observeError(metrics, AgentIDGemini)
		spanErr = err.Error()
		stream.Push(geminiStartErrorEvent(err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		observeError(metrics, AgentIDGemini)
		spanErr = err.Error()
		stream.Push(geminiStartErrorEvent(err))
		return
	}
	if err := cmd.Start(); err != nil {
		observeError(metrics, AgentIDGemini)
		if ctxErr := ctx.Err(); ctxErr != nil {
			spanErr = ctxErr.Error()
			stream.Push(geminiContextErrorEvent(ctxErr))
			return
		}
		spanErr = err.Error()
		stream.Push(geminiStartErrorEvent(err))
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
			if !geminiStderrIsNoise(line) {
				stream.Push(encodeThinking(line))
			}
			slog.Debug("gemini stderr", "line", line)
		}
	}()

	streamGeminiStdout(stdout, stream)

	waitErr := cmd.Wait()
	stderrWg.Wait()

	stderrMu.Lock()
	lines := append([]string(nil), stderrLines...)
	stderrMu.Unlock()

	if ctxErr := ctx.Err(); ctxErr != nil {
		observeError(metrics, AgentIDGemini)
		spanErr = ctxErr.Error()
		stream.Push(geminiContextErrorEvent(ctxErr))
		return
	}

	if kind, ok := geminiStderrLimitKind(lines); ok {
		observeError(metrics, AgentIDGemini)
		spanErr = kind
		stream.Push(geminiLimitErrorEvent(kind))
		return
	}

	if waitErr != nil {
		observeError(metrics, AgentIDGemini)
		spanErr = waitErr.Error()
		stream.Push(geminiExitErrorEvent(waitErr, lines))
	}
}

func geminiChunkEndEvent() map[string]any {
	return zeroUsageChunkEnd()
}

func geminiRequestTimeout() time.Duration {
	return runnerRequestTimeoutAny("GEMINI_TIMEOUT", "MILLIWAYS_GEMINI_TIMEOUT")
}

func geminiContextErrorEvent(err error) map[string]any {
	switch {
	case errors.Is(err, context.Canceled):
		return map[string]any{
			"t":     "err",
			"agent": AgentIDGemini,
			"code":  -32008,
			"msg":   "gemini: dispatch cancelled",
		}
	case errors.Is(err, context.DeadlineExceeded):
		return map[string]any{
			"t":     "err",
			"agent": AgentIDGemini,
			"code":  -32009,
			"msg":   "gemini: dispatch timeout",
		}
	default:
		return map[string]any{
			"t":     "err",
			"agent": AgentIDGemini,
			"code":  -32010,
			"msg":   "gemini: " + err.Error(),
		}
	}
}

func geminiStartErrorEvent(err error) map[string]any {
	msg := "gemini: could not start — " + installHint("gemini")
	if err != nil {
		msg += " (" + scrubBearer(err.Error()) + ")"
	}
	return map[string]any{
		"t":     "err",
		"agent": AgentIDGemini,
		"code":  -32015,
		"msg":   msg,
	}
}

func geminiLimitErrorEvent(kind string) map[string]any {
	if kind == "quota" {
		return map[string]any{
			"t":     "err",
			"agent": AgentIDGemini,
			"code":  -32013,
			"msg":   "gemini: quota or rate limit reached",
		}
	}
	return map[string]any{
		"t":     "err",
		"agent": AgentIDGemini,
		"code":  -32014,
		"msg":   "gemini: session limit reached",
	}
}

func geminiExitErrorEvent(waitErr error, stderrLines []string) map[string]any {
	return map[string]any{
		"t":     "err",
		"agent": AgentIDGemini,
		"code":  -32010,
		"msg":   exitMsg("gemini", waitErr, stderrLines),
	}
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

// geminiStderrIsNoise reports whether a stderr line is purely informational
// and should not be forwarded to the stream as a thinking event. This
// filters the "YOLO mode" banner and tool-detection noise that gemini CLI
// prints unconditionally, which would otherwise appear as spurious thinking
// output to the user.
func geminiStderrIsNoise(line string) bool {
	if strings.TrimSpace(line) == "" {
		return true
	}
	lower := strings.ToLower(line)
	return strings.Contains(lower, "yolo mode") ||
		strings.Contains(lower, "all tool calls will be automatically approved") ||
		strings.Contains(lower, "missing pgrep output")
}

// geminiStderrSignalsLimit returns true when any captured stderr line
// indicates a quota / context-window / rate-limit exhaustion. Mirrors
// the comprehensive set used by REPL's runner_gemini.go so the daemon
// surfaces the same set of conditions.
func geminiStderrSignalsLimit(lines []string) bool {
	_, ok := geminiStderrLimitKind(lines)
	return ok
}

func geminiStderrLimitKind(lines []string) (string, bool) {
	for _, l := range lines {
		lower := strings.ToLower(l)
		if strings.Contains(lower, "quota") ||
			strings.Contains(lower, "rate limit") ||
			strings.Contains(lower, "rate_limit") ||
			strings.Contains(lower, "resource_exhausted") ||
			strings.Contains(lower, "too many requests") ||
			strings.Contains(lower, "insufficient balance") ||
			strings.Contains(lower, "daily limit") ||
			strings.Contains(lower, "limit reached") {
			return "quota", true
		}
		if strings.Contains(lower, "context window") ||
			strings.Contains(lower, "context_length") ||
			strings.Contains(lower, "context_length_exceeded") ||
			strings.Contains(lower, "token limit") ||
			strings.Contains(lower, "session limit") ||
			strings.Contains(lower, "max turns") ||
			strings.Contains(lower, "turn limit") ||
			strings.Contains(lower, "too long") ||
			strings.Contains(lower, "exceeded") {
			return "session", true
		}
	}
	return "", false
}
