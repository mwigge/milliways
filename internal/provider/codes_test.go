package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mwigge/milliways/internal/session"
)

func TestCodesProviderSendParsesStreamingResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != "gpt-5.4" {
			t.Fatalf("model = %v, want gpt-5.4", payload["model"])
		}
		if payload["stream"] != true {
			t.Fatalf("stream = %v, want true", payload["stream"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" codes\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := newCodesProvider("test-key", server.URL, "gpt-5.4")
	resp, err := provider.Send(context.Background(), Request{
		Model:        ModelCodes,
		SystemPrompt: "system prompt",
		Messages: []session.Message{{
			Role:    session.RoleUser,
			Content: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp.Content != "Hello codes" {
		t.Fatalf("content = %q, want %q", resp.Content, "Hello codes")
	}
	if resp.Tokens.Input != 9 || resp.Tokens.Output != 4 {
		t.Fatalf("tokens = %+v", resp.Tokens)
	}
}

func TestCodesProviderSupportsModel(t *testing.T) {
	t.Parallel()

	provider := NewCodesProvider("key", "gpt-5.4")
	if !provider.SupportsModel(ModelCodes) {
		t.Fatal("expected codes model to be supported")
	}
	if !provider.SupportsModel(Model("codes/gpt5.4")) {
		t.Fatal("expected codes prefixed model to be supported")
	}
	if provider.SupportsModel(Model("gemini-2.5-pro")) {
		t.Fatal("unexpected model support")
	}
}
