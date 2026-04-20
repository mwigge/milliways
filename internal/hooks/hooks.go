package hooks

import "context"

// Event identifies a hook lifecycle event.
type Event string

const (
	// EventPreToolUse runs before a tool executes.
	EventPreToolUse Event = "PreToolUse"
	// EventPostToolUse runs after a tool executes.
	EventPostToolUse Event = "PostToolUse"
	// EventStop runs when a session is stopping.
	EventStop Event = "Stop"
	// EventSessionStart runs when a session starts.
	EventSessionStart Event = "SessionStart"
	// EventUserPromptSubmit runs when the user submits a prompt.
	EventUserPromptSubmit Event = "UserPromptSubmit"
)

// HookPayload is passed to external hooks as JSON.
type HookPayload struct {
	Event     Event          `json:"event"`
	SessionID string         `json:"session_id,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
	Result    string         `json:"result,omitempty"`
}

// HookResult is returned by a hook.
type HookResult struct {
	Blocked         bool        `json:"blocked"`
	Message         string      `json:"message,omitempty"`
	Modified        bool        `json:"modified"`
	ModifiedPayload HookPayload `json:"modified_payload"`
}

// HookRunner executes registered hooks for a given event.
type HookRunner interface {
	RunHooks(ctx context.Context, event Event, payload HookPayload) HookResult
}
