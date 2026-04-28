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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// RunMiniMax drains the input channel; for each batch of bytes treated as
// a prompt, it calls the MiniMax chat completion API with streaming and
// pushes each delta as {"t":"data","b64":...} events. On API completion it
// pushes {"t":"chunk_end","cost_usd":N}. When the input channel is closed
// it pushes {"t":"end"}.
//
// API endpoint, request shape, and SSE parsing are cribbed (without
// lifting wholesale) from internal/repl/runner_minimax.go.
//
// Auth: requires MINIMAX_API_KEY env var. If unset at the start of a send,
// pushes {"t":"err","code":-32005,"msg":"MINIMAX_API_KEY not set"} and
// continues draining input (subsequent sends will see the same err) until
// the channel closes.
//
// URL override: MINIMAX_API_URL is honoured for tests / proxy setups.
//
// Per-response usage (prompt/completion tokens + computed cost) is observed
// into `metrics` if non-nil; auth-missing, marshal/transport failures, and
// non-2xx responses each push an error_count tick.
func RunMiniMax(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
	for prompt := range input {
		if stream == nil {
			continue
		}
		runMiniMaxOnce(ctx, prompt, stream, metrics)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

// minimaxTimeout caps a single agent.send call's HTTP request lifetime.
const minimaxTimeout = 5 * time.Minute

// minimaxDefaultURL is the production MiniMax chat completion endpoint.
// Mirrors internal/repl/runner_minimax.go's NewMinimaxRunner default.
const minimaxDefaultURL = "https://api.minimax.io/v1/text/chatcompletion_v2"

// minimaxDefaultModel mirrors internal/repl/runner_minimax.go.
const minimaxDefaultModel = "MiniMax-M2.7"

// minimaxRequestMessage matches the OpenAI-compatible {role, content} shape.
type minimaxRequestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// minimaxStreamDelta carries the streamed text fragment from one SSE event.
type minimaxStreamDelta struct {
	Content string `json:"content"`
}

// minimaxStreamChoice wraps delta + non-streaming fallback message.
type minimaxStreamChoice struct {
	Delta   minimaxStreamDelta  `json:"delta"`
	Message *minimaxStreamDelta `json:"message,omitempty"`
}

// minimaxStreamUsage is the standard OpenAI-style token accounting block.
type minimaxStreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// minimaxStreamChunk is one decoded SSE event payload.
type minimaxStreamChunk struct {
	Choices []minimaxStreamChoice `json:"choices"`
	Usage   *minimaxStreamUsage   `json:"usage,omitempty"`
}

// runMiniMaxOnce performs a single chat completion request for one prompt.
func runMiniMaxOnce(parent context.Context, prompt []byte, stream Pusher, metrics MetricsObserver) {
	apiKey := strings.TrimSpace(os.Getenv("MINIMAX_API_KEY"))
	if apiKey == "" {
		observeError(metrics, AgentIDMiniMax)
		stream.Push(map[string]any{
			"t":    "err",
			"code": -32005,
			"msg":  "MINIMAX_API_KEY not set",
		})
		return
	}

	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		return
	}

	url := strings.TrimSpace(os.Getenv("MINIMAX_API_URL"))
	if url == "" {
		url = minimaxDefaultURL
	}
	model := strings.TrimSpace(os.Getenv("MINIMAX_MODEL"))
	if model == "" {
		model = minimaxDefaultModel
	}

	ctx, cancel := context.WithTimeout(parent, minimaxTimeout)
	defer cancel()

	payload := map[string]any{
		"model": model,
		"messages": []minimaxRequestMessage{
			{Role: "user", Content: text},
		},
		"stream": true,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		observeError(metrics, AgentIDMiniMax)
		stream.Push(map[string]any{"t": "err", "msg": "minimax marshal: " + err.Error()})
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		observeError(metrics, AgentIDMiniMax)
		stream.Push(map[string]any{"t": "err", "msg": "minimax request: " + err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: minimaxTimeout}
	resp, err := client.Do(req)
	if err != nil {
		observeError(metrics, AgentIDMiniMax)
		stream.Push(map[string]any{"t": "err", "msg": "minimax do: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		observeError(metrics, AgentIDMiniMax)
		body, _ := io.ReadAll(resp.Body)
		stream.Push(map[string]any{
			"t":   "err",
			"msg": fmt.Sprintf("minimax API %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
		})
		return
	}

	usage := streamMiniMaxSSE(ctx, resp.Body, stream)
	cost := minimaxCostUSD(usage)
	if usage != nil {
		observeTokens(metrics, AgentIDMiniMax, usage.PromptTokens, usage.CompletionTokens, cost)
	}
	push := map[string]any{
		"t":        "chunk_end",
		"cost_usd": cost,
	}
	if usage != nil {
		push["input_tokens"] = usage.PromptTokens
		push["output_tokens"] = usage.CompletionTokens
		push["total_tokens"] = usage.TotalTokens
	}
	stream.Push(push)
}

// streamMiniMaxSSE reads SSE / NDJSON lines from r, decodes each chunk,
// and pushes one {"t":"data","b64":...} event per non-empty content delta.
// Returns the final usage block (may be nil if the API didn't include one).
func streamMiniMaxSSE(ctx context.Context, r io.Reader, stream Pusher) *minimaxStreamUsage {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	var usage *minimaxStreamUsage
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return usage
		default:
		}

		line := scanner.Text()

		// Accept SSE ("data: {...}") and bare NDJSON ("{...}") lines.
		var jsonData string
		switch {
		case strings.HasPrefix(line, "data: "):
			jsonData = strings.TrimPrefix(line, "data: ")
			if jsonData == "[DONE]" {
				return usage
			}
		case strings.HasPrefix(line, "{"):
			jsonData = line
		default:
			continue
		}

		var chunk minimaxStreamChunk
		if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			delta := choice.Delta
			if choice.Message != nil && delta.Content == "" {
				delta = *choice.Message
			}
			if delta.Content != "" {
				stream.Push(encodeData(delta.Content))
			}
		}
	}
	return usage
}

// minimaxCostUSD computes a coarse USD cost from token usage. MiniMax's
// public price card hovers around $0.30/$1.20 per million in/out tokens
// for the M2 family; we use those as a stable default. If usage is nil we
// return 0 (the daemon contract permits a zero cost).
func minimaxCostUSD(u *minimaxStreamUsage) float64 {
	if u == nil {
		return 0
	}
	const inputUSDPerMTok = 0.30
	const outputUSDPerMTok = 1.20
	in := float64(u.PromptTokens) * inputUSDPerMTok / 1_000_000
	out := float64(u.CompletionTokens) * outputUSDPerMTok / 1_000_000
	return in + out
}
