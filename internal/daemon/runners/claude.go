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
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Pusher is the minimal contract the runner needs from a *daemon.Stream:
// a single Push(event any) method. Defined here to avoid a cyclic import.
type Pusher interface {
	Push(event any)
}

// claudeStreamEvent mirrors the subset of the claude --output-format
// stream-json protocol we care about for the daemon-side bridge. Cribbed
// (without lifting wholesale) from the legacy REPL runner.
type claudeStreamEvent struct {
	Type    string               `json:"type"`
	Message *claudeStreamMessage `json:"message,omitempty"`

	// rate_limit_event fields
	RateLimitInfo *claudeStreamRateLimit `json:"rate_limit_info,omitempty"`

	// result fields
	TotalCostUSD float64            `json:"total_cost_usd,omitempty"`
	IsError      bool               `json:"is_error,omitempty"`
	Usage        *claudeStreamUsage `json:"usage,omitempty"`
}

// claudeStreamRateLimit carries the rate-limit status surfaced by claude
// CLI when the user is approaching or has hit a session/quota limit. The
// Status string is provider-defined (e.g., "approaching", "throttled",
// "exhausted") and ResetsAt is a Unix timestamp at which the limit lifts
// (zero if unknown).
type claudeStreamRateLimit struct {
	Status   string `json:"status,omitempty"`
	ResetsAt int64  `json:"resetsAt,omitempty"`
}

type claudeStreamMessage struct {
	Content []claudeStreamContent `json:"content,omitempty"`
}

type claudeStreamContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// claudeStreamUsage carries the input/output token accounting block
// emitted on result events. We surface tokens_in / tokens_out into the
// metrics rollup; cache fields are tracked only for parity with the
// REPL runner today but are not pushed as metrics.
type claudeStreamUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	CacheRead    int `json:"cache_read_input_tokens,omitempty"`
	CacheWrite   int `json:"cache_creation_input_tokens,omitempty"`
}

// claudeResult is a small bundle of per-response numbers extracted from
// a successful `result` event. Returned by extractResult so RunClaude
// can wire all four numbers (cost + tokens) into the metrics observer.
// Cache tokens are surfaced separately so chunk_end can carry them
// without inflating the observed input/output counters.
type claudeResult struct {
	costUSD          float64
	inputTokens      int
	outputTokens     int
	cacheReadTokens  int
	cacheWriteTokens int
}

// claudeBinary is the executable name; var (not const) so tests can swap it.
var claudeBinary = "claude"

// claudeTimeout caps a single agent.send call's subprocess lifetime.
const claudeTimeout = 5 * time.Minute

// RunClaude is the daemon-side claude session loop. It reads prompts from
// `input`, spawns one `claude --print --output-format stream-json` per
// prompt, decodes the assistant text from each NDJSON line, and pushes
// {"t":"data","b64":...} events to the stream. After the subprocess
// exits a final {"t":"chunk_end","cost_usd":N} marks end-of-response.
//
// Per-response usage (tokens, cost) is observed into `metrics` if non-nil;
// non-zero subprocess exits and pipe failures push an error_count tick.
//
// Lifecycle:
//   - One subprocess per prompt; the session stays alive across prompts.
//   - When `input` is closed, RunClaude pushes {"t":"end"} and returns.
//   - The caller (AgentRegistry) is responsible for Close()ing the stream.
func RunClaude(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
	for prompt := range input {
		if stream == nil {
			continue
		}
		runClaudeOnce(ctx, prompt, stream, metrics)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

func runClaudeOnce(parent context.Context, prompt []byte, stream Pusher, metrics MetricsObserver) {
	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		return
	}

	// chunkEnd is updated with real cost/tokens before the function returns;
	// the defer guarantees it is pushed on every exit path including early errors.
	chunkEnd := map[string]any{"t": "chunk_end", "cost_usd": 0.0}
	spanCtx, span := startDispatchSpan(parent, AgentIDClaude, "")
	ctx, cancel := context.WithTimeout(spanCtx, claudeTimeout)
	defer cancel()
	defer func() { stream.Push(chunkEnd) }()

	cwd, _ := os.Getwd()
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
	}
	if cwd != "" {
		args = append(args, "--add-dir", cwd)
	}
	args = append(args, text)
	cmd := exec.CommandContext(ctx, claudeBinary, args...)
	cmd.Env = safeRunnerEnv()
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		observeError(metrics, AgentIDClaude)
		stream.Push(map[string]any{"t": "err", "msg": "claude: failed to start — try again"})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		observeError(metrics, AgentIDClaude)
		stream.Push(map[string]any{"t": "err", "msg": "claude: failed to start — try again"})
		return
	}
	if err := cmd.Start(); err != nil {
		observeError(metrics, AgentIDClaude)
		stream.Push(map[string]any{"t": "err", "msg": "claude: could not start — " + installHint("claude")})
		return
	}

	// Capture stderr so we can inspect for session-limit signals once the
	// subprocess exits. Lines also go to slog.Debug for ad-hoc debugging.
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
			slog.Debug("claude stderr", "line", line)
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	var lastResult claudeResult
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if text, ok := extractAssistantText(line); ok {
			stream.Push(encodeData(text))
			continue
		}
		if info, ok := extractRateLimitEvent(line); ok {
			ev := map[string]any{
				"t":      "rate_limit",
				"agent":  AgentIDClaude,
				"status": info.Status,
			}
			if info.ResetsAt > 0 {
				ev["resets_at"] = info.ResetsAt
			}
			stream.Push(ev)
			continue
		}
		if r, ok := extractResult(line); ok {
			lastResult = r
		}
	}

	waitErr := cmd.Wait()
	stderrWg.Wait()

	stderrMu.Lock()
	lines := append([]string(nil), stderrLines...)
	stderrMu.Unlock()

	if claudeStderrSignalsLimit(lines) {
		observeError(metrics, AgentIDClaude)
		stream.Push(map[string]any{
			"t":     "err",
			"agent": AgentIDClaude,
			"msg":   "claude: session limit reached",
		})
	} else if waitErr != nil {
		observeError(metrics, AgentIDClaude)
		stream.Push(map[string]any{"t": "err", "agent": AgentIDClaude, "msg": exitMsg("claude", waitErr, lines)})
	}
	observeTokens(metrics, AgentIDClaude, lastResult.inputTokens, lastResult.outputTokens, lastResult.costUSD)
	endDispatchSpan(span, lastResult.inputTokens, lastResult.outputTokens, lastResult.costUSD, "")
	chunkEnd["cost_usd"] = lastResult.costUSD
	chunkEnd["input_tokens"] = lastResult.inputTokens
	chunkEnd["output_tokens"] = lastResult.outputTokens
	if lastResult.cacheReadTokens > 0 {
		chunkEnd["cache_read_tokens"] = lastResult.cacheReadTokens
	}
	if lastResult.cacheWriteTokens > 0 {
		chunkEnd["cache_write_tokens"] = lastResult.cacheWriteTokens
	}
}

