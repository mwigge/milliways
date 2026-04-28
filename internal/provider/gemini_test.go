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
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/session"
)

func TestGeminiProviderSendParsesJSONResponse(t *testing.T) {
	t.Parallel()

	provider := newGeminiProvider("gem-key", "http://gemini.test", "gemini-2.5-pro")
	provider.httpClient = &http.Client{Transport: providerRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/models/gemini-2.5-pro:generateContent" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("key"); got != "gem-key" {
			t.Fatalf("key = %q, want gem-key", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		generationConfig := payload["generationConfig"].(map[string]any)
		if generationConfig["responseMimeType"] != "application/json" {
			t.Fatalf("responseMimeType = %v", generationConfig["responseMimeType"])
		}
		contents := payload["contents"].([]any)
		text := contents[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, "system prompt") || !strings.Contains(text, "hello") {
			t.Fatalf("request text = %q", text)
		}

		return providerTestResponse(http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"{\"answer\":\"ok\"}"}]}}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":6}}`), nil
	})}

	resp, err := provider.Send(context.Background(), Request{
		Model:        ModelGemini,
		SystemPrompt: "system prompt",
		Messages: []session.Message{{
			Role:    session.RoleUser,
			Content: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp.Content != `{"answer":"ok"}` {
		t.Fatalf("content = %q", resp.Content)
	}
	if resp.Tokens.Input != 11 || resp.Tokens.Output != 6 {
		t.Fatalf("tokens = %+v", resp.Tokens)
	}
}

func TestGeminiProviderSendParsesStreamingResponse(t *testing.T) {
	t.Parallel()

	provider := newGeminiProvider("gem-key", "http://gemini.test", "gemini-2.5-pro")
	provider.httpClient = &http.Client{Transport: providerRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return providerTestResponse(http.StatusOK,
			"data: [{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hello\"}]}}]}]\n\n"+
				"data: [{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\" Gemini\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":3}}]\n\n"), nil
	})}
	resp, err := provider.Send(context.Background(), Request{Model: Model("models/gemini-2.5-pro")})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp.Content != "Hello Gemini" {
		t.Fatalf("content = %q, want %q", resp.Content, "Hello Gemini")
	}
	if resp.Tokens.Input != 7 || resp.Tokens.Output != 3 {
		t.Fatalf("tokens = %+v", resp.Tokens)
	}
}

func TestGeminiProviderSendRejectsIncompleteStream(t *testing.T) {
	t.Parallel()

	provider := newGeminiProvider("gem-key", "http://gemini.test", "gemini-2.5-pro")
	provider.httpClient = &http.Client{Transport: providerRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return providerTestResponse(http.StatusOK, "data: [{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}]\n\n"), nil
	})}

	_, err := provider.Send(context.Background(), Request{Model: Model("models/gemini-2.5-pro")})
	if err == nil || !strings.Contains(err.Error(), "incomplete gemini stream") {
		t.Fatalf("err = %v, want incomplete gemini stream", err)
	}
}

func TestGeminiProviderSupportsModel(t *testing.T) {
	t.Parallel()

	provider := NewGeminiProvider("key", "gemini-2.5-pro")
	if !provider.SupportsModel(ModelGemini) {
		t.Fatal("expected gemini model to be supported")
	}
	if !provider.SupportsModel(Model("models/gemini-2.5-pro")) {
		t.Fatal("expected models/gemini prefix to be supported")
	}
	if provider.SupportsModel(ModelCodes) {
		t.Fatal("unexpected model support")
	}
}
