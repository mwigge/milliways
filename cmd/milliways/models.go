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

package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// knownModels is the curated fallback list for each runner. These are used
// when the runner authenticates via OAuth and the OAuth token is not compatible
// with the provider's model-listing API (e.g. codex ChatGPT OAuth ≠ OpenAI
// developer API; Claude Code OAuth ≠ Anthropic API). Updated with each
// milliways release to track current CLI model offerings.
var knownModels = map[string][]string{
	// Codex model list from the codex CLI's interactive /model picker.
	// ChatGPT OAuth tokens are scoped for chatgpt.com, not api.openai.com.
	"codex": {
		"gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
		"gpt-5.2",
	},
	// Claude models from Anthropic's public model page.
	// Claude Code uses Anthropic OAuth, not a developer API key.
	"claude": {
		"claude-opus-4-7",
		"claude-opus-4-5",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-20240229",
	},
	// Gemini models. gemini-cli uses Google OAuth scoped for Code Assist,
	// not the Generative Language API — the access token returns 403 there.
	// GEMINI_API_KEY / GOOGLE_API_KEY enables live fetch.
	"gemini": {
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.0-flash",
		"gemini-2.0-flash-lite",
		"gemini-1.5-pro",
		"gemini-1.5-flash",
	},
	// Copilot models from GitHub Copilot API (also live-fetched via OAuth).
	"copilot": {
		"gpt-4.5",
		"gpt-4o",
		"claude-sonnet-4-5",
		"claude-opus-4-5",
		"gemini-2.0-flash",
	},
}

// modelCache caches the result of a provider model-list fetch so /model
// does not make a live API call on every invocation.
type modelCache struct {
	mu      sync.RWMutex
	entries map[string]modelCacheEntry
	ttl     time.Duration
	client  *http.Client
}

type modelCacheEntry struct {
	models    []string
	fetchedAt time.Time
	fetching  bool // true while a background fetch is in flight
}

var globalModelCache = &modelCache{
	entries: make(map[string]modelCacheEntry),
	ttl:     time.Hour,
	client:  &http.Client{Timeout: 5 * time.Second},
}

// Models returns the live model list for agentID. Returns ["(fetching…)"]
// while a background fetch is in flight so callers can show a loading state.
// Falls back to knownModels when a live fetch is unavailable.
func (c *modelCache) Models(agentID string) []string {
	c.mu.RLock()
	e, ok := c.entries[agentID]
	c.mu.RUnlock()
	if ok && e.fetching {
		return []string{"(fetching…)"}
	}
	if ok && time.Since(e.fetchedAt) < c.ttl {
		return e.models
	}
	// Re-check inside write lock to prevent two concurrent callers both
	// starting a fetch (TOCTOU between the RLock check above and here).
	c.mu.Lock()
	e2 := c.entries[agentID]
	if e2.fetching || time.Since(e2.fetchedAt) < c.ttl {
		c.mu.Unlock()
		if e2.fetching {
			return []string{"(fetching…)"}
		}
		return e2.models
	}
	c.entries[agentID] = modelCacheEntry{fetching: true}
	c.mu.Unlock()

	models := c.fetch(agentID)
	if len(models) == 0 {
		models = knownModels[agentID] // curated fallback
	}

	c.mu.Lock()
	c.entries[agentID] = modelCacheEntry{models: models, fetchedAt: time.Now()}
	c.mu.Unlock()
	return models
}

// RefreshAsync starts a background goroutine to refresh the model cache for
// all runners at startup so /model shows live data without blocking.
func (c *modelCache) RefreshAsync() {
	for _, agentID := range []string{"claude", "codex", "copilot", "gemini", "minimax"} {
		go func(id string) { c.Models(id) }(agentID)
	}
}

