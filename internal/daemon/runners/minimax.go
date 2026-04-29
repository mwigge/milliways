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
	"sync"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/provider"
	"github.com/mwigge/milliways/internal/tools"
)

// RunMiniMax drains the input channel; for each batch of bytes treated as
// a prompt, it drives a chat-completion + tool-loop turn cycle against the
// MiniMax API. Per-delta content events stream as {"t":"data","b64":...};
// each completed dispatch ends with {"t":"chunk_end","cost_usd":N,
// "input_tokens":...,"output_tokens":...,"total_tokens":...}. Closing the
// input channel pushes a final {"t":"end"}.
//
// Tool-loop behaviour:
//   - A `tools.Registry` (default: `tools.NewBuiltInRegistry()`) is offered
//     to the model on every request via the OpenAI tool-call protocol.
//   - When the model requests tool calls, the daemon executes them via the
//     registry and re-issues the request with assistant + tool messages
//     appended. The shared `RunAgenticLoop` helper drives the cycle and
//     enforces a 10-turn safety bound.
//   - Set `MINIMAX_TOOLS=off` to disable tool exposure (chat-only mode).
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
const minimaxDefaultURL = "https://api.minimax.io/v1/text/chatcompletion_v2"

// minimaxDefaultModel mirrors internal/repl/runner_minimax.go.
const minimaxDefaultModel = "MiniMax-M2.7"

// minimaxSystemPrompt is the standard guidance prepended to every
// dispatch. Steers the model toward tool use and concise markdown output;
// req.Rules from CLAUDE.md is intentionally not forwarded because it
// contains Claude Code-specific orchestration instructions that confuse
// raw API models.
const minimaxSystemPrompt = "You are a helpful, concise assistant running inside a developer terminal. " +
	"Format responses in plain markdown (headers, code fences, bullet lists). " +
	"When a task requires reading or modifying files, running shell commands, or " +
	"fetching URLs, call the appropriate tool rather than describing what you would do. " +
	"Be direct and precise; avoid unnecessary preamble or filler."

// minimaxStreamUsage is the standard OpenAI-style token accounting block.
type minimaxStreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// minimaxStreamDelta carries per-chunk content + tool-call fragments.
type minimaxStreamDelta struct {
	Content   string                  `json:"content"`
	ToolCalls []minimaxStreamToolCall `json:"tool_calls,omitempty"`
}

// minimaxStreamToolCall mirrors the OpenAI streaming tool_call delta shape.
// Streamed deltas may split a single call's id/name/arguments across chunks
// so the receiver must accumulate by Index.
type minimaxStreamToolCall struct {
	ID       string                    `json:"id,omitempty"`
	Index    int                       `json:"index"`
	Type     string                    `json:"type,omitempty"`
	Function minimaxStreamToolFunction `json:"function"`
}

type minimaxStreamToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// minimaxStreamChoice wraps delta + non-streaming fallback message.
type minimaxStreamChoice struct {
	Delta        minimaxStreamDelta  `json:"delta"`
	Message      *minimaxStreamDelta `json:"message,omitempty"`
	FinishReason string              `json:"finish_reason,omitempty"`
}

// minimaxStreamChunk is one decoded SSE event payload.
type minimaxStreamChunk struct {
	Choices []minimaxStreamChoice `json:"choices"`
	Usage   *minimaxStreamUsage   `json:"usage,omitempty"`
}

// minimaxToolRegistry lets tests inject a custom registry. Production code
// builds the default registry on demand from `tools.NewBuiltInRegistry()`.
// Setting `MINIMAX_TOOLS=off` disables tool exposure entirely (returns nil).
var (
	minimaxToolRegistryMu       sync.RWMutex
	minimaxToolRegistryOverride *tools.Registry
)

func minimaxRegistry() *tools.Registry {
	if strings.EqualFold(os.Getenv("MINIMAX_TOOLS"), "off") {
		return nil
	}
	minimaxToolRegistryMu.RLock()
	r := minimaxToolRegistryOverride
	minimaxToolRegistryMu.RUnlock()
	if r != nil {
		return r
	}
	return tools.NewBuiltInRegistry()
}

