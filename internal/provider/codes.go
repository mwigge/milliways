package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/mwigge/milliways/internal/session"
)

const (
	defaultCodesBaseURL = "https://api.codes.ai/v1"
	defaultCodesModel   = "gpt-5.4"
)

// ErrMissingCodesAPIKey indicates that no Codes API key was configured.
var ErrMissingCodesAPIKey = errors.New("codes api key is required")

// CodesProvider sends OpenAI-compatible chat completion requests to Codes.
type CodesProvider struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewCodesProvider builds a Codes provider with sensible defaults.
func NewCodesProvider(apiKey, model string) *CodesProvider {
	return newCodesProvider(apiKey, "", model)
}

func newCodesProvider(apiKey, baseURL, model string) *CodesProvider {
	if strings.TrimSpace(apiKey) == "" {
		apiKey = os.Getenv("CODES_API_KEY")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultCodesBaseURL
	}
	if strings.TrimSpace(model) == "" {
		model = defaultCodesModel
	}

	return &CodesProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
}

// SupportsModel reports whether the Codes provider can handle the given model.
func (p *CodesProvider) SupportsModel(m Model) bool {
	value := strings.TrimSpace(string(m))
	return value == string(ModelCodes) || value == p.model || strings.HasPrefix(value, "codes/")
}

// Send executes a streaming chat completion request.
func (p *CodesProvider) Send(ctx context.Context, req Request) (Response, error) {
	if p == nil {
		return Response{}, errors.New("nil codes provider")
	}
	if !p.SupportsModel(req.Model) {
		return Response{}, fmt.Errorf("unsupported model %q", req.Model)
	}
	if strings.TrimSpace(p.apiKey) == "" {
		return Response{}, ErrMissingCodesAPIKey
	}

	body, err := json.Marshal(buildChatRequest(req, p.model))
	if err != nil {
		return Response{}, fmt.Errorf("marshal codes request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("create codes request: %w", err)
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
		return Response{}, fmt.Errorf("send codes request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		message, readErr := readErrorBody(httpResp.Body)
		if readErr != nil {
			return Response{}, fmt.Errorf("codes status %d: %w", httpResp.StatusCode, readErr)
		}
		return Response{}, fmt.Errorf("codes status %d: %s", httpResp.StatusCode, message)
	}

	return parseStreamResponse(httpResp.Body)
}

var _ Provider = (*CodesProvider)(nil)

var _ = session.Message{}
