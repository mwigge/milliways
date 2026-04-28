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
	"net/url"
	"os"
	"strings"

	"github.com/mwigge/milliways/internal/session"
)

const (
	defaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	defaultGeminiModel   = "gemini-2.5-pro"
)

// ErrMissingGeminiAPIKey indicates that no Gemini API key was configured.
var ErrMissingGeminiAPIKey = errors.New("gemini api key is required")

// GeminiProvider sends generateContent requests to Gemini.
type GeminiProvider struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewGeminiProvider builds a Gemini provider with sensible defaults.
func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	return newGeminiProvider(apiKey, "", model)
}

func newGeminiProvider(apiKey, baseURL, model string) *GeminiProvider {
	if strings.TrimSpace(apiKey) == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultGeminiBaseURL
	}
	if strings.TrimSpace(model) == "" {
		model = defaultGeminiModel
	}

	return &GeminiProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   strings.TrimPrefix(model, "models/"),
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
}

// SupportsModel reports whether the Gemini provider can handle the given model.
func (p *GeminiProvider) SupportsModel(m Model) bool {
	value := strings.TrimSpace(string(m))
	return value == string(ModelGemini) || strings.HasPrefix(value, "gemini") || strings.HasPrefix(value, "models/gemini")
}

// Send executes a Gemini generateContent request.
func (p *GeminiProvider) Send(ctx context.Context, req Request) (Response, error) {
	if p == nil {
		return Response{}, errors.New("nil gemini provider")
	}
	if !p.SupportsModel(req.Model) {
		return Response{}, fmt.Errorf("unsupported model %q", req.Model)
	}
	if strings.TrimSpace(p.apiKey) == "" {
		return Response{}, ErrMissingGeminiAPIKey
	}

	body, err := json.Marshal(buildGeminiRequest(req))
	if err != nil {
		return Response{}, fmt.Errorf("marshal gemini request: %w", err)
	}

	endpoint, err := url.Parse(p.baseURL + "/models/" + p.model + ":generateContent")
	if err != nil {
		return Response{}, fmt.Errorf("parse gemini endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("key", p.apiKey)
	endpoint.RawQuery = query.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("create gemini request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	httpClient := p.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}

	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("send gemini request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		message, readErr := readErrorBody(httpResp.Body)
		if readErr != nil {
			return Response{}, fmt.Errorf("gemini status %d: %w", httpResp.StatusCode, readErr)
		}
		return Response{}, fmt.Errorf("gemini status %d: %s", httpResp.StatusCode, message)
	}

	return parseGeminiResponse(httpResp.Body)
}

type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	ResponseMIMEType string `json:"responseMimeType,omitempty"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *geminiUsage      `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
}

func buildGeminiRequest(req Request) geminiRequest {
	text := buildPromptText(req)
	payload := geminiRequest{
		Contents: []geminiContent{{
			Parts: []geminiPart{{Text: text}},
		}},
	}
	if len(req.Tools) == 0 {
		payload.GenerationConfig.ResponseMIMEType = "application/json"
	}
	return payload
}

func buildPromptText(req Request) string {
	parts := make([]string, 0, len(req.Messages)+1)
	if prompt := strings.TrimSpace(req.SystemPrompt); prompt != "" {
		parts = append(parts, prompt)
	}
	for _, msg := range req.Messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", msg.Role, content))
	}
	return strings.Join(parts, "\n\n")
}

func parseGeminiResponse(body io.Reader) (Response, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return Response{}, fmt.Errorf("read gemini response: %w", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return Response{}, nil
	}
	if strings.HasPrefix(trimmed, "data:") {
		return parseGeminiStream(strings.NewReader(trimmed))
	}
	return decodeGeminiPayload([]byte(trimmed))
}

func parseGeminiStream(body io.Reader) (Response, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var content strings.Builder
	response := Response{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		decoded, err := decodeGeminiPayload([]byte(payload))
		if err != nil {
			return Response{}, err
		}
		content.WriteString(decoded.Content)
		if decoded.Tokens != (TokenCount{}) {
			response.Tokens = decoded.Tokens
		}
	}
	if err := scanner.Err(); err != nil {
		return Response{}, fmt.Errorf("read gemini stream: %w", err)
	}

	response.Content = content.String()
	return response, nil
}

func decodeGeminiPayload(data []byte) (Response, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return Response{}, nil
	}

	if trimmed[0] == '[' {
		var batch []geminiResponse
		if err := json.Unmarshal(trimmed, &batch); err != nil {
			return Response{}, fmt.Errorf("decode gemini stream chunk: %w", err)
		}
		if len(batch) == 0 {
			return Response{}, nil
		}
		return geminiResponseToProvider(batch[len(batch)-1]), nil
	}

	var payload geminiResponse
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return Response{}, fmt.Errorf("decode gemini response: %w", err)
	}
	return geminiResponseToProvider(payload), nil
}

func geminiResponseToProvider(payload geminiResponse) Response {
	response := Response{}
	if len(payload.Candidates) > 0 && len(payload.Candidates[0].Content.Parts) > 0 {
		response.Content = payload.Candidates[0].Content.Parts[0].Text
	}
	if payload.UsageMetadata != nil {
		response.Tokens = TokenCount{
			Input:  payload.UsageMetadata.PromptTokenCount,
			Output: payload.UsageMetadata.CandidatesTokenCount,
		}
	}
	return response
}

var _ Provider = (*GeminiProvider)(nil)

var _ = session.Message{}
