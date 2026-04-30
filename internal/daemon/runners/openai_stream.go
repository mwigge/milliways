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

// Shared OpenAI-compatible chat-completions streaming helpers used by every
// HTTP-based runner (currently `minimax` and `local`). The wire shape is
// identical across these providers because they all advertise the same
// OpenAI Chat Completions protocol; the per-runner code only differs in URL
// construction, auth header, default model/endpoint, and cost calculation.
//
// Centralising the SSE parser here means stream-integrity fixes (truncation
// detection, oversized-line handling, empty-tool-name fold-back) live in one
// place — the per-runner adapters reduce to a thin Client implementation.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mwigge/milliways/internal/provider"
)

// Sentinel errors so callers can distinguish stream-integrity failures from
// transport/decode errors via errors.Is.
var (
	// ErrIncompleteStream — the SSE stream ended before any finish_reason was
	// seen and no tool-call fragments were assembled. Indicates the model or
	// network truncated the response mid-flight.
	ErrIncompleteStream = errors.New("incomplete stream: EOF before terminal event")

	// ErrSSELineTooLarge — a single SSE line (typically a streamed tool-call
	// arguments JSON) exceeded the bufio.Scanner buffer cap. The runner
	// surfaces this rather than processing the partial buffer.
	ErrSSELineTooLarge = errors.New("SSE line exceeds scanner buffer")
)

// openaiStreamUsage carries the standard OpenAI token-usage block.
type openaiStreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// openaiStreamDelta carries per-chunk content + tool-call fragments.
type openaiStreamDelta struct {
	Content   string                 `json:"content"`
	ToolCalls []openaiStreamToolCall `json:"tool_calls,omitempty"`
}

// openaiStreamToolCall mirrors OpenAI's streaming tool_call delta shape.
// Streamed deltas may split a single call's id/name/arguments across chunks
// so the receiver must accumulate by Index.
type openaiStreamToolCall struct {
	ID       string                   `json:"id,omitempty"`
	Index    int                      `json:"index"`
	Type     string                   `json:"type,omitempty"`
	Function openaiStreamToolFunction `json:"function"`
}

type openaiStreamToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// openaiStreamChoice wraps delta + non-streaming fallback message.
type openaiStreamChoice struct {
	Delta        openaiStreamDelta  `json:"delta"`
	Message      *openaiStreamDelta `json:"message,omitempty"`
	FinishReason string             `json:"finish_reason,omitempty"`
}

// openaiStreamChunk is one decoded SSE event payload.
type openaiStreamChunk struct {
	Choices []openaiStreamChoice `json:"choices"`
	Usage   *openaiStreamUsage   `json:"usage,omitempty"`
}

// openaiToolFrag accumulates one tool call's id/name/arguments across
// streamed delta chunks.
type openaiToolFrag struct {
	id   strings.Builder
	name strings.Builder
	args strings.Builder
}

// sseScannerBufferCap caps any single SSE line at 1 MiB. Tool-call
// arguments JSON longer than this is rare but possible for multi-MB file
// writes; we surface ErrSSELineTooLarge rather than silently truncate.
const sseScannerBufferCap = 1 << 20