// extractAssistantText returns the concatenated text of all `text` content
// blocks in an `assistant` event line, or false if the line is not an
// assistant event with non-empty text.
func extractAssistantText(line string) (string, bool) {
	var evt claudeStreamEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return "", false
	}
	if evt.Type != "assistant" || evt.Message == nil {
		return "", false
	}
	var parts []string
	for _, c := range evt.Message.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	out := strings.Join(parts, "")
	return out, out != ""
}

// extractResult returns the per-response cost + token counts from a
// successful `result` event, or false if the line is not a (non-error)
// result event.
func extractResult(line string) (claudeResult, bool) {
	var evt claudeStreamEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return claudeResult{}, false
	}
	if evt.Type != "result" || evt.IsError {
		return claudeResult{}, false
	}
	r := claudeResult{costUSD: evt.TotalCostUSD}
	if evt.Usage != nil {
		r.inputTokens = evt.Usage.InputTokens
		r.outputTokens = evt.Usage.OutputTokens
		r.cacheReadTokens = evt.Usage.CacheRead
		r.cacheWriteTokens = evt.Usage.CacheWrite
	}
	return r, true
}

// extractRateLimitEvent decodes a rate_limit_event line. Returns the
// populated claudeStreamRateLimit and true if the line is a non-nil
// rate_limit_event; (zero, false) otherwise.
func extractRateLimitEvent(line string) (claudeStreamRateLimit, bool) {
	var evt claudeStreamEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return claudeStreamRateLimit{}, false
	}
	if evt.Type != "rate_limit_event" || evt.RateLimitInfo == nil {
		return claudeStreamRateLimit{}, false
	}
	return *evt.RateLimitInfo, true
}

// claudeStderrSignalsLimit returns true when any captured stderr line
// indicates a context-window / session-limit / context-length / "too
// long" exhaustion. Mirrors REPL's runner_claude.go check; intentionally
// narrower than the comprehensive set used for gemini/pool because
// claude CLI surfaces most quota info in-band as rate_limit_event rather
// than on stderr.
func claudeStderrSignalsLimit(lines []string) bool {
	for _, l := range lines {
		lower := strings.ToLower(l)
		if strings.Contains(lower, "context window") ||
			strings.Contains(lower, "session limit") ||
			strings.Contains(lower, "context_length") ||
			strings.Contains(lower, "context_length_exceeded") ||
			strings.Contains(lower, "too long") {
			return true
		}
	}
	return false
}

// extractResultCost returns total_cost_usd from a successful `result`
// event, or false if the line is not a (non-error) result event.
// Retained for compatibility with existing tests; new code should use
// extractResult to also surface token counts.
func extractResultCost(line string) (float64, bool) {
	r, ok := extractResult(line)
	if !ok {
		return 0, false
	}
	return r.costUSD, true
}

// encodeData wraps a text fragment in the {"t":"data","b64":...} shape
// the bridge protocol expects.
func encodeData(text string) map[string]any {
	return map[string]any{
		"t":   "data",
		"b64": base64.StdEncoding.EncodeToString([]byte(text)),
	}
}

// encodeThinking wraps model reasoning/progress fragments in a separate
// stream event so clients can show activity without mixing it into the
// final assistant answer.
func encodeThinking(text string) map[string]any {
	return map[string]any{
		"t":   "thinking",
		"b64": base64.StdEncoding.EncodeToString([]byte(text)),
	}
}
