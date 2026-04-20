package provider

import (
	"context"

	"github.com/mwigge/milliways/internal/session"
)

// Model identifies a supported LLM provider backend.
type Model string

const (
	// ModelMiniMax identifies the MiniMax chat-completions backend.
	ModelMiniMax Model = "minimax"
)

// Request describes one provider request.
type Request struct {
	Model        Model
	Messages     []session.Message
	Tools        []ToolDef
	SystemPrompt string
}

// Response contains the provider result.
type Response struct {
	Content  string
	ToolCall *ToolCall
	Tokens   TokenCount
}

// TokenCount describes prompt and completion token usage.
type TokenCount struct {
	Input  int
	Output int
}

// ToolDef describes one callable tool.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ToolCall describes one model-requested tool invocation.
type ToolCall struct {
	Name string
	Args map[string]any
}

// Provider sends requests to a language model backend.
type Provider interface {
	Send(ctx context.Context, req Request) (Response, error)
	SupportsModel(m Model) bool
}
