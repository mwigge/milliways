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
	Content          string                  `json:"content"`
	Reasoning        string                  `json:"reasoning,omitempty"`
	ReasoningContent string                  `json:"reasoning_content,omitempty"`
	ReasoningDetails []openaiReasoningDetail `json:"reasoning_details,omitempty"`
	ToolCalls        []openaiStreamToolCall  `json:"tool_calls,omitempty"`
}

type openaiReasoningDetail struct {
	Text    string `json:"text,omitempty"`
	Content string `json:"content,omitempty"`
	Summary string `json:"summary,omitempty"`
}

func (d openaiStreamDelta) reasoningText() string {
	var b strings.Builder
	b.WriteString(d.ReasoningContent)
	b.WriteString(d.Reasoning)
	for _, detail := range d.ReasoningDetails {
		b.WriteString(detail.Text)
		b.WriteString(detail.Content)
		b.WriteString(detail.Summary)
	}
	return b.String()
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
		reasoningBuf strings.Builder
		usage        *openaiStreamUsage
		finishReason string
		think        thinkTagSplitter
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
				if visible, reasoning := think.Flush(); visible != "" || reasoning != "" {
					if reasoning != "" {
						reasoningBuf.WriteString(reasoning)
						stream.Push(encodeThinking(reasoning))
					}
					if visible != "" {
						contentBuf.WriteString(visible)
						stream.Push(encodeData(visible))
					}
				}
				return assembleOpenAITurn(contentBuf.String(), reasoningBuf.String(), frags, fragOrder, usage, finishReason)
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
			// Only fall back to choice.Message when the entire response was
			// non-streaming (no content received yet). Never use it mid-stream:
			// MiniMax sometimes echoes the full accumulated text in
			// choice.Message on the final chunk, which would duplicate output.
			if choice.Message != nil && delta.Content == "" &&
				len(delta.ToolCalls) == 0 && contentBuf.Len() == 0 && reasoningBuf.Len() == 0 {
				delta = *choice.Message
			}
			// Emit structured reasoning (reasoning_content / reasoning fields)
			// only if delta.Content is empty or does not also contain the same
			// reasoning wrapped in <think> tags — prevents double-emit when the
			// provider sends reasoning in both the structured field and inline.
			if reasoning := delta.reasoningText(); reasoning != "" && delta.Content == "" {
				reasoningBuf.WriteString(reasoning)
				stream.Push(encodeThinking(reasoning))
			}
			if delta.Content != "" {
				visible, reasoning := think.Push(delta.Content)
				if reasoning != "" {
					reasoningBuf.WriteString(reasoning)
					stream.Push(encodeThinking(reasoning))
				}
				if visible != "" {
					contentBuf.WriteString(visible)
					stream.Push(encodeData(visible))
				}
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

	if visible, reasoning := think.Flush(); visible != "" || reasoning != "" {
		if reasoning != "" {
			reasoningBuf.WriteString(reasoning)
			stream.Push(encodeThinking(reasoning))
		}
		if visible != "" {
			contentBuf.WriteString(visible)
			stream.Push(encodeData(visible))
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
	return assembleOpenAITurn(contentBuf.String(), reasoningBuf.String(), frags, fragOrder, usage, finishReason)
}

// assembleOpenAITurn folds the accumulated state into a TurnResult.
//
// Empty-name tool-call fragments (id and partial args arrived but
// function.name never did — typically due to mid-stream truncation) become
// synthetic error tool calls with name="__incomplete__" and args carrying a
// diagnostic message. RunAgenticLoop's executeOneToolCall folds these into
// "error: tool ... not found" tool messages, giving the model a chance to
// recover on the next turn.
func assembleOpenAITurn(content, reasoning string, frags map[int]*openaiToolFrag, order []int, usage *openaiStreamUsage, finishReason string) (TurnResult, error) {
	// Some models (e.g. Qwen2.5-Coder) emit tool calls as XML inside the
	// content rather than as proper tool_calls JSON objects. Parse them here
	// so the agentic loop can execute them regardless of the model's format.
	if len(order) == 0 {
		if xmlCalls := parseQwenXMLToolCalls(content); len(xmlCalls) > 0 {
			for i, tc := range xmlCalls {
				frags[i] = tc
				order = append(order, i)
			}
			content = "" // consumed by tool calls
		}
	}
	tr := TurnResult{Content: content, Reasoning: reasoning, FinishReason: finishReason}
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

type thinkTagSplitter struct {
	buf     string
	inThink bool
}

func (s *thinkTagSplitter) Push(text string) (visible, reasoning string) {
	s.buf += text
	var visibleBuf, reasoningBuf strings.Builder
	for {
		tag := "<think>"
		if s.inThink {
			tag = "</think>"
		}
		lower := strings.ToLower(s.buf)
		if idx := strings.Index(lower, tag); idx >= 0 {
			before := s.buf[:idx]
			if s.inThink {
				reasoningBuf.WriteString(before)
			} else {
				visibleBuf.WriteString(before)
			}
			s.buf = s.buf[idx+len(tag):]
			s.inThink = !s.inThink
			continue
		}

		keep := tagSuffixPrefixLen(s.buf, tag)
		emit := s.buf
		if keep > 0 {
			emit = s.buf[:len(s.buf)-keep]
			s.buf = s.buf[len(s.buf)-keep:]
		} else {
			s.buf = ""
		}
		if s.inThink {
			reasoningBuf.WriteString(emit)
		} else {
			visibleBuf.WriteString(emit)
		}
		return visibleBuf.String(), reasoningBuf.String()
	}
}

func (s *thinkTagSplitter) Flush() (visible, reasoning string) {
	defer func() { s.buf = "" }()
	if s.inThink {
		return "", s.buf
	}
	return s.buf, ""
}

func tagSuffixPrefixLen(text, tag string) int {
	text = strings.ToLower(text)
	limit := len(tag) - 1
	if len(text) < limit {
		limit = len(text)
	}
	for n := limit; n > 0; n-- {
		if strings.HasSuffix(text, tag[:n]) {
			return n
		}
	}
	return 0
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

// parseQwenXMLToolCalls extracts tool calls from Qwen-style XML content:
//
//	```xml
//	<function_call>{"name":"bash","arguments":{"command":"ls /tmp"}}</function_call>
//	```
//
// or the older Qwen format:
//
//	<function name="bash" arguments='{"command":"ls /tmp"}'/>
//
// Returns nil if no XML tool calls are found.
func parseQwenXMLToolCalls(content string) map[int]*openaiToolFrag {
	stripped := strings.TrimSpace(content)
	// Strip markdown code fence if present.
	stripped = strings.TrimPrefix(stripped, "```xml")
	stripped = strings.TrimSuffix(stripped, "```")
	stripped = strings.TrimSpace(stripped)

	var calls map[int]*openaiToolFrag

	// Format 1: <function_call>{"name":"...","arguments":{...}}</function_call>
	const openTag1 = "<function_call>"
	const closeTag1 = "</function_call>"
	idx := 0
	for {
		start := strings.Index(stripped, openTag1)
		if start < 0 {
			break
		}
		end := strings.Index(stripped[start:], closeTag1)
		if end < 0 {
			break
		}
		jsonBody := strings.TrimSpace(stripped[start+len(openTag1) : start+end])
		stripped = stripped[start+end+len(closeTag1):]
		var parsed struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(jsonBody), &parsed); err == nil && parsed.Name != "" {
			if calls == nil {
				calls = make(map[int]*openaiToolFrag)
			}
			f := &openaiToolFrag{}
			f.name.WriteString(parsed.Name)
			f.id.WriteString(fmt.Sprintf("call_%d", idx))
			f.args.Write(parsed.Arguments)
			calls[idx] = f
			idx++
		}
	}
	if calls != nil {
		return calls
	}

	// Format 2: <function name="bash" arguments='{"command":"ls"}' />
	// Simple regex-free scan for the attribute values.
	const funcOpen = "<function "
	rest := stripped
	for {
		start := strings.Index(rest, funcOpen)
		if start < 0 {
			break
		}
		end := strings.IndexRune(rest[start:], '>')
		if end < 0 {
			break
		}
		tag := rest[start : start+end+1]
		rest = rest[start+end+1:]
		name := extractAttr(tag, "name")
		args := extractAttr(tag, "arguments")
		if name == "" {
			continue
		}
		if calls == nil {
			calls = make(map[int]*openaiToolFrag)
		}
		f := &openaiToolFrag{}
		f.name.WriteString(name)
		f.id.WriteString(fmt.Sprintf("call_%d", idx))
		if args != "" {
			f.args.WriteString(args)
		} else {
			f.args.WriteString("{}")
		}
		calls[idx] = f
		idx++
	}
	return calls
}

// extractAttr extracts the value of a named attribute from a simple XML tag string.
// Handles both single and double quoted values.
func extractAttr(tag, attr string) string {
	search := attr + "="
	idx := strings.Index(tag, search)
	if idx < 0 {
		return ""
	}
	rest := tag[idx+len(search):]
	if len(rest) == 0 {
		return ""
	}
	quote := rune(rest[0])
	if quote != '\'' && quote != '"' {
		return ""
	}
	end := strings.IndexRune(rest[1:], quote)
	if end < 0 {
		return ""
	}
	return rest[1 : end+1]
}
