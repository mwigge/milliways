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

// modelCache caches the result of a provider model-list fetch so /model
// does not make a live API call on every invocation.
type modelCache struct {
	mu        sync.RWMutex
	entries   map[string]modelCacheEntry
	ttl       time.Duration
	client    *http.Client
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
// Returns nil when no API key is configured or the fetch permanently fails.
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
	// Re-check inside write lock to prevent concurrent callers from
	// both starting a fetch (TOCTOU between the RLock check above and here).
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

	c.mu.Lock()
	c.entries[agentID] = modelCacheEntry{models: models, fetchedAt: time.Now()}
	c.mu.Unlock()
	return models
}

// RefreshAsync starts a background goroutine to refresh the model cache for
// all runners. Called once at chat startup so /model shows live data quickly.
func (c *modelCache) RefreshAsync() {
	for _, agentID := range []string{"claude", "codex", "copilot", "gemini", "minimax"} {
		go func(id string) { c.Models(id) }(agentID)
	}
}

func (c *modelCache) fetch(agentID string) []string {
	switch agentID {
	case "codex":
		return c.fetchOpenAI(os.Getenv("OPENAI_API_KEY"), "gpt", "o1", "o3", "o4")
	case "claude":
		return c.fetchAnthropic(os.Getenv("ANTHROPIC_API_KEY"))
	case "gemini":
		return c.fetchGemini(os.Getenv("GEMINI_API_KEY"), os.Getenv("GOOGLE_API_KEY"))
	case "minimax":
		return c.fetchMiniMax(os.Getenv("MINIMAX_API_KEY"))
	case "copilot":
		return c.fetchCopilot()
	}
	return nil
}

// fetchOpenAI queries the OpenAI /v1/models endpoint and returns IDs that
// start with any of the given prefixes (filters noise like embedding models).
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

// fetchAnthropic queries the Anthropic /v1/models endpoint.
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

// fetchGemini queries the Google Generative Language API for available models.
func (c *modelCache) fetchGemini(apiKey, fallbackKey string) []string {
	key := apiKey
	if key == "" {
		key = fallbackKey
	}
	if key == "" {
		return nil
	}
	// Use X-Goog-Api-Key header rather than a query parameter so the key
	// is not captured in access logs, proxy logs, or OTel URL attributes.
	req, err := http.NewRequest(http.MethodGet,
		"https://generativelanguage.googleapis.com/v1beta/models", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("X-Goog-Api-Key", key)
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
			Name string `json:"name"` // "models/gemini-2.5-pro"
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

// fetchMiniMax queries the MiniMax models endpoint.
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

// fetchCopilot queries the GitHub Copilot models endpoint using the
// copilot OAuth token stored by the copilot CLI.
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

// copilotToken reads the OAuth token stored by the copilot CLI.
// Returns "" if not found.
func copilotToken() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	// copilot CLI stores tokens in ~/.copilot/token.json or
	// ~/.config/github-copilot/hosts.json depending on version.
	candidates := []string{
		home + "/.copilot/token.json",
		home + "/.config/github-copilot/hosts.json",
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// Try simple {"token":"..."} shape first.
		var simple struct {
			Token string `json:"token"`
		}
		if json.Unmarshal(data, &simple) == nil && simple.Token != "" {
			return simple.Token
		}
		// Try hosts.json shape: {"github.com":{"oauth_token":"..."}}
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
