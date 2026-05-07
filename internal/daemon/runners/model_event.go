package runners

import (
	"encoding/json"
	"os"
	"strings"
)

func pushModel(stream Pusher, agentID string) {
	if stream == nil {
		return
	}
	model, source := ModelHint(agentID)
	if model == "" {
		return
	}
	stream.Push(modelEvent(model, source))
}

func pushObservedModel(stream Pusher, model string) {
	model = strings.TrimSpace(model)
	if stream == nil || model == "" {
		return
	}
	stream.Push(modelEvent(model, "observed"))
}

func modelEvent(model, source string) map[string]any {
	ev := map[string]any{"t": "model", "model": strings.TrimSpace(model)}
	if source = strings.TrimSpace(source); source != "" {
		ev["source"] = source
	}
	return ev
}

func ModelHint(agentID string) (string, string) {
	switch agentID {
	case AgentIDClaude:
		if m := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")); m != "" {
			return m, "configured"
		}
		return "claude CLI default", "cli-default"
	case AgentIDCodex:
		if m := strings.TrimSpace(os.Getenv("CODEX_MODEL")); m != "" {
			return m, "configured"
		}
		if m := strings.TrimSpace(os.Getenv("OPENAI_MODEL")); m != "" {
			return m, "configured"
		}
		return "codex CLI default", "cli-default"
	case AgentIDCopilot:
		if m := strings.TrimSpace(os.Getenv("COPILOT_MODEL")); m != "" {
			return m, "configured"
		}
		return "copilot CLI default", "cli-default"
	case AgentIDGemini:
		if m := strings.TrimSpace(os.Getenv("GEMINI_MODEL")); m != "" {
			return m, "configured"
		}
		if m := strings.TrimSpace(os.Getenv("GOOGLE_MODEL")); m != "" {
			return m, "configured"
		}
		return "gemini CLI default", "cli-default"
	case AgentIDPool:
		return "Poolside ACP", "cli-default"
	case AgentIDMiniMax, AgentIDLocal:
		return currentModel(agentID), "configured"
	default:
		return "", ""
	}
}

func extractModelFromJSONLine(line string) string {
	var payload any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return ""
	}
	return firstModelField(payload)
}

func firstModelField(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, key := range []string{"model", "model_name", "modelName", "model_id", "modelId"} {
			if raw, ok := x[key]; ok {
				if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		for _, raw := range x {
			if s := firstModelField(raw); s != "" {
				return s
			}
		}
	case []any:
		for _, raw := range x {
			if s := firstModelField(raw); s != "" {
				return s
			}
		}
	}
	return ""
}
