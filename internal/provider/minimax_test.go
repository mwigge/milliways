package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/session"
)

func TestMiniMaxProviderSendParsesStreamingResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		messages := payload["messages"].([]any)
		if len(messages) != 2 {
			t.Fatalf("messages len = %d, want 2", len(messages))
		}
		first := messages[0].(map[string]any)
		if first["role"] != "system" || first["content"] != "system prompt" {
			t.Fatalf("system message = %#v", first)
		}
		tools := payload["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("tools len = %d, want 1", len(tools))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" world\",\"tool_calls\":[{\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"Read\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMiniMaxProvider("test-key", server.URL, "test-model")
	resp, err := provider.Send(context.Background(), Request{
		Model:        ModelMiniMax,
		SystemPrompt: "system prompt",
		Messages: []session.Message{{
			Role:    "user",
			Content: "hello",
		}},
		Tools: []ToolDef{{
			Name:        "Read",
			Description: "Read a file",
			InputSchema: map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp.Content != "Hello world" {
		t.Fatalf("content = %q, want %q", resp.Content, "Hello world")
	}
	if resp.ToolCall == nil || resp.ToolCall.Name != "Read" {
		t.Fatalf("tool call = %+v", resp.ToolCall)
	}
	if resp.ToolCall.Args["path"] != "README.md" {
		t.Fatalf("tool args = %+v", resp.ToolCall.Args)
	}
	if resp.Tokens.Input != 12 || resp.Tokens.Output != 5 {
		t.Fatalf("tokens = %+v", resp.Tokens)
	}
}

func TestMiniMaxProviderSendRequiresAPIKey(t *testing.T) {
	t.Parallel()

	provider := NewMiniMaxProvider("", "https://example.com", "model")
	provider.apiKey = ""

	_, err := provider.Send(context.Background(), Request{Model: ModelMiniMax})
	if err == nil || !strings.Contains(err.Error(), ErrMissingAPIKey.Error()) {
		t.Fatalf("expected missing api key error, got %v", err)
	}
}

func TestMiniMaxProviderSupportsModel(t *testing.T) {
	t.Parallel()

	provider := NewMiniMaxProvider("key", "", "")
	if !provider.SupportsModel(ModelMiniMax) {
		t.Fatal("expected minimax model to be supported")
	}
	if provider.SupportsModel(Model("other")) {
		t.Fatal("unexpected model support")
	}
}
