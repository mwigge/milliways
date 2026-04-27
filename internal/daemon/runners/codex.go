package runners

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"strings"
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
// Lifecycle:
//   - One subprocess per prompt; the session stays alive across prompts.
//   - When `input` is closed, RunCodex pushes {"t":"end"} and returns.
//   - The caller (AgentRegistry) is responsible for Close()ing the stream.
func RunCodex(ctx context.Context, input <-chan []byte, stream Pusher) {
	for prompt := range input {
		if stream == nil {
			continue
		}
		runCodexOnce(ctx, prompt, stream)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

func runCodexOnce(parent context.Context, prompt []byte, stream Pusher) {
	ctx, cancel := context.WithTimeout(parent, codexTimeout)
	defer cancel()

	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		return
	}

	cmd := exec.CommandContext(ctx, codexBinary,
		"exec",
		"--json",
		"--color", "never",
		"--skip-git-repo-check",
		"--",
		text,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stream.Push(map[string]any{"t": "err", "msg": "codex stdout pipe: " + err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stream.Push(map[string]any{"t": "err", "msg": "codex stderr pipe: " + err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		stream.Push(map[string]any{"t": "err", "msg": "codex start: " + err.Error()})
		return
	}

	// Drain stderr to avoid blocking the child; log lines for debugging.
	go func() {
		s := bufio.NewScanner(stderr)
		s.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for s.Scan() {
			slog.Debug("codex stderr", "line", s.Text())
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if text, ok := extractCodexAssistantText(line); ok {
			stream.Push(encodeData(text))
		}
	}

	if err := cmd.Wait(); err != nil {
		stream.Push(map[string]any{"t": "err", "msg": "codex exited: " + err.Error()})
	}
	stream.Push(map[string]any{"t": "chunk_end", "cost_usd": 0.0})
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
