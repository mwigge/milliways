package conversation

import (
	"fmt"
	"strings"
)

const (
	maxContinuationTranscriptTurns = 40
	maxContinuationTranscriptChars = 12000
)

// ContinueInput describes the state used to hand a conversation to another provider.
type ContinueInput struct {
	Conversation *Conversation
	NextProvider string
	Reason       string
}

// BuildContinuationPrompt reconstructs provider-visible context from canonical state.
func BuildContinuationPrompt(in ContinueInput) string {
	if in.Conversation == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("Continue an in-progress Milliways conversation.\n\n")
	b.WriteString("Original goal:\n")
	b.WriteString(in.Conversation.Prompt)
	b.WriteString("\n\n")

	if in.Reason != "" {
		b.WriteString("Why you are taking over:\n")
		b.WriteString(in.Reason)
		b.WriteString("\n\n")
	}

	if in.Conversation.Memory.WorkingSummary != "" {
		b.WriteString("Current working memory:\n")
		b.WriteString(in.Conversation.Memory.WorkingSummary)
		b.WriteString("\n\n")
	}

	if len(in.Conversation.Memory.ActiveGoals) > 0 {
		b.WriteString("Active goals:\n")
		for _, goal := range in.Conversation.Memory.ActiveGoals {
			b.WriteString("- ")
			b.WriteString(goal)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if in.Conversation.Memory.NextAction != "" {
		b.WriteString("Current next action:\n")
		b.WriteString(in.Conversation.Memory.NextAction)
		b.WriteString("\n\n")
	}

	if len(in.Conversation.Context.SpecRefs) > 0 {
		b.WriteString("Relevant specs:\n")
		for _, ref := range in.Conversation.Context.SpecRefs {
			b.WriteString("- ")
			b.WriteString(ref)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if in.Conversation.Context.CodeGraphText != "" {
		b.WriteString("Relevant repository context:\n")
		b.WriteString(in.Conversation.Context.CodeGraphText)
		b.WriteString("\n\n")
	}

	if in.Conversation.Context.MemPalaceText != "" {
		b.WriteString("Relevant persistent memory:\n")
		b.WriteString(in.Conversation.Context.MemPalaceText)
		b.WriteString("\n\n")
	}

	transcript := boundedTranscript(in.Conversation.Transcript, maxContinuationTranscriptTurns, maxContinuationTranscriptChars)
	if transcript != "" {
		b.WriteString("Transcript so far:\n")
		b.WriteString(transcript)
		b.WriteString("\n\n")
	}

	b.WriteString(fmt.Sprintf("Continue from the current state in %s. Do not restart the task from scratch.", in.NextProvider))
	return b.String()
}

func boundedTranscript(turns []Turn, maxTurns, maxChars int) string {
	if len(turns) == 0 {
		return ""
	}
	if maxTurns <= 0 {
		maxTurns = len(turns)
	}
	start := 0
	if len(turns) > maxTurns {
		start = len(turns) - maxTurns
	}
	window := turns[start:]
	var parts []string
	omitted := start
	for _, turn := range window {
		parts = append(parts, fmt.Sprintf("[%s:%s] %s", turn.Role, turn.Provider, turn.Text))
	}
	text := strings.Join(parts, "\n")
	if maxChars > 0 && len(text) > maxChars {
		cut := len(text) - maxChars
		if cut > len(text) {
			cut = len(text)
		}
		text = text[cut:]
		if idx := strings.IndexByte(text, '\n'); idx >= 0 {
			text = text[idx+1:]
		}
		omitted++
	}
	if omitted > 0 {
		return fmt.Sprintf("[system:milliways] %d earlier transcript turn(s) omitted to keep the continuation payload bounded.\n%s", omitted, text)
	}
	return text
}
