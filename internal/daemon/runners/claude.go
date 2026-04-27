package runners

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// Pusher is the minimal contract the runner needs from a *daemon.Stream:
// a single Push(event any) method. Defined here to avoid a cyclic import.
type Pusher interface {
	Push(event any)
}

// claudeStreamEvent mirrors the subset of the claude --output-format
// stream-json protocol we care about for the daemon-side bridge. Cribbed
// (without lifting wholesale) from internal/repl/runner_claude.go.
type claudeStreamEvent struct {
	Type    string               `json:"type"`
	Message *claudeStreamMessage `json:"message,omitempty"`

	// result fields
	TotalCostUSD float64            `json:"total_cost_usd,omitempty"`
	IsError      bool               `json:"is_error,omitempty"`
	Usage        *claudeStreamUsage `json:"usage,omitempty"`
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
type claudeResult struct {
	costUSD      float64
	inputTokens  int
	outputTokens int
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
	ctx, cancel := context.WithTimeout(parent, claudeTimeout)
	defer cancel()

	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		return
	}

	cmd := exec.CommandContext(ctx, claudeBinary,
		"--print",
		"--output-format", "stream-json",
		"--verbose",
		text,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		observeError(metrics, AgentIDClaude)
		stream.Push(map[string]any{"t": "err", "msg": "claude stdout pipe: " + err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		observeError(metrics, AgentIDClaude)
		stream.Push(map[string]any{"t": "err", "msg": "claude stderr pipe: " + err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		observeError(metrics, AgentIDClaude)
		stream.Push(map[string]any{"t": "err", "msg": "claude start: " + err.Error()})
		return
	}

	// Drain stderr to avoid blocking the child; log lines for debugging.
	go func() {
		s := bufio.NewScanner(stderr)
		s.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for s.Scan() {
			slog.Debug("claude stderr", "line", s.Text())
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
		if r, ok := extractResult(line); ok {
			lastResult = r
		}
	}

	if err := cmd.Wait(); err != nil {
		observeError(metrics, AgentIDClaude)
		stream.Push(map[string]any{"t": "err", "msg": "claude exited: " + err.Error()})
	}
	observeTokens(metrics, AgentIDClaude, lastResult.inputTokens, lastResult.outputTokens, lastResult.costUSD)
	stream.Push(map[string]any{"t": "chunk_end", "cost_usd": lastResult.costUSD})
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
	}
	return r, true
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
