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

// RunLocal targets an OpenAI-compatible chat-completions endpoint at
// $MILLIWAYS_LOCAL_ENDPOINT (default http://localhost:8765/v1). The
// default endpoint matches what scripts/install_local.sh configures and
// what `milliwaysctl local install-server` provisions. Compatible
// backends include llama.cpp's `llama-server`, `llama-swap`, vLLM,
// LMStudio, and Ollama's `/v1` shim.
//
// This is a deliberate change from the previous Ollama-native
// (`/api/chat`) implementation: every other piece of the local-model
// stack in this repo (REPL runner, milliwaysctl local subcommands,
// install_local.sh, install_local_swap.sh) targets the OpenAI-compatible
// path at port 8765. Aligning the daemon path here removes the silent
// misconfiguration where the daemon would talk to a different
// backend/port from what the user installed.

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

const (
	localDefaultEndpoint = "http://localhost:8765/v1"
	localDefaultModel    = "qwen2.5-coder-1.5b"
	localTimeout         = 5 * time.Minute
)

// RunLocal is the daemon-side local-model session loop. Drains the input
// channel; for each prompt issues one chat-completion request to the
// configured backend and streams content deltas as
// {"t":"data","b64":...} events. Closing the input channel pushes a
// final {"t":"end"}.
func RunLocal(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
	for prompt := range input {
		if stream == nil {
			continue
		}
		runLocalOnce(ctx, prompt, stream, metrics)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

type localChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type localChatRequest struct {
	Model    string             `json:"model"`
	Stream   bool               `json:"stream"`
	Messages []localChatMessage `json:"messages"`
}

type localChatChoice struct {
	Delta struct {
		Content string `json:"content"`
	} `json:"delta"`
	Message *struct {
		Content string `json:"content"`
	} `json:"message,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type localChatChunk struct {
	Choices []localChatChoice `json:"choices"`
	Usage   *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

func runLocalOnce(parent context.Context, prompt []byte, stream Pusher, metrics MetricsObserver) {
	endpoint := strings.TrimRight(os.Getenv("MILLIWAYS_LOCAL_ENDPOINT"), "/")
	if endpoint == "" {
		endpoint = localDefaultEndpoint
	}
	model := strings.TrimSpace(os.Getenv("MILLIWAYS_LOCAL_MODEL"))
	if model == "" {
		model = localDefaultModel
	}
	apiKey := strings.TrimSpace(os.Getenv("MILLIWAYS_LOCAL_API_KEY"))

	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		return
	}

	payload := localChatRequest{
		Model:  model,
		Stream: true,
		Messages: []localChatMessage{
			{Role: "user", Content: text},
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		observeError(metrics, AgentIDLocal)
		stream.Push(map[string]any{"t": "err", "msg": "local marshal: " + err.Error()})
		return
	}

	url := endpoint + "/chat/completions"
	ctx, cancel := context.WithTimeout(parent, localTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		observeError(metrics, AgentIDLocal)
		stream.Push(map[string]any{"t": "err", "msg": "local request: " + err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		observeError(metrics, AgentIDLocal)
		stream.Push(map[string]any{
			"t":   "err",
			"msg": fmt.Sprintf("local connect %s: %v (is the backend running? `milliwaysctl local install-server` to bootstrap)", url, err),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		observeError(metrics, AgentIDLocal)
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		stream.Push(map[string]any{
			"t":   "err",
			"msg": fmt.Sprintf("local API %s: %s", resp.Status, strings.TrimSpace(string(body))),
		})
		return
	}

	usage := streamLocalSSE(ctx, resp.Body, stream)
	if usage != nil {
		observeTokens(metrics, AgentIDLocal, usage.promptTokens, usage.completionTokens, 0)
	}
	chunkEnd := map[string]any{"t": "chunk_end", "cost_usd": 0.0}
	if usage != nil {
		chunkEnd["input_tokens"] = usage.promptTokens
		chunkEnd["output_tokens"] = usage.completionTokens
		chunkEnd["total_tokens"] = usage.totalTokens
	}
	stream.Push(chunkEnd)
}

type localUsage struct {
	promptTokens     int
	completionTokens int
	totalTokens      int
}

// streamLocalSSE reads SSE / NDJSON lines, pushes content deltas as
// {"t":"data","b64":...} events, and returns the final usage block (or
// nil if none was emitted).
func streamLocalSSE(ctx context.Context, r io.Reader, stream Pusher) *localUsage {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	var usage *localUsage
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return usage
		default:
		}

		line := scanner.Text()
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

		var chunk localChatChunk
		if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = &localUsage{
				promptTokens:     chunk.Usage.PromptTokens,
				completionTokens: chunk.Usage.CompletionTokens,
				totalTokens:      chunk.Usage.TotalTokens,
			}
		}
		for _, choice := range chunk.Choices {
			content := choice.Delta.Content
			if content == "" && choice.Message != nil {
				content = choice.Message.Content
			}
			if content != "" {
				stream.Push(encodeData(content))
			}
		}
	}
	return usage
}
