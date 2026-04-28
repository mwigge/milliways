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

func TestCodesProviderSendParsesStreamingResponse(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: providerRoundTripFunc(func(r *http.Request) (*http.Response, error) {
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

		return providerTestResponse(http.StatusOK,
			"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n"+
				"data: {\"choices\":[{\"delta\":{\"content\":\" codes\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4}}\n\n"+
				"data: [DONE]\n\n"), nil
	})}

	provider := newCodesProvider("test-key", "http://codes.test", "gpt-5.4")
	provider.httpClient = client
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

func TestCodesProviderSendRejectsIncompleteStream(t *testing.T) {
	t.Parallel()

	provider := newCodesProvider("test-key", "http://codes.test", "gpt-5.4")
	provider.httpClient = &http.Client{Transport: providerRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return providerTestResponse(http.StatusOK, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"), nil
	})}

	_, err := provider.Send(context.Background(), Request{Model: ModelCodes})
	if err == nil || !strings.Contains(err.Error(), "incomplete SSE stream") {
		t.Fatalf("err = %v, want incomplete SSE stream", err)
	}
}

func TestCodesProviderSupportsModel(t *testing.T) {
	t.Parallel()

	provider := NewCodesProvider("key", "gpt-5.4")
	if !provider.SupportsModel(ModelCodes) {
		t.Fatal("expected codes model to be supported")
	}
	if !provider.SupportsModel(Model("gpt-5.4")) {
		t.Fatal("expected direct codes model to be supported")
	}
	if !provider.SupportsModel(Model("codes/gpt5.4")) {
		t.Fatal("expected codes prefixed model to be supported")
	}
	if provider.SupportsModel(Model("gemini-2.5-pro")) {
		t.Fatal("unexpected model support")
	}
}
