package repl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/maitre"
)

// MinimaxReasoningMode controls how much live progress MiniMax shows during execution.
type MinimaxReasoningMode string

const (
	MinimaxReasoningOff     MinimaxReasoningMode = "off"
	MinimaxReasoningSummary MinimaxReasoningMode = "summary"
	MinimaxReasoningVerbose MinimaxReasoningMode = "verbose"
)

// MinimaxSettings captures the current runner configuration.
type MinimaxSettings struct {
	Model         string
	ReasoningMode MinimaxReasoningMode
	URL           string
}

type MinimaxRunner struct {
	apiKey        string
	model         string
	url           string
	client        *http.Client
	reasoningMode MinimaxReasoningMode

	mu                sync.Mutex
	sessionIn         int
	sessionOut        int
	sessionCostUSD    float64
	sessionDispatches int
}

func NewMinimaxRunner(apiKey, model, url string) *MinimaxRunner {
	if model == "" {
		model = "MiniMax-M2.7"
	}
	if url == "" {
		url = "https://api.minimax.io/v1/text/chatcompletion_v2"
	}
	return &MinimaxRunner{
		apiKey:        apiKey,
		model:         model,
		url:           url,
		client:        &http.Client{Timeout: 5 * time.Minute},
		reasoningMode: MinimaxReasoningVerbose,
	}
}

func (r *MinimaxRunner) Name() string { return "minimax" }

func (r *MinimaxRunner) SetModel(model string) {
	r.model = strings.TrimSpace(model)
}

func (r *MinimaxRunner) SetReasoningMode(mode MinimaxReasoningMode) {
	switch mode {
	case MinimaxReasoningOff, MinimaxReasoningSummary, MinimaxReasoningVerbose:
		r.reasoningMode = mode
	default:
		r.reasoningMode = MinimaxReasoningSummary
	}
}

func (r *MinimaxRunner) Settings() MinimaxSettings {
	return MinimaxSettings{
		Model:         r.model,
		ReasoningMode: r.reasoningMode,
		URL:           r.url,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatDelta struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"` // MiniMax thinking field
	Thinking         string `json:"thinking,omitempty"`           // alternate thinking field
}

type chatChoice struct {
	Delta        chatDelta  `json:"delta"`
	Message      *chatDelta `json:"message,omitempty"` // non-streaming fallback
	FinishReason string     `json:"finish_reason,omitempty"`
}

type minimaxUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatResponse struct {
	Choices []chatChoice  `json:"choices"`
	Usage   *minimaxUsage `json:"usage,omitempty"`
}

func (r *MinimaxRunner) Execute(ctx context.Context, req DispatchRequest, out io.Writer) error {
	if len(req.Attachments) > 0 {
		slog.Warn("minimax: image attachments not supported, proceeding with text only",
			"count", len(req.Attachments))
	}

	var messages []chatMessage
	if req.Rules != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.Rules})
	}
	for _, t := range req.History {
		messages = append(messages, chatMessage{Role: t.Role, Content: t.Text})
	}

	// Prepend context fragments as an additional user message before the prompt.
	if len(req.Context) > 0 {
		var sb strings.Builder
		for _, f := range req.Context {
			sb.WriteString("## " + f.Label + "\n\n")
			sb.WriteString(f.Content + "\n\n")
		}
		messages = append(messages, chatMessage{Role: "user", Content: sb.String()})
	}

	messages = append(messages, chatMessage{Role: "user", Content: req.Prompt})

	payload := map[string]any{
		"model":    r.model,
		"messages": messages,
		"stream":   true,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", r.url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)

	usage, err := runMinimaxSSE(ctx, r.client, httpReq, r.model, out, r.reasoningMode)
	if usage != nil {
		r.mu.Lock()
		r.sessionIn += usage.PromptTokens
		r.sessionOut += usage.CompletionTokens
		r.sessionDispatches++
		r.mu.Unlock()
	}
	return err
}

func (r *MinimaxRunner) AuthStatus() (bool, error) {
	return r.apiKey != "", nil
}

func (r *MinimaxRunner) Login() error {
	if r.apiKey != "" {
		fmt.Println("minimax: already authenticated (API key set)")
		return nil
	}
	return maitre.LoginAPIKey("minimax")
}

func (r *MinimaxRunner) Logout() error {
	return nil
}

func (r *MinimaxRunner) Quota() (*QuotaInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessionDispatches == 0 {
		return nil, nil
	}
	return &QuotaInfo{
		Session: &SessionUsage{
			InputTokens:  r.sessionIn,
			OutputTokens: r.sessionOut,
			CostUSD:      r.sessionCostUSD,
			Dispatches:   r.sessionDispatches,
		},
	}, nil
}

