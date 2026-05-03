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

// copilotBinary is the executable name; var (not const) so tests can swap it.
var copilotBinary = "copilot"

// copilotArgsBuilder constructs the argv passed to the CLI for a given prompt
// and working directory. Tests can swap it to point at a shell fixture.
var copilotArgsBuilder = buildCopilotCmdArgs

// copilotChunkSize is the buffer size for streaming raw stdout/stderr.
// Each Read up to this size becomes one {"t":"data","b64":...} event.
const copilotChunkSize = 4 * 1024

// RunCopilot is the daemon-side copilot session loop. It reads prompts
// from `input`, spawns one `copilot -p <prompt> --allow-all-tools
// --add-dir <cwd>` per prompt, and streams stdout+stderr bytes (plain
// text, no JSON) as {"t":"data","b64":...} events. After the subprocess
// exits a final {"t":"chunk_end","cost_usd":0} marks end-of-response.
//
// Copilot's plain-text output does not surface token usage, so only
// error_count is observed via `metrics` (non-nil); tokens / cost are
// left for a future copilot CLI release that surfaces them.
//
// Lifecycle:
//   - One subprocess per prompt; the session stays alive across prompts.
//   - When `input` is closed, RunCopilot pushes {"t":"end"} and returns.
//   - The caller (AgentRegistry) is responsible for Close()ing the stream.
func RunCopilot(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
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
			runCopilotOnce(ctx, prompt, stream, metrics)
		}
	}
}

func runCopilotOnce(parent context.Context, prompt []byte, stream Pusher, metrics MetricsObserver) {
	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		stream.Push(map[string]any{"t": "chunk_end", "cost_usd": 0.0, "input_tokens": 0, "output_tokens": 0, "total_tokens": 0})
		return
	}

	spanCtx, span := startDispatchSpan(parent, AgentIDCopilot, "")
	ctx, cancel := contextWithOptionalTimeout(spanCtx, copilotRequestTimeout())
	defer cancel()

	spanErr := ""
	defer func() {
		endDispatchSpan(span, 0, 0, 0, spanErr)
		stream.Push(map[string]any{"t": "chunk_end", "cost_usd": 0.0, "input_tokens": 0, "output_tokens": 0, "total_tokens": 0})
	}()

	cwd, _ := os.Getwd()
	cmd := exec.CommandContext(ctx, copilotBinary, copilotArgsBuilder(text, cwd)...)
	cmd.Env = safeRunnerEnv()
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		observeError(metrics, AgentIDCopilot)
		spanErr = err.Error()
		stream.Push(copilotStartError("stdout pipe", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		observeError(metrics, AgentIDCopilot)
		spanErr = err.Error()
		stream.Push(copilotStartError("stderr pipe", err))
		return
	}
	if err := cmd.Start(); err != nil {
		observeError(metrics, AgentIDCopilot)
		spanErr = err.Error()
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			stream.Push(classifyDispatchError(AgentIDCopilot, ctx.Err()))
			return
		}
		stream.Push(copilotStartError("start", err))
		return
	}

	// Capture stderr for session-limit detection at the end of the run.
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
			stream.Push(encodeThinking(line))
			slog.Debug("copilot stderr", "line", line, "agent", AgentIDCopilot)
		}
	}()

	streamCopilotStdout(stdout, stream)

	waitErr := cmd.Wait()
	stderrWg.Wait()

	stderrMu.Lock()
	lines := append([]string(nil), stderrLines...)
	stderrMu.Unlock()

	if ctxErr := ctx.Err(); ctxErr != nil {
		observeError(metrics, AgentIDCopilot)
		spanErr = ctxErr.Error()
		stream.Push(classifyDispatchError(AgentIDCopilot, ctxErr))
		return
	}

	if errEvent, ok := classifyCopilotStderr(lines); ok {
		observeError(metrics, AgentIDCopilot)
		spanErr = errEvent["msg"].(string)
		stream.Push(errEvent)
		return
	}

	if waitErr != nil {
		observeError(metrics, AgentIDCopilot)
		spanErr = waitErr.Error()
		stream.Push(map[string]any{"t": "err", "agent": AgentIDCopilot, "code": -32010, "msg": exitMsg("copilot", waitErr, lines)})
	}
}

func buildCopilotCmdArgs(prompt, cwd string) []string {
	// --add-dir scopes file search to the project directory, avoiding system
	// paths that produce permission errors when file search expands broadly.
	args := []string{"-p", prompt, "--allow-all-tools", "--allow-all-paths"}
	if model := strings.TrimSpace(os.Getenv("COPILOT_MODEL")); model != "" {
		args = append(args, "--model", model)
	}
	if cwd != "" {
		args = append(args, "--add-dir", cwd)
	}
	return args
}

func copilotRequestTimeout() time.Duration {
	return runnerRequestTimeout("COPILOT_TIMEOUT")
}

func classifyCopilotStderr(lines []string) (map[string]any, bool) {
	for _, l := range lines {
		lower := strings.ToLower(l)
		switch {
		case strings.Contains(lower, "quota") ||
			strings.Contains(lower, "rate limit") ||
			strings.Contains(lower, "rate_limit") ||
			strings.Contains(lower, "too many requests") ||
			strings.Contains(lower, "insufficient balance") ||
			strings.Contains(lower, "daily limit"):
			return map[string]any{
				"t":     "err",
				"agent": AgentIDCopilot,
				"code":  -32013,
				"msg":   "copilot: quota or rate limit reached",
			}, true
		case strings.Contains(lower, "session limit") ||
			strings.Contains(lower, "context window") ||
			strings.Contains(lower, "context_length") ||
			strings.Contains(lower, "context_length_exceeded") ||
			strings.Contains(lower, "token limit") ||
			strings.Contains(lower, "max turns") ||
			strings.Contains(lower, "turn limit") ||
			strings.Contains(lower, "limit reached"):
			return map[string]any{
				"t":     "err",
				"agent": AgentIDCopilot,
				"code":  -32013,
				"msg":   "copilot: session limit reached",
			}, true
		case strings.Contains(lower, "timed out") ||
			strings.Contains(lower, "timeout") ||
			strings.Contains(lower, "deadline exceeded"):
			return map[string]any{
				"t":     "err",
				"agent": AgentIDCopilot,
				"code":  -32009,
				"msg":   "copilot: dispatch timeout",
			}, true
		}
	}
	return nil, false
}

// copilotStderrSignalsLimit returns true when any captured stderr line
// indicates a quota, session, or context-window exhaustion.
func copilotStderrSignalsLimit(lines []string) bool {
	event, ok := classifyCopilotStderr(lines)
	if !ok {
		return false
	}
	code, _ := event["code"].(int)
	return code == -32013
}

func copilotStartError(stage string, err error) map[string]any {
	msg := "copilot: could not start — " + installHint("gh")
	if stage != "start" {
		msg = "copilot: failed to start — try again"
	}
	return map[string]any{
		"t":      "err",
		"agent":  AgentIDCopilot,
		"code":   -32010,
		"msg":    msg,
		"detail": scrubBearer(err.Error()),
	}
}

// streamCopilotStdout reads from r in copilotChunkSize chunks and pushes
// each non-empty chunk as {"t":"data","b64":...}. Plain text — no JSON
// parsing — copilot's `-p` output is human-readable.
func streamCopilotStdout(r io.Reader, stream Pusher) {
	buf := make([]byte, copilotChunkSize)
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
