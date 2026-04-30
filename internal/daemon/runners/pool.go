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

// poolBinary is the executable name; var (not const) so tests can swap it.
var poolBinary = "pool"

// poolArgsBuilder constructs the argv passed to the Poolside CLI for a given
// prompt and working directory. Default builds the headless invocation
// `pool exec -p <prompt> --directory <dir> --unsafe-auto-allow`. Tests can swap it.
var poolArgsBuilder = func(prompt, dir string) []string {
	args := []string{"exec", "-p", prompt, "--unsafe-auto-allow"}
	if dir != "" {
		args = append(args, "--directory", dir)
	}
	return args
}

// poolTimeout caps a single agent.send call's subprocess lifetime.
const poolTimeout = 5 * time.Minute

// poolChunkSize is the raw stdout buffer size; each Read up to this size
// becomes one {"t":"data","b64":...} event.
const poolChunkSize = 4 * 1024

// RunPool drains the input channel, spawning one
// `pool exec -p <prompt> --unsafe-auto-allow` subprocess per prompt. Stdout
// streams as {"t":"data","b64":...} events; stderr is consumed in parallel
// and inspected for session-limit signals (quota / rate-limit /
// context-window exhaustion). On subprocess exit a {"t":"chunk_end",
// "cost_usd":0} event marks end-of-response. Closing the input channel
// pushes a final {"t":"end"}.
//
// Pool's plain-text headless output does not surface token usage, so only
// error_count is observed; tokens / cost will land when a future pool CLI
// release exposes them.
//
// Lifecycle:
//   - One subprocess per prompt; the session stays alive across prompts.
//   - When `input` is closed, RunPool pushes {"t":"end"} and returns.
//   - The caller (AgentRegistry) is responsible for Close()ing the stream.
func RunPool(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
	for prompt := range input {
		if stream == nil {
			continue
		}
		runPoolOnce(ctx, prompt, stream, metrics)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

func runPoolOnce(parent context.Context, prompt []byte, stream Pusher, metrics MetricsObserver) {
	ctx, cancel := context.WithTimeout(parent, poolTimeout)
	defer cancel()

	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		return
	}

	defer func() { stream.Push(map[string]any{"t": "chunk_end", "cost_usd": 0.0}) }()

	cwd, _ := os.Getwd()
	cmd := exec.CommandContext(ctx, poolBinary, poolArgsBuilder(text, cwd)...)
	cmd.Env = safeRunnerEnv()
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		observeError(metrics, AgentIDPool)
		stream.Push(map[string]any{"t": "err", "msg": "pool stdout pipe: " + err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		observeError(metrics, AgentIDPool)
		stream.Push(map[string]any{"t": "err", "msg": "pool stderr pipe: " + err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		observeError(metrics, AgentIDPool)
		stream.Push(map[string]any{"t": "err", "msg": "pool start: " + err.Error()})
		return
	}

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
			slog.Debug("pool stderr", "line", line)
		}
	}()

	streamPoolStdout(stdout, stream)

	waitErr := cmd.Wait()
	stderrWg.Wait()

	stderrMu.Lock()
	lines := append([]string(nil), stderrLines...)
	stderrMu.Unlock()

	if poolStderrSignalsLimit(lines) {
		observeError(metrics, AgentIDPool)
		stream.Push(map[string]any{"t": "err", "agent": AgentIDPool, "msg": "pool: session limit reached"})
		return
	}

	if waitErr != nil {
		observeError(metrics, AgentIDPool)
		stream.Push(map[string]any{"t": "err", "msg": "pool exited: " + waitErr.Error()})
	}
}

// streamPoolStdout reads stdout in poolChunkSize chunks and pushes each
// non-empty chunk as {"t":"data","b64":...}. Plain text — no JSON parsing —
// pool's exec -p output is human-readable.
func streamPoolStdout(r io.Reader, stream Pusher) {
	buf := make([]byte, poolChunkSize)
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

// poolStderrSignalsLimit returns true when any captured stderr line
// indicates a quota / context-window / rate-limit exhaustion. Mirrors the
// comprehensive set used by REPL's runner_pool.go.
func poolStderrSignalsLimit(lines []string) bool {
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
