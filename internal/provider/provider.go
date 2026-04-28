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
	// ModelCodes identifies the Codes chat-completions backend.
	ModelCodes Model = "codes"
	// ModelGemini identifies the Gemini generateContent backend.
	ModelGemini Model = "gemini"
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
