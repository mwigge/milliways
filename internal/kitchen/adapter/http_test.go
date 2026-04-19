package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

func TestNewHTTPKitchen_RequiresFieldsAndDefaults(t *testing.T) {
	tests := []struct {
		name    string
		cfg     HTTPKitchenConfig
		wantErr string
	}{
		{
			name:    "missing auth key",
			cfg:     HTTPKitchenConfig{BaseURL: "https://api.example.test", Model: "gpt-4.1"},
			wantErr: "auth_key is required",
		},
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewHTTPKitchen("api", tt.cfg, []string{"code"}, kitchen.Cloud)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want substring %q", err, tt.wantErr)
			}
		})
	}

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello \"},\"finish_reason\":\"\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"world\"},\"finish_reason\":\"\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}]}\n\n"))
	}))
	defer server.Close()

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL: server.URL,
		AuthKey: envKey,
		Model:   "gpt-4.1",
	}, []string{"code"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("X-API-Key")
		gotVersion = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"alpha\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL:        server.URL,
		AuthKey:        envKey,
		AuthType:       "apikey",
		Model:          "claude-3-7-sonnet",
		ResponseFormat: "anthropic",
	}, []string{"review"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"message\":{\"content\":\"part\"},\"done\":false}\n\n"))
		_, _ = w.Write([]byte("data: {\"message\":{\"content\":\"ial\"},\"done\":true}\n\n"))
	}))
	defer server.Close()

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL:        server.URL,
		AuthKey:        envKey,
		Model:          "llama3",
		ResponseFormat: "ollama",
	}, []string{"code"}, kitchen.Local)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}

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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"\"}]}\n\n"))
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL: server.URL,
		AuthKey: envKey,
		Model:   "gpt-4.1",
	}, []string{"code"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}

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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad upstream", http.StatusTooManyRequests)
	}))
	defer server.Close()

	k, err := NewHTTPKitchen("api", HTTPKitchenConfig{
		BaseURL: server.URL,
		AuthKey: envKey,
		Model:   "gpt-4.1",
	}, []string{"code"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}

	_, err = k.Exec(context.Background(), kitchen.Task{Prompt: "say hi"})
	if err == nil || !strings.Contains(err.Error(), "API status 429") {
		t.Fatalf("err = %v, want API status 429", err)
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