// minimaxThinkFilter strips <think>...</think> blocks from streaming content,
// routing thinking text to writeThink and regular content to writeText.
type minimaxThinkFilter struct {
	thinking bool
	buf      strings.Builder
}

func (f *minimaxThinkFilter) write(chunk string, writeText func(string), writeThink func(string)) {
	for len(chunk) > 0 {
		if f.thinking {
			idx := strings.Index(chunk, "</think>")
			if idx >= 0 {
				f.buf.WriteString(chunk[:idx])
				if f.buf.Len() > 0 {
					writeThink(f.buf.String())
					f.buf.Reset()
				}
				f.thinking = false
				chunk = chunk[idx+len("</think>"):]
			} else {
				f.buf.WriteString(chunk)
				return
			}
		} else {
			idx := strings.Index(chunk, "<think>")
			if idx >= 0 {
				if idx > 0 {
					writeText(chunk[:idx])
				}
				f.thinking = true
				chunk = chunk[idx+len("<think>"):]
			} else {
				writeText(chunk)
				return
			}
		}
	}
}

func runMinimaxSSE(ctx context.Context, client *http.Client, req *http.Request, model string, out io.Writer, reasoningMode MinimaxReasoningMode) (*minimaxUsage, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("minimax API error %d: %s", resp.StatusCode, string(body))
	}

	scheme := MiniMaxScheme()

	writeText := func(text string) {
		_, _ = out.Write([]byte(ColorText(scheme, text)))
	}

	writeProgress := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		_, _ = out.Write([]byte(AccentColorText(scheme, text) + "\n"))
	}

	if reasoningMode != MinimaxReasoningOff {
		writeProgress(fmt.Sprintf("* minimax: start  model:%s", model))
	}

	start := time.Now()
	var finalUsage *minimaxUsage
	var lineBuf strings.Builder
	var thinkFilter minimaxThinkFilter

	flushLine := func() {
		line := lineBuf.String()
		lineBuf.Reset()
		if line == "" {
			return
		}
		writeText(line + "\n")
	}

	appendContent := func(content string) {
		thinkFilter.write(content,
			// regular content: buffer line by line
			func(text string) {
				for {
					nl := strings.IndexByte(text, '\n')
					if nl < 0 {
						lineBuf.WriteString(text)
						break
					}
					lineBuf.WriteString(text[:nl])
					flushLine()
					text = text[nl+1:]
				}
			},
			// thinking content: emit as progress if mode allows
			func(thinking string) {
				if reasoningMode == MinimaxReasoningOff {
					return
				}
				summary := oneLine(strings.TrimSpace(thinking))
				if summary != "" {
					writeProgress("* minimax: thinking - " + summary)
				}
			},
		)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return finalUsage, ctx.Err()
		default:
		}

		line := scanner.Text()

		// Accept both SSE ("data: {...}") and bare NDJSON ("{...}") lines.
		var jsonData string
		switch {
		case strings.HasPrefix(line, "data: "):
			jsonData = strings.TrimPrefix(line, "data: ")
			if jsonData == "[DONE]" {
				goto done
			}
		case strings.HasPrefix(line, "{"):
			jsonData = line
		default:
			continue
		}

		{
			var cr chatResponse
			if err := json.Unmarshal([]byte(jsonData), &cr); err != nil {
				continue
			}

			if cr.Usage != nil {
				finalUsage = cr.Usage
			}

			for _, choice := range cr.Choices {
				// Streaming uses delta.content; non-streaming fallback uses message.content.
				delta := choice.Delta
				if choice.Message != nil && delta.Content == "" {
					delta = *choice.Message
				}

				// Surface explicit reasoning fields (other models).
				if reasoningMode != MinimaxReasoningOff {
					thinking := firstNonEmpty(delta.ReasoningContent, delta.Thinking)
					if thinking != "" {
						writeProgress("* minimax: thinking - " + oneLine(thinking))
					}
				}

				if delta.Content != "" {
					appendContent(delta.Content)
				}
			}
		}
	}
done:

	// Flush any remaining buffered content.
	if lineBuf.Len() > 0 {
		flushLine()
	}

	if reasoningMode != MinimaxReasoningOff {
		elapsed := time.Since(start)
		parts := []string{fmt.Sprintf("ok minimax: done  %.1fs", elapsed.Seconds())}
		if finalUsage != nil {
			parts = append(parts, fmt.Sprintf("%din/%dout", finalUsage.PromptTokens, finalUsage.CompletionTokens))
		}
		writeProgress(strings.Join(parts, "  "))
	}

	return finalUsage, nil
}
