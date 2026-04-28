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

package adapter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func setHTTPKitchenTransport(k *HTTPKitchen, fn roundTripFunc) {
	k.client = &http.Client{Transport: fn, Timeout: k.timeout}
}

func testHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type contextCancelBody struct {
	ctx  context.Context
	data string
	sent bool
}

func (b *contextCancelBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, b.data), nil
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *contextCancelBody) Close() error { return nil }

func TestNewHTTPKitchen_RequiresFieldsAndDefaults(t *testing.T) {
	// Error cases — run in parallel (no t.Setenv)
	errorCases := []struct {
		name    string
		cfg     HTTPKitchenConfig
		wantErr string
	}{
		{
			name:    "missing base url",
			cfg:     HTTPKitchenConfig{AuthKey: "TEST_API_KEY", Model: "gpt-4.1"},
			wantErr: "base_url is required",
		},
		{
			name:    "missing model",
			cfg:     HTTPKitchenConfig{BaseURL: "https://api.example.test", AuthKey: "TEST_API_KEY"},
			wantErr: "model is required",
		},
	}
	for _, tt := range errorCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewHTTPKitchen("api", tt.cfg, []string{"code"}, kitchen.Cloud)
			if err == nil {
				t.Fatalf("err = nil, want substring %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want substring %q", err, tt.wantErr)
			}
		})
	}

	// Valid empty auth (Ollama no-auth) — no t.Setenv needed
	t.Run("empty auth key is valid for local kitchens", func(t *testing.T) {
		k, err := NewHTTPKitchen("ollama", HTTPKitchenConfig{
			BaseURL: "http://localhost:11434",
			Model:   "llama3",
		}, []string{"local"}, kitchen.Free)
		if err != nil {
			t.Fatalf("NewHTTPKitchen() error = %v, want no error", err)
		}
		if k.Status() != kitchen.Ready {
			t.Fatalf("Status() = %s, want ready (empty authKey means no credential needed)", k.Status())
		}
	})

	// Defaults applied — needs t.Setenv, run last
	t.Setenv("TEST_HTTP_DEFAULTS", "")
	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL: "https://api.example.test/",
		AuthKey: "TEST_HTTP_DEFAULTS",
		Model:   "gpt-4.1",
	}, []string{"code"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}
	if k.baseURL != "https://api.example.test" {
		t.Fatalf("baseURL = %q, want trimmed", k.baseURL)
	}
	if k.authType != "bearer" {
		t.Fatalf("authType = %q, want bearer", k.authType)
	}
	if k.responseFormat != "openai" {
		t.Fatalf("responseFormat = %q, want openai", k.responseFormat)
	}
	if k.timeout != 5*time.Minute {
		t.Fatalf("timeout = %v, want %v", k.timeout, 5*time.Minute)
	}
	if k.Status() != kitchen.NeedsAuth {
		t.Fatalf("Status() = %s, want %s", k.Status(), kitchen.NeedsAuth)
	}
}

func TestNewHTTPKitchen_OverridesStationsAndTier(t *testing.T) {
	t.Setenv("TEST_HTTP_OVERRIDES", "secret")
	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL:  "https://api.example.test",
		AuthKey:  "TEST_HTTP_OVERRIDES",
		Model:    "gpt-4.1",
		Stations: []string{"review"},
		Tier:     kitchen.Free,
		Timeout:  time.Second,
	}, []string{"code"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}
	if k.CostTier() != kitchen.Free {
		t.Fatalf("CostTier() = %s, want %s", k.CostTier(), kitchen.Free)
	}
	stations := k.Stations()
	if len(stations) != 1 || stations[0] != "review" {
		t.Fatalf("Stations() = %v, want [review]", stations)
	}
	stations[0] = "mutated"
	if k.Stations()[0] != "review" {
		t.Fatal("Stations() should return a defensive copy")
	}
	if k.Status() != kitchen.Ready {
		t.Fatalf("Status() = %s, want %s", k.Status(), kitchen.Ready)
	}
}

func TestHTTPKitchen_Exec_OpenAIStream(t *testing.T) {
	const envKey = "TEST_HTTP_KITCHEN_OPENAI"
	t.Setenv(envKey, "secret")

	var gotPath string
	var gotAuth string
	var gotContentType string

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL: "http://api.test",
		AuthKey: envKey,
		Model:   "gpt-4.1",
	}, []string{"code"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}
	setHTTPKitchenTransport(k, func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		return testHTTPResponse(http.StatusOK,
			"data: {\"choices\":[{\"delta\":{\"content\":\"hello \"},\"finish_reason\":\"\"}]}\n\n"+
				"data: {\"choices\":[{\"delta\":{\"content\":\"world\"},\"finish_reason\":\"\"}]}\n\n"+
				"data: {\"choices\":[{\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}]}\n\n"), nil
	})

	var lines []string
	result, err := k.Exec(context.Background(), kitchen.Task{
		Prompt: "say hi",
		OnLine: func(line string) { lines = append(lines, line) },
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q, want bearer", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Output != "hello world" {
		t.Fatalf("Output = %q, want hello world", result.Output)
	}
	if len(lines) != 2 || lines[0] != "hello " || lines[1] != "world" {
		t.Fatalf("OnLine lines = %v, want [hello  world]", lines)
	}
	if result.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
}

