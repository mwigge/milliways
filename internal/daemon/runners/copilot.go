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
	"time"
)

// copilotBinary is the executable name; var (not const) so tests can swap it.
var copilotBinary = "copilot"

// copilotTimeout caps a single agent.send call's subprocess lifetime.
const copilotTimeout = 5 * time.Minute

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
	for prompt := range input {
		if stream == nil {
			continue
		}
		runCopilotOnce(ctx, prompt, stream, metrics)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

func runCopilotOnce(parent context.Context, prompt []byte, stream Pusher, metrics MetricsObserver) {
	ctx, cancel := context.WithTimeout(parent, copilotTimeout)
	defer cancel()

	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		return
	}

	cwd, _ := os.Getwd()
	// --add-dir scopes file search to the project directory, avoiding macOS
	// system paths that produce permission errors when copilot searches broadly.
	args := []string{"-p", text, "--allow-all-tools"}
	if cwd != "" {
		args = append(args, "--add-dir", cwd)
	}
	cmd := exec.CommandContext(ctx, copilotBinary, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		observeError(metrics, AgentIDCopilot)
		stream.Push(map[string]any{"t": "err", "msg": "copilot stdout pipe: " + err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		observeError(metrics, AgentIDCopilot)
		stream.Push(map[string]any{"t": "err", "msg": "copilot stderr pipe: " + err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		observeError(metrics, AgentIDCopilot)
		stream.Push(map[string]any{"t": "err", "msg": "copilot start: " + err.Error()})
		return
	}

	// Drain stderr in parallel; log lines for debugging.
	go func() {
		s := bufio.NewScanner(stderr)
		s.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for s.Scan() {
			slog.Debug("copilot stderr", "line", s.Text())
		}
	}()

	streamCopilotStdout(stdout, stream)

	if err := cmd.Wait(); err != nil {
		observeError(metrics, AgentIDCopilot)
		stream.Push(map[string]any{"t": "err", "msg": "copilot exited: " + err.Error()})
	}
	stream.Push(map[string]any{"t": "chunk_end", "cost_usd": 0.0})
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
