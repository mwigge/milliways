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
	"encoding/json"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// codexJSONEvent mirrors the subset of the `codex exec --json` event
// stream we care about for the daemon-side bridge. Cribbed (without
// lifting wholesale) from internal/repl/runner_codex.go.
type codexJSONEvent struct {
	Type    string          `json:"type"`
	Content string          `json:"content,omitempty"`
	Message string          `json:"message,omitempty"`
	Text    string          `json:"text,omitempty"`
	Delta   string          `json:"delta,omitempty"`
	Item    json.RawMessage `json:"item,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type codexJSONItem struct {
	ItemType string `json:"item_type,omitempty"`
	Type     string `json:"type,omitempty"`
	Text     string `json:"text,omitempty"`
	Content  string `json:"content,omitempty"`
	Message  string `json:"message,omitempty"`
}

// codexBinary is the executable name; var (not const) so tests can swap it.
var codexBinary = "codex"

// codexTimeout caps a single agent.send call's subprocess lifetime.
const codexTimeout = 5 * time.Minute

// RunCodex is the daemon-side codex session loop. It reads prompts from
// `input`, spawns one `codex exec --json --color never --skip-git-repo-check`
// per prompt, decodes the assistant text from each NDJSON line, and pushes
// {"t":"data","b64":...} events to the stream. After the subprocess
// exits a final {"t":"chunk_end","cost_usd":0} marks end-of-response.
//
// Codex's JSON event stream does not currently expose token usage, so
// only error_count is observed via `metrics` (non-nil); tokens_in /
// tokens_out / cost_usd are left for a future codex CLI release that
// surfaces them.
//
// Lifecycle:
//   - One subprocess per prompt; the session stays alive across prompts.
//   - When `input` is closed, RunCodex pushes {"t":"end"} and returns.
//   - The caller (AgentRegistry) is responsible for Close()ing the stream.
func RunCodex(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
	for prompt := range input {
		if stream == nil {
			continue
		}
		runCodexOnce(ctx, prompt, stream, metrics)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

func runCodexOnce(parent context.Context, prompt []byte, stream Pusher, metrics MetricsObserver) {
	ctx, cancel := context.WithTimeout(parent, codexTimeout)
	defer cancel()

	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		return
	}

	cmd := exec.CommandContext(ctx, codexBinary, buildCodexCmdArgs(text, nil)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		observeError(metrics, AgentIDCodex)
		stream.Push(map[string]any{"t": "err", "msg": "codex stdout pipe: " + err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		observeError(metrics, AgentIDCodex)
		stream.Push(map[string]any{"t": "err", "msg": "codex stderr pipe: " + err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		observeError(metrics, AgentIDCodex)
		stream.Push(map[string]any{"t": "err", "msg": "codex start: " + err.Error()})
		return
	}

	// Capture stderr; check for proxy-block and session-limit signals at the end.
	var (
		stderrLines     []string
		stderrMu        sync.Mutex
		stderrWg        sync.WaitGroup
		sawProxyBlock   bool
		sawProxyMu      sync.Mutex
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
			if codexLineLooksProxyBlocked(line) {
				sawProxyMu.Lock()
				sawProxyBlock = true
				sawProxyMu.Unlock()
			}
			stderrMu.Lock()
			stderrLines = append(stderrLines, line)
			stderrMu.Unlock()
			slog.Debug("codex stderr", "line", line)
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	var sawSessionLimit bool
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if codexLineLooksProxyBlocked(line) {
			sawProxyMu.Lock()
			sawProxyBlock = true
			sawProxyMu.Unlock()
			continue
		}
		if codexLineSignalsSessionLimit(line) {
			sawSessionLimit = true
		}
		if text, ok := extractCodexAssistantText(line); ok {
			stream.Push(encodeData(text))
		}
	}

	waitErr := cmd.Wait()
	stderrWg.Wait()

	sawProxyMu.Lock()
	proxyBlocked := sawProxyBlock
	sawProxyMu.Unlock()

	if proxyBlocked {
		observeError(metrics, AgentIDCodex)
		stream.Push(map[string]any{
			"t":     "err",
			"agent": AgentIDCodex,
			"msg":   "codex: blocked by Zscaler/proxy — open ChatGPT in a browser, approve the security prompt, then retry",
		})
	} else if sawSessionLimit {
		observeError(metrics, AgentIDCodex)
		stream.Push(map[string]any{
			"t":     "err",
			"agent": AgentIDCodex,
			"msg":   "codex: session limit reached",
		})
	} else if waitErr != nil {
		observeError(metrics, AgentIDCodex)
		stream.Push(map[string]any{"t": "err", "msg": "codex exited: " + waitErr.Error()})
	}
	stream.Push(map[string]any{"t": "chunk_end", "cost_usd": 0.0})
}

// buildCodexCmdArgs assembles the codex CLI argv. Always begins with
// `exec --json --color never --skip-git-repo-check`, then merges any
// caller-supplied extra flags, then injects safe agentic defaults
// (--sandbox workspace-write --ask-for-approval never) only when the
// caller has not already set them, and finally appends `-- <prompt>`.
//
// Without these defaults, recent codex CLI versions run `exec --json` in
// read-only / on-request mode and silently refuse tool execution. This
// mirrors the buildCodexArgs fix landed in internal/kitchen/adapter/
// codex.go for the kitchen path.
func buildCodexCmdArgs(prompt string, extra []string) []string {
	args := []string{"exec", "--json", "--color", "never", "--skip-git-repo-check"}
	args = append(args, extra...)
	if !codexHasFlag(extra, "--sandbox") {
		args = append(args, "--sandbox", "workspace-write")
	}
	if !codexHasFlag(extra, "--ask-for-approval") {
		args = append(args, "--ask-for-approval", "never")
	}
	args = append(args, "--", prompt)
	return args
}

func codexHasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// codexLineLooksProxyBlocked returns true when a stdout/stderr line carries
// the signature of a Zscaler / corporate-proxy interception of the codex
// CLI's connection to chatgpt.com. Mirrors REPL's check.
func codexLineLooksProxyBlocked(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "zscaler") ||
		strings.Contains(lower, "internet security by zscaler") ||
		strings.Contains(lower, "unexpected status 403 forbidden") ||
		strings.Contains(lower, "307 temporary redirect") ||
		(strings.Contains(lower, "chatgpt.com/backend-api/codex") && strings.Contains(lower, "failed to connect"))
}

// codexLineSignalsSessionLimit returns true when a stdout JSON event line
// indicates that the codex session has hit its turn or context limit.
// Mirrors REPL's runner_codex.go check.
func codexLineSignalsSessionLimit(line string) bool {
	var evt codexJSONEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return false
	}
	switch evt.Type {
	case "max_turns", "context_length_exceeded":
		return true
	case "error":
		lower := strings.ToLower(codexFirstNonEmpty(evt.Message, evt.Content, evt.Text))
		return strings.Contains(lower, "context") || strings.Contains(lower, "limit")
	}
	return false
}

// extractCodexAssistantText returns the assistant text from a codex
// JSON event line, or false if the line is not an assistant-bearing
// event with non-empty text.
func extractCodexAssistantText(line string) (string, bool) {
	var evt codexJSONEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return "", false
	}

	switch evt.Type {
	case "message", "assistant", "text", "response.output_text.done", "agent_message":
		out := codexFirstNonEmpty(evt.Content, evt.Message, evt.Text)
		return out, out != ""
	case "response.output_text.delta":
		return evt.Delta, evt.Delta != ""
	case "item.completed":
		var item codexJSONItem
		if len(evt.Item) > 0 && json.Unmarshal(evt.Item, &item) == nil {
			if item.ItemType == "assistant_message" || item.Type == "assistant_message" || item.Type == "message" {
				out := codexFirstNonEmpty(item.Text, item.Content, item.Message)
				return out, out != ""
			}
		}
	}

	if len(evt.Error) > 0 {
		var payload map[string]any
		if json.Unmarshal(evt.Error, &payload) == nil {
			if msg, ok := payload["message"].(string); ok && msg != "" {
				return msg, true
			}
		}
	}
	return "", false
}

func codexFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
