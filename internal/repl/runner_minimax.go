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

// MinimaxModelKind identifies which MiniMax API a given model routes to.
type MinimaxModelKind string

const (
	MinimaxKindChat   MinimaxModelKind = "chat"
	MinimaxKindImage  MinimaxModelKind = "image"
	MinimaxKindMusic  MinimaxModelKind = "music"
	MinimaxKindLyrics MinimaxModelKind = "lyrics"
)

const (
	minimaxChatURL   = "https://api.minimax.io/v1/text/chatcompletion_v2"
	minimaxImageURL  = "https://api.minimax.io/v1/image_generation"
	minimaxMusicURL  = "https://api.minimax.io/v1/music_generation"
	minimaxLyricsURL = "https://api.minimax.io/v1/lyrics_generation"
)

type minimaxModelEntry struct {
	ID   string
	Kind MinimaxModelKind
	Note string
}

// MinimaxModelCatalog is the full set of models exposed via /minimax-model.
var MinimaxModelCatalog = []minimaxModelEntry{
	// Chat — OpenAI-compatible endpoint
	{"MiniMax-M2.7", MinimaxKindChat, "~60 tps, recursive self-improvement"},
	{"MiniMax-M2.7-highspeed", MinimaxKindChat, "~100 tps"},
	{"MiniMax-M2.5", MinimaxKindChat, "~60 tps, peak performance"},
	{"MiniMax-M2.5-highspeed", MinimaxKindChat, "~100 tps"},
	{"MiniMax-M2.1", MinimaxKindChat, "~60 tps, enhanced programming"},
	{"MiniMax-M2.1-highspeed", MinimaxKindChat, "~100 tps"},
	{"MiniMax-M2", MinimaxKindChat, "agentic + advanced reasoning"},
	// Image
	{"image-01", MinimaxKindImage, "text-to-image"},
	{"image-01-live", MinimaxKindImage, "image-to-image with reference"},
	// Music
	{"music-2.6", MinimaxKindMusic, ""},
	{"music-2.6-free", MinimaxKindMusic, "free tier"},
	{"music-cover", MinimaxKindMusic, "cover generation"},
	{"music-cover-free", MinimaxKindMusic, "cover, free tier"},
	// Lyrics (no model field in the API — "lyrics" is a virtual model name)
	{"lyrics", MinimaxKindLyrics, "write_full_song or edit mode"},
}

func minimaxLookup(model string) (MinimaxModelKind, bool) {
	for _, e := range MinimaxModelCatalog {
		if e.ID == model {
			return e.Kind, true
		}
	}
	return MinimaxKindChat, false
}

// MinimaxSettings captures the current runner configuration.
type MinimaxSettings struct {
	Model         string
	Kind          MinimaxModelKind
	ReasoningMode MinimaxReasoningMode
	URL           string
}

type MinimaxRunner struct {
	apiKey        string
	model         string
	kind          MinimaxModelKind
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
		url = minimaxChatURL
	}
	kind, _ := minimaxLookup(model)
	return &MinimaxRunner{
		apiKey:        apiKey,
		model:         model,
		kind:          kind,
		url:           url,
		client:        &http.Client{Timeout: 5 * time.Minute},
		reasoningMode: MinimaxReasoningVerbose,
	}
}

func (r *MinimaxRunner) Name() string { return "minimax" }

func (r *MinimaxRunner) SetModel(model string) {
	model = strings.TrimSpace(model)
	r.model = model
	kind, known := minimaxLookup(model)
	r.kind = kind
	if !known {
		// Unknown model — assume chat on the existing URL.
		return
	}
	switch kind {
	case MinimaxKindImage:
		r.url = minimaxImageURL
	case MinimaxKindMusic:
		r.url = minimaxMusicURL
	case MinimaxKindLyrics:
		r.url = minimaxLyricsURL
	default:
		r.url = minimaxChatURL
	}
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
		Kind:          r.kind,
		ReasoningMode: r.reasoningMode,
		URL:           r.url,
	}
}

