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

package conversation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mwigge/milliways/internal/editorcontext"
)

const (
	maxContinuationTranscriptTurns = 40
	maxContinuationTranscriptChars = 12000
	maxRenderedEditorContextChars  = 2000
)

// ContinueInput describes the state used to hand a conversation to another provider.
type ContinueInput struct {
	Conversation  *Conversation
	NextProvider  string
	Reason        string
	EditorContext *editorcontext.Bundle
}

// RenderEditorContext returns a condensed text representation of an editor Bundle,
// capped at approximately 500 tokens with the highest-signal entries first.
func RenderEditorContext(b *editorcontext.Bundle) string {
	if b == nil {
		return ""
	}

	lines := []string{"Editor Context:"}
	signals := b.Signals()
	if signals.LSPErrors > 0 || signals.LSPWarnings > 0 {
		lines = append(lines, fmt.Sprintf("  LSP: %d error(s), %d warning(s)", signals.LSPErrors, signals.LSPWarnings))
	}

	for _, name := range orderedCollectorNames(b.Collectors) {
		collector := b.Collectors[name]
		if collector == nil {
			continue
		}

		if collector.Buffer != nil {
			modified := ""
			if collector.Buffer.Modified {
				modified = " modified=true"
			}
			lines = append(lines, fmt.Sprintf("  %s: %s [%s]%s", name, collector.Buffer.Path, collector.Buffer.Filetype, modified))
		}

		if collector.Git != nil {
			dirty := ""
			if collector.Git.Dirty {
				dirty = " (dirty)"
			}
			filesChanged := ""
			if collector.Git.FilesChanged > 0 {
				filesChanged = fmt.Sprintf(" %d files changed", collector.Git.FilesChanged)
			}
			lines = append(lines, fmt.Sprintf("  Git: %s%s%s", collector.Git.Branch, dirty, filesChanged))
		}

		if collector.Cursor != nil {
			scope := ""
			if collector.Cursor.Scope != "" {
				scope = " in " + collector.Cursor.Scope
			}
			lines = append(lines, fmt.Sprintf("  Cursor: line %d, col %d%s", collector.Cursor.Line, collector.Cursor.Column, scope))
		}

		if collector.Selection != nil {
			selectionLines := collector.Selection.EndLine - collector.Selection.StartLine + 1
			if selectionLines < 1 {
				selectionLines = 1
			}
			lines = append(lines, fmt.Sprintf("  Selection: %d lines", selectionLines))
		}
	}

	result := strings.Join(lines, "\n")
	if len(result) <= maxRenderedEditorContextChars {
		return result
	}

	suffix := "\n  [...truncated...]"
	if maxRenderedEditorContextChars <= len(suffix) {
		return suffix[:maxRenderedEditorContextChars]
	}

	return result[:maxRenderedEditorContextChars-len(suffix)] + suffix
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

	if in.EditorContext != nil {
		editorText := RenderEditorContext(in.EditorContext)
		if editorText != "" {
			b.WriteString("Editor context:\n")
			b.WriteString(editorText)
			b.WriteString("\n\n")
		}
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

func orderedCollectorNames(collectors map[string]*editorcontext.Collector) []string {
	keys := make([]string, 0, len(collectors))
	for key := range collectors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
