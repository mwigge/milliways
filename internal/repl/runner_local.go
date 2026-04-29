// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package repl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// LocalRunner talks to any OpenAI-compatible /v1/chat/completions endpoint.
// Default backend: llama-server on http://localhost:8080/v1 (set up by
// scripts/install_local.sh). Compatible with llama-swap, vLLM, LMStudio,
// and Ollama's /v1 endpoint when MILLIWAYS_LOCAL_ENDPOINT is set.
type LocalRunner struct {
	endpoint string
	model    string
	client   *http.Client

	mu                sync.Mutex
	sessionIn         int
	sessionOut        int
	sessionDispatches int
}

func NewLocalRunner() *LocalRunner {
	endpoint := os.Getenv("MILLIWAYS_LOCAL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8080/v1"
	}
	model := os.Getenv("MILLIWAYS_LOCAL_MODEL")
	if model == "" {
		model = "qwen2.5-coder-1.5b"
	}
	return &LocalRunner{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		client:   &http.Client{Timeout: 0},
	}
}

func (r *LocalRunner) Name() string { return "local" }

func (r *LocalRunner) SetModel(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	r.mu.Lock()
	r.model = model
	r.mu.Unlock()
}

func (r *LocalRunner) SetEndpoint(endpoint string) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return
	}
	r.mu.Lock()
	r.endpoint = strings.TrimRight(endpoint, "/")
	r.mu.Unlock()
}

func (r *LocalRunner) AuthStatus() (bool, error) { return true, nil }
func (r *LocalRunner) Login() error              { return nil }
func (r *LocalRunner) Logout() error             { return nil }

func (r *LocalRunner) Quota() (*QuotaInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessionDispatches == 0 {
		return nil, nil
	}
	return &QuotaInfo{
		Session: &SessionUsage{
			InputTokens:  r.sessionIn,
			OutputTokens: r.sessionOut,
			CostUSD:      0,
			Dispatches:   r.sessionDispatches,
		},
	}, nil
}

type LocalSettings struct {
	Endpoint string
	Model    string
}

func (r *LocalRunner) Settings() LocalSettings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return LocalSettings{Endpoint: r.endpoint, Model: r.model}
}

// localChatMessage matches the OpenAI Chat Completions schema.
type localChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type localChatRequest struct {
	Model    string        `json:"model"`
	Messages []localChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type localChatStreamChoice struct {
	Delta struct {
		Content string `json:"content"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type localChatStreamChunk struct {
	Choices []localChatStreamChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (r *LocalRunner) Execute(ctx context.Context, req DispatchRequest, out io.Writer) error {
	if len(req.Attachments) > 0 {
		// local models we ship with do not handle images
		fmt.Fprintln(out, ColorText(LocalScheme(), "[local: image attachments ignored]"))
	}

	r.mu.Lock()
	endpoint := r.endpoint
	model := r.model
	r.mu.Unlock()

	messages := buildLocalMessages(req)

	body := localChatRequest{Model: model, Messages: messages, Stream: true}
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("local: marshal request: %w", err)
	}

	url := endpoint + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("local: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := r.client.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("local: connect %s: %w (run scripts/install_local.sh and start llama-server)", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		lower := strings.ToLower(string(body))
		if localBodySignalsLimit(lower) {
			return fmt.Errorf("%w: %s", ErrSessionLimit, string(body))
		}
		return fmt.Errorf("local: HTTP %d: %s", resp.StatusCode, string(body))
	}

	tokens, err := streamLocalSSE(ctx, resp.Body, out)
	r.mu.Lock()
	r.sessionIn += promptCharsToTokens(req)
	r.sessionOut += tokens
	r.sessionDispatches++
	r.mu.Unlock()
	return err
}

// buildLocalMessages translates a DispatchRequest into OpenAI-style messages.
// A single system message carries Rules + injected context, then each history
// turn becomes its own role-tagged message, then the new user prompt.
func buildLocalMessages(req DispatchRequest) []localChatMessage {
	out := make([]localChatMessage, 0, len(req.History)+2)

	var sys strings.Builder
	if req.Rules != "" {
		sys.WriteString(req.Rules)
	}
	for _, f := range req.Context {
		if sys.Len() > 0 {
			sys.WriteString("\n\n")
		}
		sys.WriteString("## " + f.Label + "\n\n" + f.Content)
	}
	if sys.Len() > 0 {
		out = append(out, localChatMessage{Role: "system", Content: sys.String()})
	}

	for _, t := range req.History {
		role := strings.ToLower(t.Role)
		if role != "user" && role != "assistant" {
			role = "user"
		}
		out = append(out, localChatMessage{Role: role, Content: t.Text})
	}

	out = append(out, localChatMessage{Role: "user", Content: req.Prompt})
	return out
}

// streamLocalSSE reads a text/event-stream from llama-server / Ollama / vLLM
// and writes the assistant's content to out as it arrives. Returns an
// approximate output-token count (chars/4).
func streamLocalSSE(ctx context.Context, body io.Reader, out io.Writer) (int, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	scheme := LocalScheme()
	var written int
	startedLine := false

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var chunk localChatStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			lower := strings.ToLower(chunk.Error.Message)
			if localBodySignalsLimit(lower) {
				return written, fmt.Errorf("%w: %s", ErrSessionLimit, chunk.Error.Message)
			}
			return written, fmt.Errorf("local: %s", chunk.Error.Message)
		}

		for _, c := range chunk.Choices {
			if c.Delta.Content == "" {
				continue
			}
			if !startedLine {
				_, _ = out.Write([]byte(BlackBackground + scheme.FG))
				startedLine = true
			}
			_, _ = out.Write([]byte(c.Delta.Content))
			written += len(c.Delta.Content) / 4
			if strings.Contains(c.Delta.Content, "\n") {
				_, _ = out.Write([]byte(ResetColor + BlackBackground + scheme.FG))
			}
		}
	}

	if startedLine {
		_, _ = out.Write([]byte(ResetColor + "\n"))
	}

	if err := scanner.Err(); err != nil {
		return written, fmt.Errorf("local: read stream: %w", err)
	}
	return written, nil
}

func promptCharsToTokens(req DispatchRequest) int {
	n := len(req.Prompt) + len(req.Rules)
	for _, t := range req.History {
		n += len(t.Text)
	}
	for _, f := range req.Context {
		n += len(f.Content)
	}
	return n / 4
}

// localBodySignalsLimit recognises context/quota exhaustion in
// llama-server / vLLM / Ollama / OpenAI-compatible error bodies.
func localBodySignalsLimit(lower string) bool {
	patterns := []string{
		"context window",
		"context_length",
		"context length exceeded",
		"context_length_exceeded",
		"maximum context",
		"too many tokens",
		"prompt is too long",
		"rate limit",
		"quota",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// HealthCheck returns nil if the configured endpoint responds.
func (r *LocalRunner) HealthCheck(ctx context.Context) error {
	r.mu.Lock()
	endpoint := r.endpoint
	r.mu.Unlock()

	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("local: backend HTTP %d", resp.StatusCode)
	}
	return nil
}

// ListModels asks the backend for available models. llama-server returns the
// single loaded model; llama-swap returns all configured models.
func (r *LocalRunner) ListModels(ctx context.Context) ([]string, error) {
	r.mu.Lock()
	endpoint := r.endpoint
	r.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local: HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		out = append(out, m.ID)
	}
	return out, nil
}
