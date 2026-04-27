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
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	IsError      bool    `json:"is_error,omitempty"`
}

type claudeStreamMessage struct {
	Content []claudeStreamContent `json:"content,omitempty"`
}

type claudeStreamContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
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
// Lifecycle:
//   - One subprocess per prompt; the session stays alive across prompts.
//   - When `input` is closed, RunClaude pushes {"t":"end"} and returns.
//   - The caller (AgentRegistry) is responsible for Close()ing the stream.
func RunClaude(ctx context.Context, input <-chan []byte, stream Pusher) {
	for prompt := range input {
		if stream == nil {
			continue
		}
		runClaudeOnce(ctx, prompt, stream)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

func runClaudeOnce(parent context.Context, prompt []byte, stream Pusher) {
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
		stream.Push(map[string]any{"t": "err", "msg": "claude stdout pipe: " + err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stream.Push(map[string]any{"t": "err", "msg": "claude stderr pipe: " + err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
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

	var lastCost float64
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if text, ok := extractAssistantText(line); ok {
			stream.Push(encodeData(text))
			continue
		}
		if cost, ok := extractResultCost(line); ok {
			lastCost = cost
		}
	}

	if err := cmd.Wait(); err != nil {
		stream.Push(map[string]any{"t": "err", "msg": "claude exited: " + err.Error()})
	}
	stream.Push(map[string]any{"t": "chunk_end", "cost_usd": lastCost})
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

// extractResultCost returns total_cost_usd from a successful `result` event,
// or false if the line is not a result event (or is an error result).
func extractResultCost(line string) (float64, bool) {
	var evt claudeStreamEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return 0, false
	}
	if evt.Type != "result" || evt.IsError {
		return 0, false
	}
	return evt.TotalCostUSD, true
}

// encodeData wraps a text fragment in the {"t":"data","b64":...} shape
// the bridge protocol expects.
func encodeData(text string) map[string]any {
	return map[string]any{
		"t":   "data",
		"b64": base64.StdEncoding.EncodeToString([]byte(text)),
	}
}