// ----- Execute dispatch -----

func (r *MinimaxRunner) Execute(ctx context.Context, req DispatchRequest, out io.Writer) error {
	switch r.kind {
	case MinimaxKindImage:
		return r.executeImage(ctx, req, out)
	case MinimaxKindMusic:
		return r.executeMusic(ctx, req, out)
	case MinimaxKindLyrics:
		return r.executeLyrics(ctx, req, out)
	default:
		return r.executeChat(ctx, req, out)
	}
}

// ----- Chat -----

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatDelta struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
	Thinking         string `json:"thinking,omitempty"`
}

type chatChoice struct {
	Delta        chatDelta  `json:"delta"`
	Message      *chatDelta `json:"message,omitempty"`
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

func (r *MinimaxRunner) executeChat(ctx context.Context, req DispatchRequest, out io.Writer) error {
	if len(req.Attachments) > 0 {
		slog.Warn("minimax: image attachments not supported, proceeding with text only",
			"count", len(req.Attachments))
	}

	var messages []chatMessage
	// req.Rules contains CLAUDE.md which has Claude Code-specific agent/skill
	// orchestration instructions that confuse raw API models. HTTP runners receive
	// the conversation and prompt directly without these meta-instructions.
	for _, t := range req.History {
		messages = append(messages, chatMessage{Role: t.Role, Content: t.Text})
	}

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

// ----- Image -----

type minimaxImageResponse struct {
	Data struct {
		ImageURLs   []string `json:"image_urls"`
		ImageBase64 []string `json:"image_base64"`
	} `json:"data"`
	Metadata struct {
		SuccessCount json.Number `json:"success_count"`
		FailedCount  json.Number `json:"failed_count"`
	} `json:"metadata"`
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

func (r *MinimaxRunner) executeImage(ctx context.Context, req DispatchRequest, out io.Writer) error {
	scheme := MiniMaxScheme()
	writeProgress := func(text string) {
		_, _ = out.Write([]byte(AccentColorText(scheme, text) + "\n"))
	}

	if r.reasoningMode != MinimaxReasoningOff {
		writeProgress(fmt.Sprintf("* minimax: start  model:%s", r.model))
	}

	payload := map[string]any{
		"model":  r.model,
		"prompt": req.Prompt,
		"n":      1,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", minimaxImageURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)

	start := time.Now()
	resp, err := r.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		return fmt.Errorf("minimax image API error %d: %s", resp.StatusCode, string(body))
	}

	var ir minimaxImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		return fmt.Errorf("minimax image: decode error: %w", err)
	}
	if ir.BaseResp.StatusCode != 0 {
		return fmt.Errorf("minimax image: %s (code %d)", ir.BaseResp.StatusMsg, ir.BaseResp.StatusCode)
	}

	for _, u := range ir.Data.ImageURLs {
		_, _ = out.Write([]byte(ColorText(scheme, u) + "\n"))
	}
	if len(ir.Data.ImageURLs) == 0 {
		_, _ = out.Write([]byte(MutedText("no images returned") + "\n"))
	}

	if r.reasoningMode != MinimaxReasoningOff {
		writeProgress(fmt.Sprintf("ok minimax: done  %.1fs  %d image(s)", time.Since(start).Seconds(), len(ir.Data.ImageURLs)))
	}

	r.mu.Lock()
	r.sessionDispatches++
	r.mu.Unlock()
	return nil
}

// ----- Music -----

type minimaxMusicResponse struct {
	Data struct {
		Status int    `json:"status"` // 1=in-progress, 2=completed
		Audio  string `json:"audio"`  // URL when output_format=url
	} `json:"data"`
	ExtraInfo struct {
		MusicDuration string `json:"music_duration"`
	} `json:"extra_info"`
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

func (r *MinimaxRunner) executeMusic(ctx context.Context, req DispatchRequest, out io.Writer) error {
	scheme := MiniMaxScheme()
	writeProgress := func(text string) {
		_, _ = out.Write([]byte(AccentColorText(scheme, text) + "\n"))
	}

	if r.reasoningMode != MinimaxReasoningOff {
		writeProgress(fmt.Sprintf("* minimax: start  model:%s", r.model))
	}

	payload := map[string]any{
		"model":         r.model,
		"prompt":        req.Prompt,
		"output_format": "url",
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", minimaxMusicURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)

	start := time.Now()
	resp, err := r.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		return fmt.Errorf("minimax music API error %d: %s", resp.StatusCode, string(body))
	}

	var mr minimaxMusicResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return fmt.Errorf("minimax music: decode error: %w", err)
	}
	if mr.BaseResp.StatusCode != 0 {
		return fmt.Errorf("minimax music: %s (code %d)", mr.BaseResp.StatusMsg, mr.BaseResp.StatusCode)
	}

	if mr.Data.Audio != "" {
		_, _ = out.Write([]byte(ColorText(scheme, mr.Data.Audio) + "\n"))
	} else {
		_, _ = out.Write([]byte(MutedText("no audio returned") + "\n"))
	}

	if r.reasoningMode != MinimaxReasoningOff {
		dur := ""
		if mr.ExtraInfo.MusicDuration != "" {
			dur = "  duration:" + mr.ExtraInfo.MusicDuration
		}
		writeProgress(fmt.Sprintf("ok minimax: done  %.1fs%s", time.Since(start).Seconds(), dur))
	}

	r.mu.Lock()
	r.sessionDispatches++
	r.mu.Unlock()
	return nil
}

// ----- Lyrics -----

type minimaxLyricsResponse struct {
	SongTitle string `json:"song_title"`
	StyleTags string `json:"style_tags"`
	Lyrics    string `json:"lyrics"`
	BaseResp  struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

func (r *MinimaxRunner) executeLyrics(ctx context.Context, req DispatchRequest, out io.Writer) error {
	scheme := MiniMaxScheme()
	writeProgress := func(text string) {
		_, _ = out.Write([]byte(AccentColorText(scheme, text) + "\n"))
	}

	if r.reasoningMode != MinimaxReasoningOff {
		writeProgress("* minimax: start  model:lyrics")
	}

	payload := map[string]any{
		"mode":   "write_full_song",
		"prompt": req.Prompt,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", minimaxLyricsURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)

	start := time.Now()
	resp, err := r.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		return fmt.Errorf("minimax lyrics API error %d: %s", resp.StatusCode, string(body))
	}

	var lr minimaxLyricsResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return fmt.Errorf("minimax lyrics: decode error: %w", err)
	}
	if lr.BaseResp.StatusCode != 0 {
		return fmt.Errorf("minimax lyrics: %s (code %d)", lr.BaseResp.StatusMsg, lr.BaseResp.StatusCode)
	}

	if lr.SongTitle != "" {
		_, _ = out.Write([]byte(AccentColorText(scheme, "# "+lr.SongTitle) + "\n"))
	}
	if lr.StyleTags != "" {
		_, _ = out.Write([]byte(MutedText(lr.StyleTags) + "\n\n"))
	}
	if lr.Lyrics != "" {
		_, _ = out.Write([]byte(ColorText(scheme, lr.Lyrics) + "\n"))
	}

	if r.reasoningMode != MinimaxReasoningOff {
		writeProgress(fmt.Sprintf("ok minimax: done  %.1fs", time.Since(start).Seconds()))
	}

	r.mu.Lock()
	r.sessionDispatches++
	r.mu.Unlock()
	return nil
}

// ----- Auth / Quota -----

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

// ----- SSE chat streaming -----

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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
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
				delta := choice.Delta
				if choice.Message != nil && delta.Content == "" {
					delta = *choice.Message
				}

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

	if lineBuf.Len() > 0 {
		flushLine()
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return finalUsage, fmt.Errorf("minimax: SSE read error: %w", scanErr)
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
