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

package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/session"
)

const (
	defaultMiniMaxBaseURL = "https://api.minimax.chat/v1"
	defaultMiniMaxModel   = "MiniMax-Text-01"
	defaultHTTPTimeout    = 60 * time.Second
)

// ErrMissingAPIKey indicates that no MiniMax API key was configured.
var ErrMissingAPIKey = errors.New("minimax api key is required")

// MiniMaxProvider sends OpenAI-compatible chat completion requests to MiniMax.
type MiniMaxProvider struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewMiniMaxProvider builds a MiniMax provider with sensible defaults.
func NewMiniMaxProvider(apiKey, baseURL, model string) *MiniMaxProvider {
	if strings.TrimSpace(apiKey) == "" {
		apiKey = os.Getenv("MINIMAX_API_KEY")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultMiniMaxBaseURL
	}
	if strings.TrimSpace(model) == "" {
		model = defaultMiniMaxModel
	}

	return &MiniMaxProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
}

// SupportsModel reports whether the MiniMax provider can handle the given model.
func (p *MiniMaxProvider) SupportsModel(m Model) bool {
	return m == ModelMiniMax
}

// Send executes a streaming chat completion request.
func (p *MiniMaxProvider) Send(ctx context.Context, req Request) (Response, error) {
	if p == nil {
		return Response{}, errors.New("nil minimax provider")
	}
	if !p.SupportsModel(req.Model) {
		return Response{}, fmt.Errorf("unsupported model %q", req.Model)
	}
	if strings.TrimSpace(p.apiKey) == "" {
		return Response{}, ErrMissingAPIKey
	}

	body, err := json.Marshal(buildChatRequest(req, p.model))
	if err != nil {
		return Response{}, fmt.Errorf("marshal minimax request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("create minimax request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	httpClient := p.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}

	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("send minimax request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		message, readErr := readErrorBody(httpResp.Body)
		if readErr != nil {
			return Response{}, fmt.Errorf("minimax status %d: %w", httpResp.StatusCode, readErr)
		}
		return Response{}, fmt.Errorf("minimax status %d: %s", httpResp.StatusCode, message)
	}

	return parseStreamResponse(httpResp.Body)
}

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type streamEvent struct {
	Choices []streamChoice `json:"choices"`
	Usage   *streamUsage   `json:"usage,omitempty"`
}

type streamChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type streamDelta struct {
	Content   string           `json:"content,omitempty"`
	ToolCalls []streamToolCall `json:"tool_calls,omitempty"`
}

type streamToolCall struct {
	ID       string             `json:"id,omitempty"`
	Index    int                `json:"index,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function streamToolFunction `json:"function"`
}

type streamToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type streamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type toolFragment struct {
	Name string
	Args strings.Builder
	Seen int
}

func buildChatRequest(req Request, model string) chatCompletionRequest {
	messages := make([]chatMessage, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.SystemPrompt) != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.SystemPrompt})
	}
	for _, msg := range req.Messages {
		messages = append(messages, chatMessage{Role: string(msg.Role), Content: msg.Content})
	}

	tools := make([]chatTool, 0, len(req.Tools))
	for _, tool := range req.Tools {
		tools = append(tools, chatTool{
			Type: "function",
			Function: chatFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}

	return chatCompletionRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}
}

func parseStreamResponse(body io.Reader) (Response, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var content strings.Builder
	toolParts := make(map[string]*toolFragment)
	order := make([]string, 0)
	response := Response{}
	completed := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			completed = true
			break
		}

		var event streamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return Response{}, fmt.Errorf("decode SSE event: %w", err)
		}
		if event.Usage != nil {
			response.Tokens = TokenCount{Input: event.Usage.PromptTokens, Output: event.Usage.CompletionTokens}
		}
		for _, choice := range event.Choices {
			if choice.FinishReason != "" {
				completed = true
			}
			content.WriteString(choice.Delta.Content)
			for _, call := range choice.Delta.ToolCalls {
				key := call.ID
				if key == "" {
					key = fmt.Sprintf("index-%d", call.Index)
				}
				fragment, ok := toolParts[key]
				if !ok {
					fragment = &toolFragment{}
					toolParts[key] = fragment
					order = append(order, key)
				}
				fragment.Seen++
				if call.Function.Name != "" {
					fragment.Name = call.Function.Name
				}
				if call.Function.Arguments != "" {
					fragment.Args.WriteString(call.Function.Arguments)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Response{}, fmt.Errorf("read SSE stream: %w", err)
	}
	if !completed {
		return Response{}, fmt.Errorf("incomplete SSE stream: EOF before terminal event")
	}

	response.Content = content.String()
	if len(order) > 0 {
		sort.SliceStable(order, func(i, j int) bool {
			return toolParts[order[i]].Seen < toolParts[order[j]].Seen
		})
		fragment := toolParts[order[0]]
		args, err := parseToolArgs(fragment.Args.String())
		if err != nil {
			return Response{}, err
		}
		response.ToolCall = &ToolCall{Name: fragment.Name, Args: args}
	}

	return response, nil
}

func parseToolArgs(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("decode tool arguments: %w", err)
	}
	return args, nil
}

func readErrorBody(body io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read error body: %w", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return http.StatusText(http.StatusBadRequest), nil
	}

	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &payload); err == nil && strings.TrimSpace(payload.Error.Message) != "" {
		return payload.Error.Message, nil
	}
	return trimmed, nil
}

var _ Provider = (*MiniMaxProvider)(nil)

var _ = session.Message{}