// withMinimaxToolRegistry installs `r` as the registry seen by RunMiniMax for
// the duration of the test. Restored automatically on test cleanup.
func withMinimaxToolRegistry(t *testing.T, r *tools.Registry) {
	t.Helper()
	minimaxToolRegistryMu.Lock()
	prev := minimaxToolRegistryOverride
	minimaxToolRegistryOverride = r
	minimaxToolRegistryMu.Unlock()
	t.Cleanup(func() {
		minimaxToolRegistryMu.Lock()
		minimaxToolRegistryOverride = prev
		minimaxToolRegistryMu.Unlock()
	})
}

// runMiniMaxOnce drives one prompt to completion, pushing per-delta content
// events to `stream`, executing tool calls inline via RunAgenticLoop, and
// emitting a final chunk_end with token + cost totals.
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

	registry := minimaxRegistry()
	messages := []Message{
		{Role: RoleSystem, Content: minimaxSystemPrompt},
		{Role: RoleUser, Content: text},
	}
	client := &minimaxClient{
		http:   &http.Client{Timeout: minimaxTimeout},
		url:    url,
		apiKey: apiKey,
		model:  model,
		stream: stream,
	}

	result, err := RunAgenticLoop(ctx, client, registry, &messages, LoopOptions{
		SessionID: AgentIDMiniMax,
	})
	if err != nil {
		observeError(metrics, AgentIDMiniMax)
		stream.Push(map[string]any{"t": "err", "msg": "minimax: " + err.Error()})
		return
	}

	usage := &minimaxStreamUsage{
		PromptTokens:     result.TotalUsage.PromptTokens,
		CompletionTokens: result.TotalUsage.CompletionTokens,
		TotalTokens:      result.TotalUsage.TotalTokens,
	}
	cost := minimaxCostUSD(usage)
	if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		observeTokens(metrics, AgentIDMiniMax, usage.PromptTokens, usage.CompletionTokens, cost)
	}
	push := map[string]any{
		"t":             "chunk_end",
		"cost_usd":      cost,
		"input_tokens":  usage.PromptTokens,
		"output_tokens": usage.CompletionTokens,
		"total_tokens":  usage.TotalTokens,
	}
	if result.StoppedAt == StopReasonMaxTurns {
		push["max_turns_hit"] = true
	}
	stream.Push(push)
}

// minimaxClient implements the runners.Client interface for RunAgenticLoop.
// Each Send issues one chat-completion request, streams content deltas to
// the daemon `stream`, accumulates tool-call argument fragments by index,
// and returns a TurnResult on the SSE stream's terminal event.
type minimaxClient struct {
	http   *http.Client
	url    string
	apiKey string
	model  string
	stream Pusher
}

func (c *minimaxClient) Send(ctx context.Context, messages []Message, toolDefs []provider.ToolDef) (TurnResult, error) {
	payload := buildMinimaxPayload(c.model, messages, toolDefs)
	body, err := json.Marshal(payload)
	if err != nil {
		return TurnResult{}, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return TurnResult{}, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return TurnResult{}, fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return TurnResult{}, fmt.Errorf("API %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	return streamMinimaxTurn(ctx, resp.Body, c.stream)
}

// buildMinimaxPayload converts the agentic-loop Messages slice into the
// OpenAI-compatible MiniMax payload, including the optional tools array.
//
// Role-specific shaping:
//   - assistant turns with ToolCalls become {role:"assistant", content:null,
//     tool_calls:[{id,type,function:{name,arguments}}]} per the OpenAI spec
//   - tool turns become {role:"tool", tool_call_id, content}
//   - everything else passes through as {role, content}
func buildMinimaxPayload(model string, messages []Message, toolDefs []provider.ToolDef) map[string]any {
	apiMessages := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case RoleAssistant:
			if len(m.ToolCalls) > 0 {
				tcs := make([]map[string]any, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					tcs = append(tcs, map[string]any{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]any{
							"name":      tc.Name,
							"arguments": tc.Args,
						},
					})}
				apiMessages = append(apiMessages, map[string]any{
					"role":       "assistant",
					"content":    nil,
					"tool_calls": tcs,
				})
				continue
			}
			apiMessages = append(apiMessages, map[string]any{"role": "assistant", "content": m.Content})
		case RoleTool:
			apiMessages = append(apiMessages, map[string]any{
				"role":         "tool",
				"tool_call_id": m.ToolCallID,
				"content":      m.Content,
			})
		default:
			apiMessages = append(apiMessages, map[string]any{"role": m.Role, "content": m.Content})
		}
	}

	payload := map[string]any{
		"model":    model,
		"messages": apiMessages,
		"stream":   true,
	}
	if len(toolDefs) > 0 {
		t := make([]map[string]any, 0, len(toolDefs))
		for _, td := range toolDefs {
			t = append(t, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        td.Name,
					"description": td.Description,
					"parameters":  td.InputSchema,
				},
			})
		}
		payload["tools"] = t
	}
	return payload
}