// streamOpenAITurn parses a streaming OpenAI-compatible chat-completions
// response, pushes content deltas to `stream` as {"t":"data","b64":...},
// accumulates tool-call argument fragments by index, and returns a
// TurnResult on the stream's terminal event ([DONE] or any choice with
// finish_reason).
//
// Error contract (review-driven):
//   - bufio.Scanner buffer overflow → wraps ErrSSELineTooLarge
//   - context cancellation → returns ctx.Err()
//   - other scanner errors → wrapped with "sse scan: %w"
//   - EOF before any finish_reason AND no tool-call fragments seen →
//     ErrIncompleteStream
//   - EOF after any finish_reason OR with tool-call fragments → returns the
//     assembled TurnResult (assembleOpenAITurn folds empty-name fragments
//     into a structured error message back to the model)
func streamOpenAITurn(ctx context.Context, r io.Reader, stream Pusher) (TurnResult, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 256*1024), sseScannerBufferCap)

	var (
		contentBuf   strings.Builder
		usage        *openaiStreamUsage
		finishReason string
	)
	frags := map[int]*openaiToolFrag{}
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
				return assembleOpenAITurn(contentBuf.String(), frags, fragOrder, usage, finishReason)
			}
		case strings.HasPrefix(line, "{"):
			jsonData = line
		default:
			continue
		}

		var chunk openaiStreamChunk
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
					frag = &openaiToolFrag{}
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

	// Scanner exited without [DONE]. Check for buffer overflow first — that
	// path silently swallowed lines on prior versions of this code.
	if scanErr := scanner.Err(); scanErr != nil {
		if errors.Is(scanErr, bufio.ErrTooLong) {
			return TurnResult{}, fmt.Errorf("sse scan: %w", ErrSSELineTooLarge)
		}
		return TurnResult{}, fmt.Errorf("sse scan: %w", scanErr)
	}

	// Genuinely empty / truncated stream.
	if finishReason == "" && len(fragOrder) == 0 {
		return TurnResult{}, ErrIncompleteStream
	}

	// Stream ended without [DONE] but produced something assemblable.
	return assembleOpenAITurn(contentBuf.String(), frags, fragOrder, usage, finishReason)
}

// assembleOpenAITurn folds the accumulated state into a TurnResult.
//
// Empty-name tool-call fragments (id and partial args arrived but
// function.name never did — typically due to mid-stream truncation) become
// synthetic error tool calls with name="__incomplete__" and args carrying a
// diagnostic message. RunAgenticLoop's executeOneToolCall folds these into
// "error: tool ... not found" tool messages, giving the model a chance to
// recover on the next turn.
func assembleOpenAITurn(content string, frags map[int]*openaiToolFrag, order []int, usage *openaiStreamUsage, finishReason string) (TurnResult, error) {
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
		id := f.id.String()
		if name == "" {
			// Reviewer HIGH 6: surface incomplete tool calls rather than
			// dropping them silently. Synthesise a sentinel name so the
			// loop's executeOneToolCall returns "error: tool ... not found"
			// back to the model.
			if id == "" {
				id = fmt.Sprintf("incomplete_%d", idx)
			}
			tr.ToolCalls = append(tr.ToolCalls, ToolCall{
				ID:   id,
				Name: "__incomplete__",
				Args: fmt.Sprintf(`{"reason":"tool call assembled with empty function.name (likely mid-stream truncation)","id":%q}`, id),
			})
			continue
		}
		tr.ToolCalls = append(tr.ToolCalls, ToolCall{
			ID:   id,
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

// buildOpenAIChatPayload converts agentic-loop Messages into an
// OpenAI-compatible chat-completions payload, including the optional tools
// array.
//
// Role-specific shaping:
//   - assistant turns with ToolCalls become {role:"assistant", content:null,
//     tool_calls:[{id,type,function:{name,arguments}}]}
//   - tool turns become {role:"tool", tool_call_id, content}
//   - everything else passes through as {role, content}
//
// Reviewer MEDIUM 11: empty tool_call_id can cause provider 400s. We
// synthesise a stable id when missing.
func buildOpenAIChatPayload(model string, messages []Message, toolDefs []provider.ToolDef) map[string]any {
	apiMessages := make([]map[string]any, 0, len(messages))
	for i, m := range messages {
		switch m.Role {
		case RoleAssistant:
			if len(m.ToolCalls) > 0 {
				tcs := make([]map[string]any, 0, len(m.ToolCalls))
				for j, tc := range m.ToolCalls {
					id := tc.ID
					if id == "" {
						id = fmt.Sprintf("call_%d_%d", i, j)
					}
					tcs = append(tcs, map[string]any{
						"id":   id,
						"type": "function",
						"function": map[string]any{
							"name":      tc.Name,
							"arguments": tc.Args,
						},
					})
				}
				apiMessages = append(apiMessages, map[string]any{
					"role":       "assistant",
					"content":    nil,
					"tool_calls": tcs,
				})
				continue
			}
			apiMessages = append(apiMessages, map[string]any{"role": "assistant", "content": m.Content})
		case RoleTool:
			id := m.ToolCallID
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			apiMessages = append(apiMessages, map[string]any{
				"role":         "tool",
				"tool_call_id": id,
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
