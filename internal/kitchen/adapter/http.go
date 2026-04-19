package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

// HTTPKitchenConfig configures an HTTP-backed kitchen adapter.
type HTTPKitchenConfig struct {
	BaseURL        string
	AuthKey        string
	AuthType       string
	Model          string
	Stations       []string
	Tier           kitchen.CostTier
	ResponseFormat string
	Timeout        time.Duration
}

// HTTPKitchen executes kitchen tasks against a streaming HTTP API.
type HTTPKitchen struct {
	name           string
	baseURL        string
	authKey        string
	authType       string
	model          string
	stations       []string
	costTier       kitchen.CostTier
	responseFormat string
	timeout        time.Duration
	client         *http.Client
}

// Compile-time interface checks.
var _ kitchen.Kitchen = (*HTTPKitchen)(nil)

// NewHTTPKitchen creates an HTTP-backed kitchen.
func NewHTTPKitchen(name string, cfg HTTPKitchenConfig, stations []string, tier kitchen.CostTier) (*HTTPKitchen, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("http kitchen %q: base_url is required", name)
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("http kitchen %q: model is required", name)
	}

	authType := strings.ToLower(cfg.AuthType)
	if authType == "" {
		authType = "bearer"
	}

	responseFormat := strings.ToLower(cfg.ResponseFormat)
	if responseFormat == "" {
		responseFormat = "openai"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	effectiveTier := tier
	if cfg.Tier != kitchen.CostTierUnknown {
		effectiveTier = cfg.Tier
	}

	effectiveStations := append([]string(nil), stations...)
	if len(cfg.Stations) > 0 {
		effectiveStations = append([]string(nil), cfg.Stations...)
	}

	return &HTTPKitchen{
		name:           name,
		baseURL:        strings.TrimSuffix(cfg.BaseURL, "/"),
		authKey:        cfg.AuthKey,
		authType:       authType,
		model:          cfg.Model,
		stations:       effectiveStations,
		costTier:       effectiveTier,
		responseFormat: responseFormat,
		timeout:        timeout,
		client:         &http.Client{Timeout: timeout},
	}, nil
}

// Name returns the configured kitchen name.
func (k *HTTPKitchen) Name() string { return k.name }

// Stations returns the kitchen's supported stations.
func (k *HTTPKitchen) Stations() []string { return append([]string(nil), k.stations...) }

// CostTier returns the kitchen's configured cost tier.
func (k *HTTPKitchen) CostTier() kitchen.CostTier { return k.costTier }

// Status reports whether the kitchen has the required API credential.
// Kitchens with no authKey (e.g. local Ollama) are always Ready.
func (k *HTTPKitchen) Status() kitchen.Status {
	if k.authKey == "" {
		return kitchen.Ready
	}
	if os.Getenv(k.authKey) == "" {
		return kitchen.NeedsAuth
	}
	return kitchen.Ready
}