// streamMinimaxTurn reads SSE / NDJSON lines, pushes content deltas to
// `stream` as {"t":"data","b64":...}, accumulates tool-call argument
// fragments by index, and returns the assembled TurnResult on the stream's
// terminal event ([DONE] or any choice with finish_reason).
//
// If EOF arrives before a finish_reason is set AND no tool-call fragments
// were observed, the function returns an "incomplete stream" error so the
// daemon can surface it to the user (matching the prior streamMiniMaxSSE
// contract that the existing tests rely on).
func streamMinimaxTurn(ctx context.Context, r io.Reader, stream Pusher) (TurnResult, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	var (
		contentBuf   strings.Builder
		usage        *minimaxStreamUsage
		finishReason string
	)
	frags := map[int]*minimaxToolFrag{}
	var fragOrder []int

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return TurnResult{}, ctx.Err()
		default:
		}

		line := scanner.Text()
		var jsonData string
		switch {
		case strings.HasPrefix(line, "data: "):
			jsonData = strings.TrimPrefix(line, "data: ")
			if jsonData == "[DONE]" {
				return assembleTurn(contentBuf.String(), frags, fragOrder, usage, finishReason)
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
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
			delta := choice.Delta
			if choice.Message != nil && delta.Content == "" && len(delta.ToolCalls) == 0 {
				delta = *choice.Message
			}
			if delta.Content != "" {
				contentBuf.WriteString(delta.Content)
				stream.Push(encodeData(delta.Content))
			}
			for _, tc := range delta.ToolCalls {
				frag, ok := frags[tc.Index]
				if !ok {
					frag = &minimaxToolFrag{}
					frags[tc.Index] = frag
					fragOrder = append(fragOrder, tc.Index)
				}
				if tc.ID != "" {
					frag.id.WriteString(tc.ID)
				}
				if tc.Function.Name != "" {
					frag.name.WriteString(tc.Function.Name)
				}
				if tc.Function.Arguments != "" {
					frag.args.WriteString(tc.Function.Arguments)
				}
			}
		}
	}

	// Stream exhausted without a [DONE] marker. If we saw any terminal
	// signal (finish_reason) the turn is still well-formed; otherwise it's
	// an incomplete stream.
	if finishReason == "" && len(fragOrder) == 0 {
		return TurnResult{}, fmt.Errorf("incomplete stream: EOF before terminal event")
	}
	return assembleTurn(contentBuf.String(), frags, fragOrder, usage, finishReason)
}

// minimaxToolFrag accumulates one tool call's id/name/arguments across
// streamed delta chunks. The model can split a single call's argument JSON
// across many SSE events; we concatenate by Index until the stream ends.
type minimaxToolFrag struct {
	id   strings.Builder
	name strings.Builder
	args strings.Builder
}

func assembleTurn(content string, frags map[int]*minimaxToolFrag, order []int, usage *minimaxStreamUsage, finishReason string) (TurnResult, error) {
	tr := TurnResult{Content: content, FinishReason: finishReason}
	if usage != nil {
		tr.Usage = &Usage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
		}
	}
	for _, idx := range order {
		f := frags[idx]
		if f == nil {
			continue
		}
		name := f.name.String()
		if name == "" {
			continue
		}
		tr.ToolCalls = append(tr.ToolCalls, ToolCall{
			ID:   f.id.String(),
			Name: name,
			Args: f.args.String(),
		})
	}
	if tr.FinishReason == "" && len(tr.ToolCalls) > 0 {
		// Some backends omit finish_reason when sending tool_calls; treat
		// as the canonical tool_calls finish so RunAgenticLoop continues.
		tr.FinishReason = FinishToolCalls
	}
	return tr, nil
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
