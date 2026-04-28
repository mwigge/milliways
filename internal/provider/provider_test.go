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
	"context"
	"testing"

	"github.com/mwigge/milliways/internal/session"
)

type mockProvider struct {
	lastReq  Request
	response Response
	called   bool
}

func (m *mockProvider) Send(_ context.Context, req Request) (Response, error) {
	m.called = true
	m.lastReq = req
	return m.response, nil
}

func (m *mockProvider) SupportsModel(model Model) bool {
	return model == ModelMiniMax
}

func TestProviderRequestResponseRoundTrip(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{
		response: Response{
			Content: "ok",
			ToolCall: &ToolCall{
				Name: "Read",
				Args: map[string]any{"path": "README.md"},
			},
			Tokens: TokenCount{Input: 7, Output: 3},
		},
	}

	req := Request{
		Model: ModelMiniMax,
		Messages: []session.Message{{
			Role:    "user",
			Content: "hello",
		}},
		Tools: []ToolDef{{
			Name:        "Read",
			Description: "Read file contents",
			InputSchema: map[string]any{"type": "object"},
		}},
		SystemPrompt: "system",
	}

	resp, err := provider.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !provider.called {
		t.Fatal("expected provider to be called")
	}
	if provider.lastReq.SystemPrompt != req.SystemPrompt {
		t.Fatalf("system prompt mismatch: got %q want %q", provider.lastReq.SystemPrompt, req.SystemPrompt)
	}
	if len(provider.lastReq.Messages) != 1 || provider.lastReq.Messages[0].Content != "hello" {
		t.Fatalf("messages mismatch: %+v", provider.lastReq.Messages)
	}
	if len(provider.lastReq.Tools) != 1 || provider.lastReq.Tools[0].Name != "Read" {
		t.Fatalf("tools mismatch: %+v", provider.lastReq.Tools)
	}
	if resp.Content != "ok" {
		t.Fatalf("content mismatch: got %q", resp.Content)
	}
	if resp.ToolCall == nil || resp.ToolCall.Name != "Read" {
		t.Fatalf("tool call mismatch: %+v", resp.ToolCall)
	}
	if resp.Tokens.Input != 7 || resp.Tokens.Output != 3 {
		t.Fatalf("token mismatch: %+v", resp.Tokens)
	}
}
