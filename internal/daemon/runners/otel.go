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
	"context"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// genAISystem maps milliways agent IDs to the OTel gen_ai.system value
// from the OpenTelemetry Semantic Conventions for Generative AI.
var genAISystem = map[string]string{
	AgentIDClaude:  "anthropic",
	AgentIDCodex:   "openai",
	AgentIDCopilot: "github_copilot",
	AgentIDGemini:  "google",
	AgentIDPool:    "poolside",
	AgentIDMiniMax: "minimax",
	AgentIDLocal:   "local",
}

const tracerName = "github.com/mwigge/milliways/runners"

// startDispatchSpan begins an OTel span following the Gen AI semantic
// conventions (https://opentelemetry.io/docs/specs/semconv/gen-ai/).
// Returns a child context carrying the span and the span itself.
// Callers must call span.End() when the dispatch completes.
func startDispatchSpan(ctx context.Context, agentID, model string) (context.Context, trace.Span) {
	system := genAISystem[agentID]
	if system == "" {
		system = agentID
	}
	if model == "" {
		model = currentModel(agentID)
	}

	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "gen_ai.client.operation",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("gen_ai.system", system),
			attribute.String("gen_ai.operation.name", "chat"),
			attribute.String("gen_ai.request.model", model),
			attribute.String("milliways.agent_id", agentID),
		),
	)
	return ctx, span
}

// endDispatchSpan finalises the span with token counts, cost, and finish reason.
// tokIn/tokOut are 0 for CLI runners that do not surface token usage.
func endDispatchSpan(span trace.Span, tokIn, tokOut int, costUSD float64, errMsg string) {
	if span == nil {
		return
	}
	if tokIn > 0 {
		span.SetAttributes(attribute.Int("gen_ai.usage.input_tokens", tokIn))
	}
	if tokOut > 0 {
		span.SetAttributes(attribute.Int("gen_ai.usage.output_tokens", tokOut))
	}
	if costUSD > 0 {
		span.SetAttributes(attribute.Float64("gen_ai.usage.cost_usd", costUSD))
	}
	if errMsg != "" {
		span.SetStatus(codes.Error, errMsg)
		span.SetAttributes(attribute.String("gen_ai.response.finish_reasons", "error"))
	} else {
		span.SetAttributes(attribute.String("gen_ai.response.finish_reasons", "stop"))
	}
	span.End()
}

// startToolSpan begins a child span for one tool call inside the agentic loop.
func startToolSpan(ctx context.Context, toolName string) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	return tracer.Start(ctx, "gen_ai.execute_tool",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("gen_ai.tool.name", toolName),
			attribute.String("gen_ai.tool.type", "function"),
		),
	)
}

// endToolSpan closes a tool span, setting error status if errMsg is non-empty.
func endToolSpan(span trace.Span, errMsg string) {
	if span == nil {
		return
	}
	if errMsg != "" {
		span.SetStatus(codes.Error, errMsg)
	}
	span.End()
}

// currentModel returns the active model for agentID by reading the
// same env vars the runners use.
func currentModel(agentID string) string {
	switch agentID {
	case AgentIDClaude:
		if m := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")); m != "" {
			return m
		}
		return "claude-opus-4-5"
	case AgentIDCodex:
		if m := strings.TrimSpace(os.Getenv("CODEX_MODEL")); m != "" {
			return m
		}
		return "o4-mini"
	case AgentIDCopilot:
		if m := strings.TrimSpace(os.Getenv("COPILOT_MODEL")); m != "" {
			return m
		}
		return "default"
	case AgentIDGemini:
		if m := strings.TrimSpace(os.Getenv("GEMINI_MODEL")); m != "" {
			return m
		}
		return "gemini-2.5-pro"
	case AgentIDPool:
		return "poolside"
	case AgentIDMiniMax:
		if m := strings.TrimSpace(os.Getenv("MINIMAX_MODEL")); m != "" {
			return m
		}
		return minimaxDefaultModel
	case AgentIDLocal:
		if m := strings.TrimSpace(os.Getenv("MILLIWAYS_LOCAL_MODEL")); m != "" {
			return m
		}
		return localDefaultModel
	}
	return agentID
}