// fetch attempts a live model list for agentID using whatever credentials
// are available. Returns nil if no live fetch is possible — the caller will
// use knownModels as the fallback.
//
// Auth strategy per runner:
//   - minimax:  MINIMAX_API_KEY env var → live API call
//   - copilot:  OAuth token from ~/.copilot/ or ~/.config/github-copilot/ → live API call
//   - gemini:   GEMINI_API_KEY / GOOGLE_API_KEY env var → live API call
//               (~/.gemini/oauth_creds.json token returns 403 — wrong OAuth scope)
//   - codex:    OPENAI_API_KEY env var → live API call
//               (ChatGPT OAuth token is scoped for chatgpt.com, not api.openai.com)
//   - claude:   ANTHROPIC_API_KEY env var → live API call
//               (Claude Code OAuth is scoped for Claude Code, not api.anthropic.com)
func (c *modelCache) fetch(agentID string) []string {
	switch agentID {
	case "minimax":
		return c.fetchMiniMax(os.Getenv("MINIMAX_API_KEY"))

	case "copilot":
		return c.fetchCopilot()

	case "gemini":
		key := os.Getenv("GEMINI_API_KEY")
		if key == "" {
			key = os.Getenv("GOOGLE_API_KEY")
		}
		return c.fetchGemini(key)

	case "codex":
		return c.fetchOpenAI(os.Getenv("OPENAI_API_KEY"), "gpt", "o1", "o3", "o4")

	case "claude":
		return c.fetchAnthropic(os.Getenv("ANTHROPIC_API_KEY"))
	}
	return nil
}

// ---- provider fetchers ----

func (c *modelCache) fetchOpenAI(apiKey string, prefixes ...string) []string {
	if apiKey == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/models", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.client.Do(req)
	if err != nil {
		slog.Debug("models fetch failed", "provider", "openai", "err", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	var out []string
	for _, m := range payload.Data {
		for _, p := range prefixes {
			if strings.HasPrefix(m.ID, p) {
				out = append(out, m.ID)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func (c *modelCache) fetchAnthropic(apiKey string) []string {
	if apiKey == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := c.client.Do(req)
	if err != nil {
		slog.Debug("models fetch failed", "provider", "anthropic", "err", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	var out []string
	for _, m := range payload.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out
}

func (c *modelCache) fetchGemini(apiKey string) []string {
	if apiKey == "" {
		return nil
	}
	// Use X-Goog-Api-Key header so the key is not captured in URL logs.
	req, err := http.NewRequest(http.MethodGet,
		"https://generativelanguage.googleapis.com/v1beta/models", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("X-Goog-Api-Key", apiKey)
	resp, err := c.client.Do(req)
	if err != nil {
		slog.Debug("models fetch failed", "provider", "gemini", "err", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	var out []string
	for _, m := range payload.Models {
		id := strings.TrimPrefix(m.Name, "models/")
		if strings.HasPrefix(id, "gemini") {
			out = append(out, id)
		}
	}
	return out
}

func (c *modelCache) fetchMiniMax(apiKey string) []string {
	if apiKey == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, "https://api.minimax.io/v1/models", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.client.Do(req)
	if err != nil {
		slog.Debug("models fetch failed", "provider", "minimax", "err", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	var out []string
	for _, m := range payload.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out
}

// fetchCopilot fetches the GitHub Copilot model list using the OAuth token
// stored by the copilot CLI. Returns nil if no token is found.
func (c *modelCache) fetchCopilot() []string {
	token := copilotToken()
	if token == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, "https://api.githubcopilot.com/models", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	resp, err := c.client.Do(req)
	if err != nil {
		slog.Debug("models fetch failed", "provider", "copilot", "err", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	var out []string
	for _, m := range payload.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out
}

// copilotToken reads the GitHub OAuth token stored by the copilot CLI.
func copilotToken() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidates := []string{
		home + "/.copilot/token.json",
		home + "/.config/github-copilot/hosts.json",
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var simple struct {
			Token string `json:"token"`
		}
		if json.Unmarshal(data, &simple) == nil && simple.Token != "" {
			return simple.Token
		}
		var hosts map[string]struct {
			OAuthToken string `json:"oauth_token"`
		}
		if json.Unmarshal(data, &hosts) == nil {
			for _, v := range hosts {
				if v.OAuthToken != "" {
					return v.OAuthToken
				}
			}
		}
	}
	return ""
}
