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

package runners

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/provider"
	"github.com/mwigge/milliways/internal/tools"
)

type openAICompatibleRunnerConfig struct {
	AgentID           string
	DisplayName       string
	DefaultURL        string
	URLEnv            string
	APIKeyEnvs        []string
	ModelEnv          string
	DefaultModel      string
	TimeoutEnv        string
	ToolsEnv          string
	InputUSDPerMTok   float64
	OutputUSDPerMTok  float64
	InputPriceEnv     string
	OutputPriceEnv    string
	MissingKeyMessage string
}

var openAICompatibleConfigs = map[string]openAICompatibleRunnerConfig{
	AgentIDKimi: {
		AgentID:           AgentIDKimi,
		DisplayName:       "Kimi",
		DefaultURL:        "https://api.moonshot.ai/v1/chat/completions",
		URLEnv:            "KIMI_API_URL",
		APIKeyEnvs:        []string{"KIMI_API_KEY", "MOONSHOT_API_KEY"},
		ModelEnv:          "KIMI_MODEL",
		DefaultModel:      "kimi-k2.6",
		TimeoutEnv:        "KIMI_TIMEOUT",
		ToolsEnv:          "KIMI_TOOLS",
		InputPriceEnv:     "KIMI_INPUT_USD_PER_MTOK",
		OutputPriceEnv:    "KIMI_OUTPUT_USD_PER_MTOK",
		MissingKeyMessage: "Kimi API key not set - run /login kimi to set KIMI_API_KEY",
	},
	AgentIDDeepSeek: {
		AgentID:           AgentIDDeepSeek,
		DisplayName:       "DeepSeek",
		DefaultURL:        "https://api.deepseek.com/chat/completions",
		URLEnv:            "DEEPSEEK_API_URL",
		APIKeyEnvs:        []string{"DEEPSEEK_API_KEY"},
		ModelEnv:          "DEEPSEEK_MODEL",
		DefaultModel:      "deepseek-v4-flash",
		TimeoutEnv:        "DEEPSEEK_TIMEOUT",
		ToolsEnv:          "DEEPSEEK_TOOLS",
		InputUSDPerMTok:   0.14,
		OutputUSDPerMTok:  0.28,
		InputPriceEnv:     "DEEPSEEK_INPUT_USD_PER_MTOK",
		OutputPriceEnv:    "DEEPSEEK_OUTPUT_USD_PER_MTOK",
		MissingKeyMessage: "DeepSeek API key not set - run /login deepseek to set it",
	},
}

func RunKimi(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
	RunKimiWithSecurityWorkspace(ctx, input, stream, metrics, "")
}

func RunKimiWithSecurityWorkspace(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver, securityWorkspace string) {
	runOpenAICompatibleSession(ctx, openAICompatibleConfigs[AgentIDKimi], input, stream, metrics, securityWorkspace)
}

func RunDeepSeek(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
	RunDeepSeekWithSecurityWorkspace(ctx, input, stream, metrics, "")
}

func RunDeepSeekWithSecurityWorkspace(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver, securityWorkspace string) {
	runOpenAICompatibleSession(ctx, openAICompatibleConfigs[AgentIDDeepSeek], input, stream, metrics, securityWorkspace)
}

type openAICompatibleSessionState struct {
	messages        []Message
	pendingApproval *approvalGatePending
}