func TestHTTPKitchen_Exec_AnthropicStream(t *testing.T) {
	const envKey = "TEST_HTTP_KITCHEN_ANTHROPIC"
	t.Setenv(envKey, "secret")

	var gotPath string
	var gotAuth string
	var gotVersion string

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL:        "http://api.test",
		AuthKey:        envKey,
		AuthType:       "apikey",
		Model:          "claude-3-7-sonnet",
		ResponseFormat: "anthropic",
	}, []string{"review"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}
	setHTTPKitchenTransport(k, func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("X-API-Key")
		gotVersion = r.Header.Get("anthropic-version")
		return testHTTPResponse(http.StatusOK,
			"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"alpha\"}}\n\n"+
				"data: {\"type\":\"message_stop\"}\n\n"), nil
	})

	result, err := k.Exec(context.Background(), kitchen.Task{Prompt: "say hi"})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", gotPath)
	}
	if gotAuth != "secret" {
		t.Fatalf("X-API-Key = %q, want secret", gotAuth)
	}
	if gotVersion != "2023-06-01" {
		t.Fatalf("anthropic-version = %q, want 2023-06-01", gotVersion)
	}
	if result.Output != "alpha" {
		t.Fatalf("Output = %q, want alpha", result.Output)
	}
}

func TestHTTPKitchen_Exec_OllamaStream(t *testing.T) {
	const envKey = "TEST_HTTP_KITCHEN_OLLAMA"
	t.Setenv(envKey, "secret")

	var gotPath string

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL:        "http://api.test",
		AuthKey:        envKey,
		Model:          "llama3",
		ResponseFormat: "ollama",
	}, []string{"code"}, kitchen.Local)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}
	setHTTPKitchenTransport(k, func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		return testHTTPResponse(http.StatusOK,
			"data: {\"message\":{\"content\":\"part\"},\"done\":false}\n\n"+
				"data: {\"message\":{\"content\":\"ial\"},\"done\":true}\n\n"), nil
	})

	result, err := k.Exec(context.Background(), kitchen.Task{Prompt: "say hi"})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if gotPath != "/api/chat" {
		t.Fatalf("path = %q, want /api/chat", gotPath)
	}
	if result.Output != "partial" {
		t.Fatalf("Output = %q, want partial", result.Output)
	}
}

func TestHTTPKitchen_Exec_ReturnsPartialOutputOnContextCancel(t *testing.T) {
	const envKey = "TEST_HTTP_KITCHEN_CANCEL"
	t.Setenv(envKey, "secret")

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL: "http://api.test",
		AuthKey: envKey,
		Model:   "gpt-4.1",
	}, []string{"code"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}
	setHTTPKitchenTransport(k, func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: &contextCancelBody{
				ctx:  r.Context(),
				data: "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"\"}]}\n\n",
			},
		}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	var lines []string
	gotLine := make(chan struct{}, 1)
	errCh := make(chan error, 1)
	var result kitchen.Result
	go func() {
		var execErr error
		result, execErr = k.Exec(ctx, kitchen.Task{Prompt: "say hi", OnLine: func(line string) {
			lines = append(lines, line)
			select {
			case gotLine <- struct{}{}:
			default:
			}
		}})
		errCh <- execErr
	}()

	<-gotLine
	cancel()

	err = <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if result.Output != "partial" {
		t.Fatalf("Output = %q, want partial", result.Output)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if len(lines) != 1 || lines[0] != "partial" {
		t.Fatalf("OnLine lines = %v, want [partial]", lines)
	}
}

func TestHTTPKitchen_Exec_HTTPError(t *testing.T) {
	const envKey = "TEST_HTTP_KITCHEN_ERROR"
	t.Setenv(envKey, "secret")

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL: "http://api.test",
		AuthKey: envKey,
		Model:   "gpt-4.1",
	}, []string{"code"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}
	setHTTPKitchenTransport(k, func(r *http.Request) (*http.Response, error) {
		return testHTTPResponse(http.StatusTooManyRequests, "bad upstream\n"), nil
	})

	_, err = k.Exec(context.Background(), kitchen.Task{Prompt: "say hi"})
	if err == nil || !strings.Contains(err.Error(), "API status 429") {
		t.Fatalf("err = %v, want API status 429", err)
	}
}

func TestHTTPKitchen_Exec_IncompleteStreamReturnsError(t *testing.T) {
	const envKey = "TEST_HTTP_KITCHEN_INCOMPLETE"
	t.Setenv(envKey, "secret")

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL: "http://api.test",
		AuthKey: envKey,
		Model:   "gpt-4.1",
	}, []string{"code"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}
	setHTTPKitchenTransport(k, func(r *http.Request) (*http.Response, error) {
		return testHTTPResponse(http.StatusOK, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"\"}]}\n\n"), nil
	})

	result, err := k.Exec(context.Background(), kitchen.Task{Prompt: "say hi"})
	if err == nil || !strings.Contains(err.Error(), "incomplete HTTP stream") {
		t.Fatalf("err = %v, want incomplete HTTP stream", err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1", result.ExitCode)
	}
	if result.Output != "partial" {
		t.Fatalf("Output = %q, want partial", result.Output)
	}
}

func TestHTTPKitchen_StatusReflectsEnvChanges(t *testing.T) {
	const envKey = "TEST_HTTP_KITCHEN_STATUS"
	_ = os.Unsetenv(envKey)
	t.Cleanup(func() {
		_ = os.Unsetenv(envKey)
	})

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL: "https://api.example.test",
		AuthKey: envKey,
		Model:   "gpt-4.1",
	}, []string{"code"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}
	if k.Status() != kitchen.NeedsAuth {
		t.Fatalf("Status() = %s, want %s", k.Status(), kitchen.NeedsAuth)
	}
	t.Setenv(envKey, "secret")
	if k.Status() != kitchen.Ready {
		t.Fatalf("Status() = %s, want %s", k.Status(), kitchen.Ready)
	}
}
