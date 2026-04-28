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
	"strings"
	"time"
)

// ConversationTurn is one history entry in the terminal ring buffer.
type ConversationTurn struct {
	Role   string // "user" | "assistant"
	Text   string
	Runner string // which runner was active
	At     time.Time
}

// DispatchRequest is the unified input to Runner.Execute.
type DispatchRequest struct {
	Prompt      string
	History     []ConversationTurn // oldest-first; already capped to MaxHistoryTurns
	Rules       string             // global rules content from AGENTS.md/CLAUDE.md; "" if not found
	ClientID    string             // e.g. "repl/claude"
	Context     []ContextFragment  // injected context, prepended by runners
	Attachments []Attachment       // images queued via /image command
}

// MaxHistoryTurns is the maximum number of turns kept in the ring buffer.
const MaxHistoryTurns = 20

// buildTextPrompt composes a plain-text prompt for runners that cannot handle
// structured message arrays (codex, copilot).
func buildTextPrompt(req DispatchRequest) string {
	var b strings.Builder

	// Prepend injected context fragments as labelled sections.
	for _, f := range req.Context {
		b.WriteString("## " + f.Label + "\n\n")
		b.WriteString(f.Content + "\n\n")
	}

	if req.Rules != "" {
		b.WriteString("[RULES]\n")
		b.WriteString(req.Rules)
		b.WriteString("\n\n")
	}
	if len(req.History) > 0 {
		b.WriteString("[CONVERSATION]\n")
		for _, t := range req.History {
			b.WriteString(strings.ToUpper(t.Role))
			b.WriteString(": ")
			b.WriteString(t.Text)
			b.WriteString("\n")
		}
		b.WriteString("\n[TASK]\n")
	}
	b.WriteString(req.Prompt)
	return b.String()
}