// Exec sends a task to the HTTP API and streams the response.
func (k *HTTPKitchen) Exec(ctx context.Context, task kitchen.Task) (kitchen.Result, error) {
	start := time.Now()
	if k.Status() != kitchen.Ready {
		return kitchen.Result{ExitCode: 1, Duration: time.Since(start)}, fmt.Errorf("%s kitchen not ready: %s", k.name, k.Status())
	}

	// Only inject auth header if authKey is configured (local kitchens like Ollama have none)
	var apiKey string
	if k.authKey != "" {
		apiKey = os.Getenv(k.authKey)
		if apiKey == "" {
			return kitchen.Result{ExitCode: 1, Duration: time.Since(start)}, fmt.Errorf("%s: API key not set", k.authKey)
		}
	}

	reqBody, err := k.buildRequestBody(task.Prompt)
	if err != nil {
		return kitchen.Result{ExitCode: 1, Duration: time.Since(start)}, fmt.Errorf("build request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.buildEndpoint(), strings.NewReader(reqBody))
	if err != nil {
		return kitchen.Result{ExitCode: 1, Duration: time.Since(start)}, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if k.authType == "apikey" {
		req.Header.Set("X-API-Key", apiKey)
	} else {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	}
	if k.responseFormat == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	resp, err := k.client.Do(req)
	if err != nil {
		return kitchen.Result{ExitCode: 1, Duration: time.Since(start)}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return kitchen.Result{ExitCode: 1, Duration: time.Since(start)}, fmt.Errorf("API status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	reader := bufio.NewReader(resp.Body)
	var output strings.Builder

	for {
		if err := ctx.Err(); err != nil {
			return kitchen.Result{ExitCode: 0, Output: output.String(), Duration: time.Since(start)}, err
		}

		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			switch {
			case readErr == io.EOF:
				return kitchen.Result{ExitCode: 0, Output: output.String(), Duration: time.Since(start)}, nil
			case ctx.Err() != nil:
				return kitchen.Result{ExitCode: 0, Output: output.String(), Duration: time.Since(start)}, ctx.Err()
			case output.Len() > 0:
				return kitchen.Result{ExitCode: 0, Output: output.String(), Duration: time.Since(start)}, nil
			default:
				return kitchen.Result{ExitCode: 1, Output: output.String(), Duration: time.Since(start)}, fmt.Errorf("reading response: %w", readErr)
			}
		}

		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "" || payload == "[DONE]" {
			continue
		}

		content, done := k.parseChunk(payload)
		if content != "" {
			output.WriteString(content)
			if task.OnLine != nil {
				task.OnLine(content)
			}
		}
		if done {
			return kitchen.Result{ExitCode: 0, Output: output.String(), Duration: time.Since(start)}, nil
		}
	}
}

func (k *HTTPKitchen) buildEndpoint() string {
	switch k.responseFormat {
	case "anthropic":
		return k.baseURL + "/v1/messages"
	case "ollama":
		return k.baseURL + "/api/chat"
	default:
		return k.baseURL + "/chat/completions"
	}
}

func (k *HTTPKitchen) buildRequestBody(prompt string) (string, error) {
	var (
		data []byte
		err  error
	)

	switch k.responseFormat {
	case "anthropic":
		data, err = json.Marshal(anthropicRequest{
			Model:     k.model,
			Stream:    true,
			MaxTokens: 8192,
			Messages: []anthropicMessage{{
				Role: "user",
				Content: []anthropicContent{{
					Type: "text",
					Text: prompt,
				}},
			}},
		})
	case "ollama":
		data, err = json.Marshal(ollamaRequest{
			Model:  k.model,
			Stream: true,
			Messages: []openAIMessage{{
				Role:    "user",
				Content: prompt,
			}},
		})
	default:
		data, err = json.Marshal(openAIRequest{
			Model:  k.model,
			Stream: true,
			Messages: []openAIMessage{{
				Role:    "user",
				Content: prompt,
			}},
		})
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (k *HTTPKitchen) parseChunk(line string) (content string, done bool) {
	switch k.responseFormat {
	case "anthropic":
		return parseAnthropicChunk(line)
	case "ollama":
		return parseOllamaChunk(line)
	case "minimax":
		return parseOpenAIChunk(line)
	default:
		return parseOpenAIChunk(line)
	}
}

type openAIRequest struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []openAIMessage `json:"messages"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	Stream    bool               `json:"stream"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []openAIMessage `json:"messages"`
}

func parseOpenAIChunk(line string) (content string, done bool) {
	var chunk struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Delta        struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(line), &chunk); err != nil {
		return "", false
	}
	if len(chunk.Choices) == 0 {
		return "", false
	}
	if chunk.Choices[0].FinishReason == "stop" && chunk.Choices[0].Delta.Content == "" {
		return "", true
	}
	return chunk.Choices[0].Delta.Content, false
}

func parseAnthropicChunk(line string) (content string, done bool) {
	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(line), &base); err != nil {
		return "", false
	}
	if base.Type == "message_stop" {
		return "", true
	}
	if base.Type != "content_block_delta" {
		return "", false
	}

	var chunk struct {
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(line), &chunk); err != nil {
		return "", false
	}
	if chunk.Delta.Type != "text_delta" {
		return "", false
	}
	return chunk.Delta.Text, false
}

func parseOllamaChunk(line string) (content string, done bool) {
	var chunk struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Done bool `json:"done"`
	}
	if err := json.Unmarshal([]byte(line), &chunk); err != nil {
		return "", false
	}
	return chunk.Message.Content, chunk.Done
}

type httpKitchenAdapter struct {
	kitchen *HTTPKitchen
	opts    AdapterOpts
}

var _ Adapter = (*httpKitchenAdapter)(nil)

func newHTTPKitchenAdapter(k *HTTPKitchen, opts AdapterOpts) *httpKitchenAdapter {
	return &httpKitchenAdapter{kitchen: k, opts: opts}
}

func (a *httpKitchenAdapter) Exec(ctx context.Context, task kitchen.Task) (<-chan Event, error) {
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)

		originalOnLine := task.OnLine
		task.OnLine = func(line string) {
			if originalOnLine != nil {
				originalOnLine(line)
			}
			ch <- Event{Type: EventText, Kitchen: a.kitchen.Name(), Text: line}
		}

		result, err := a.kitchen.Exec(ctx, task)
		if err != nil {
			ch <- Event{Type: EventError, Kitchen: a.kitchen.Name(), Text: err.Error()}
		}
		ch <- Event{Type: EventDone, Kitchen: a.kitchen.Name(), ExitCode: result.ExitCode}
	}()
	return ch, nil
}

func (a *httpKitchenAdapter) Send(_ context.Context, _ string) error { return ErrNotInteractive }

func (a *httpKitchenAdapter) SupportsResume() bool { return false }

func (a *httpKitchenAdapter) SessionID() string { return "" }

func (a *httpKitchenAdapter) Capabilities() Capabilities {
	return Capabilities{
		NativeResume:        false,
		InteractiveSend:     false,
		StructuredEvents:    true,
		ExhaustionDetection: "none",
	}
}