func runOpenAICompatibleSession(ctx context.Context, cfg openAICompatibleRunnerConfig, input <-chan []byte, stream Pusher, metrics MetricsObserver, securityWorkspace string) {
	state := &openAICompatibleSessionState{}
	for prompt := range input {
		if stream == nil {
			continue
		}
		runOpenAICompatibleOnce(ctx, cfg, prompt, stream, metrics, state, securityWorkspace)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

func runOpenAICompatibleOnce(parent context.Context, cfg openAICompatibleRunnerConfig, prompt []byte, stream Pusher, metrics MetricsObserver, state *openAICompatibleSessionState, securityWorkspace string) {
	apiKey := openAICompatibleAPIKey(cfg)
	if apiKey == "" {
		observeError(metrics, cfg.AgentID)
		stream.Push(map[string]any{
			"t":    "err",
			"code": -32005,
			"msg":  cfg.MissingKeyMessage,
		})
		stream.Push(zeroUsageChunkEnd())
		return
	}

	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		stream.Push(zeroUsageChunkEnd())
		return
	}
	if state == nil {
		state = &openAICompatibleSessionState{}
	}
	if state.pendingApproval != nil {
		if approvalGateExpired(state.pendingApproval.Request, time.Now()) {
			state.pendingApproval = nil
			approvalGateExpiredInput(stream)
			return
		}
		approved, rejected := approvalGateDecision(text)
		switch {
		case approved:
			text = approvalGateImplementPrompt(state.pendingApproval.OriginalPrompt, state.pendingApproval.Plan)
			state.pendingApproval = nil
		case rejected:
			state.pendingApproval = nil
			approvalGateCancelled(stream)
			return
		default:
			original := state.pendingApproval.OriginalPrompt
			text = approvalGatePlanPrompt(original + "\n\nUser feedback:\n" + text)
			state.pendingApproval = &approvalGatePending{
				OriginalPrompt: original,
				Request:        approvalGateNewRequest(cfg.AgentID, securityWorkspace, original, time.Now()),
			}
		}
	} else if approvalGateNeedsPlan(text) {
		state.pendingApproval = &approvalGatePending{
			OriginalPrompt: text,
			Request:        approvalGateNewRequest(cfg.AgentID, securityWorkspace, text, time.Now()),
		}
		text = approvalGatePlanPrompt(text)
	}

	url := strings.TrimSpace(os.Getenv(cfg.URLEnv))
	if url == "" {
		url = cfg.DefaultURL
	}
	model := strings.TrimSpace(os.Getenv(cfg.ModelEnv))
	if model == "" {
		model = cfg.DefaultModel
	}
	stream.Push(modelEvent(model, "configured"))
	timeout := runnerRequestTimeout(cfg.TimeoutEnv)

	spanCtx, span := startDispatchSpan(parent, cfg.AgentID, model)
	ctx := spanCtx
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(spanCtx, timeout)
		defer cancel()
	}

	planningOnly := state.pendingApproval != nil && state.pendingApproval.Plan == ""
	registry := openAICompatibleRegistry(cfg)
	if planningOnly {
		registry = nil
	}
	if len(state.messages) == 0 {
		state.messages = []Message{{Role: RoleSystem, Content: minimaxSystemPrompt}}
	}
	messages := append([]Message(nil), state.messages...)
	messages = append(messages, Message{Role: RoleUser, Content: text})
	client := &openAICompatibleClient{
		http:    &http.Client{Timeout: timeout},
		url:     url,
		apiKey:  apiKey,
		model:   model,
		stream:  stream,
		agentID: cfg.AgentID,
	}

	result, err := RunAgenticLoop(ctx, client, registry, &messages, LoopOptions{
		SessionID:              cfg.AgentID,
		Logger:                 slog.Default(),
		StopOnUserInputRequest: true,
		CommandFirewall:        commandFirewallForAgentWorkspace(cfg.AgentID, securityWorkspace),
	})
	if err != nil {
		observeError(metrics, cfg.AgentID)
		endDispatchSpan(span, 0, 0, 0, err.Error())
		stream.Push(classifyDispatchError(cfg.AgentID, err))
		stream.Push(zeroUsageChunkEnd())
		return
	}
	state.messages = messages

	usage := &openaiStreamUsage{
		PromptTokens:     result.TotalUsage.PromptTokens,
		CompletionTokens: result.TotalUsage.CompletionTokens,
		TotalTokens:      result.TotalUsage.TotalTokens,
	}
	cost, costKnown := openAICompatibleCostUSD(cfg, usage)
	if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		observeTokens(metrics, cfg.AgentID, usage.PromptTokens, usage.CompletionTokens, cost)
	}
	endDispatchSpan(span, usage.PromptTokens, usage.CompletionTokens, cost, "")
	push := map[string]any{
		"t":             "chunk_end",
		"input_tokens":  usage.PromptTokens,
		"output_tokens": usage.CompletionTokens,
		"total_tokens":  usage.TotalTokens,
	}
	if costKnown {
		push["cost_usd"] = cost
		push["cost_known"] = true
	} else {
		push["cost_known"] = false
	}
	if result.StoppedAt == StopReasonMaxTurns {
		push["max_turns_hit"] = true
	}
	if result.StoppedAt == StopReasonNeedsInput {
		push["needs_input"] = true
	}
	if planningOnly {
		if state.pendingApproval != nil {
			state.pendingApproval.Plan = strings.TrimSpace(result.FinalContent)
		}
		approvalGateNeedsInput(stream, push, state.pendingApproval.Request)
		return
	}
	stream.Push(push)
}

func openAICompatibleAPIKey(cfg openAICompatibleRunnerConfig) string {
	for _, key := range cfg.APIKeyEnvs {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func openAICompatibleRegistry(cfg openAICompatibleRunnerConfig) *tools.Registry {
	if toolsDisabledByEnv(os.Getenv(cfg.ToolsEnv)) {
		return nil
	}
	return tools.NewBuiltInRegistry()
}

type openAICompatibleClient struct {
	http    *http.Client
	url     string
	apiKey  string
	model   string
	stream  Pusher
	agentID string
}

func (c *openAICompatibleClient) Send(ctx context.Context, messages []Message, toolDefs []provider.ToolDef) (TurnResult, error) {
	payload := buildOpenAIChatPayload(c.model, messages, toolDefs)
	payload["stream_options"] = map[string]any{"include_usage": true}
	body, err := json.Marshal(payload)
	if err != nil {
		return TurnResult{}, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return TurnResult{}, fmt.Errorf("request %s: %s", sanitizeProviderURL(c.url), scrubProviderSecrets(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return TurnResult{}, fmt.Errorf("connect %s: %s", sanitizeProviderURL(c.url), scrubProviderSecrets(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		msg := sanitizeProviderErrorBody(errBody)
		if resp.StatusCode == http.StatusTooManyRequests || minimaxBodyLooksQuota(msg) {
			return TurnResult{}, fmt.Errorf("%w: API %d: %s", ErrMiniMaxQuota, resp.StatusCode, msg)
		}
		return TurnResult{}, fmt.Errorf("API %d: %s", resp.StatusCode, msg)
	}

	return streamOpenAITurn(ctx, resp.Body, c.stream)
}

func openAICompatibleCostUSD(cfg openAICompatibleRunnerConfig, u *openaiStreamUsage) (float64, bool) {
	if u == nil {
		return 0, false
	}
	inRate, inKnown := envFloatDefaultKnown(cfg.InputPriceEnv, cfg.InputUSDPerMTok)
	outRate, outKnown := envFloatDefaultKnown(cfg.OutputPriceEnv, cfg.OutputUSDPerMTok)
	if !inKnown || !outKnown {
		return 0, false
	}
	in := float64(u.PromptTokens) * inRate / 1_000_000
	out := float64(u.CompletionTokens) * outRate / 1_000_000
	return in + out, true
}

func envFloatDefault(key string, def float64) float64 {
	value, _ := envFloatDefaultKnown(key, def)
	return value
}

func envFloatDefaultKnown(key string, def float64) (float64, bool) {
	if key == "" {
		return def, def > 0
	}
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, def > 0
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return def, def > 0
	}
	return parsed, true
}
