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
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// LocalRunner talks to any OpenAI-compatible /v1/chat/completions endpoint.
// Default backend: llama-server on http://localhost:8765/v1 (set up by
// scripts/install_local.sh). Compatible with llama-swap, vLLM, LMStudio,
// and Ollama's /v1 endpoint when MILLIWAYS_LOCAL_ENDPOINT is set.
type LocalRunner struct {
	client *http.Client

	mu                sync.Mutex
	endpoint          string
	model             string
	apiKey            string
	temperature       float64 // -1 means "do not send"
	maxTokens         int     // 0 means "do not send"
	sessionIn         int
	sessionOut        int
	sessionDispatches int
}

func NewLocalRunner() *LocalRunner {
	endpoint := os.Getenv("MILLIWAYS_LOCAL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8765/v1"
	}
	model := os.Getenv("MILLIWAYS_LOCAL_MODEL")
	if model == "" {
		model = "qwen2.5-coder-1.5b"
	}
	r := &LocalRunner{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		apiKey:   os.Getenv("MILLIWAYS_LOCAL_API_KEY"),
		// 0.2 is a coding-friendly default — sharpens the distribution toward
		// the highest-probability token without going fully deterministic
		// (some local models loop at exactly 0). Override with /local-temp.
		temperature: 0.2,
		maxTokens:   0,
		client:      &http.Client{Timeout: 0},
	}
	if v := os.Getenv("MILLIWAYS_LOCAL_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			r.temperature = f
		}
	}
	if v := os.Getenv("MILLIWAYS_LOCAL_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			r.maxTokens = n
		}
	}
	return r
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

// SetTemperature sets the sampling temperature. Pass a negative value to
// fall back to the server-side default (i.e. omit the field from the request).
func (r *LocalRunner) SetTemperature(t float64) {
	r.mu.Lock()
	r.temperature = t
	r.mu.Unlock()
}

// SetMaxTokens caps the model's reply length. Pass 0 to omit the field.
func (r *LocalRunner) SetMaxTokens(n int) {
	if n < 0 {
		n = 0
	}
	r.mu.Lock()
	r.maxTokens = n
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
	Endpoint    string
	Model       string
	Temperature float64
	MaxTokens   int
}

func (r *LocalRunner) Settings() LocalSettings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return LocalSettings{
		Endpoint:    r.endpoint,
		Model:       r.model,
		Temperature: r.temperature,
		MaxTokens:   r.maxTokens,
	}
}

// localChatMessage matches the OpenAI Chat Completions schema.
type localChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type localChatRequest struct {
	Model       string             `json:"model"`
	Messages    []localChatMessage `json:"messages"`
	Stream      bool               `json:"stream"`
	Temperature *float64           `json:"temperature,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
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
		fmt.Fprintln(out, ColorText(LocalScheme(), "[local: image attachments ignored]"))
	}

	r.mu.Lock()
	endpoint := r.endpoint
	model := r.model
	apiKey := r.apiKey
	temperature := r.temperature
	maxTokens := r.maxTokens
	r.mu.Unlock()

	messages := buildLocalMessages(req)

	body := localChatRequest{Model: model, Messages: messages, Stream: true, MaxTokens: maxTokens}
	if temperature >= 0 {
		t := temperature
		body.Temperature = &t
	}
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
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := r.client.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("local: connect %s: %w (run scripts/install_local.sh and start llama-server)", endpoint, err)
	}
	defer resp.Body.Close()

	// Ensure ctx-cancel actually unblocks the SSE reader: closing the body
	// causes the next scanner.Scan() to return an error immediately.
	stop := context.AfterFunc(ctx, func() { _ = resp.Body.Close() })
	defer stop()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		lower := strings.ToLower(string(errBody))
		if localBodySignalsLimit(lower) {
			return fmt.Errorf("%w: %s", ErrSessionLimit, string(errBody))
		}
		return fmt.Errorf("local: HTTP %d: %s", resp.StatusCode, string(errBody))
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
// approximate output-token count (chars/4 with utf8 awareness).
func streamLocalSSE(ctx context.Context, body io.Reader, out io.Writer) (int, error) {
	scanner := bufio.NewScanner(body)
	// 8 MiB max line — covers tool-call blobs and large deltas without dropping.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	scheme := LocalScheme()
	useColor := isTerminalWriter(out)
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
			if useColor && !startedLine {
				_, _ = out.Write([]byte(BlackBackground + scheme.FG))
				startedLine = true
			}
			_, _ = out.Write([]byte(c.Delta.Content))
			written += utf8.RuneCountInString(c.Delta.Content) / 4
			if useColor && strings.Contains(c.Delta.Content, "\n") {
				_, _ = out.Write([]byte(ResetColor + BlackBackground + scheme.FG))
			}
		}
	}

	if useColor && startedLine {
		_, _ = out.Write([]byte(ResetColor + "\n"))
	}

	if err := scanner.Err(); err != nil {
		// ctx cancellation closes the body and surfaces here as a read error;
		// prefer the canonical context error.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return written, ctxErr
		}
		// bufio.ErrTooLong indicates a > 8 MiB single SSE event — surface it
		// rather than silently truncating the response.
		if errors.Is(err, bufio.ErrTooLong) {
			return written, fmt.Errorf("local: SSE event larger than 8MiB: %w", err)
		}
		return written, fmt.Errorf("local: read stream: %w", err)
	}
	return written, nil
}

func promptCharsToTokens(req DispatchRequest) int {
	n := utf8.RuneCountInString(req.Prompt) + utf8.RuneCountInString(req.Rules)
	for _, t := range req.History {
		n += utf8.RuneCountInString(t.Text)
	}
	for _, f := range req.Context {
		n += utf8.RuneCountInString(f.Content)
	}
	return n / 4
}

// localBodySignalsLimit recognises context-window / rate-limit exhaustion in
// llama-server / vLLM / Ollama / OpenAI-compatible error bodies.
//
// We deliberately do NOT match a bare "quota" — that produces false positives
// on disk-quota / inode-quota / EDQUOT messages. A real quota signal will use
// "rate limit", "exceeded", or be paired with "tokens"/"requests".
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
		"rate_limit",
		"token limit",
		"insufficient_quota",
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
	apiKey := r.apiKey
	r.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
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

// isTerminalWriter reports whether out is a writer that should receive ANSI
// colour codes. Best-effort: returns true for *os.File pointing at a tty,
// false for anything else (pipes, log files, the shell-buffer multiwriter).
func isTerminalWriter(out io.Writer) bool {
	type fileLike interface {
		Stat() (os.FileInfo, error)
		Fd() uintptr
	}
	f, ok := out.(fileLike)
	if !ok {
		return true // multiwriter case — keep colour, the REPL strips it on disk
	}
	info, err := f.Stat()
	if err != nil {
		return true
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
