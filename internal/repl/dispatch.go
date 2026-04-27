package repl

import (
	"strings"
	"time"
)

// ConversationTurn is one history entry in the REPL ring buffer.
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
	Rules       string             // content of ai_local/CLAUDE.md; "" if not found
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
