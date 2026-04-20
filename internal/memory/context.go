package memory

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mwigge/milliways/internal/mempalace"
	"github.com/mwigge/milliways/internal/session"
)

const (
	compactConversationThreshold = 20
	recentConversationTurns      = 5
)

// BuildSystemPrompt formats working memory, durable context, and recent conversation.
func BuildSystemPrompt(hits []mempalace.SearchResult, memory []MemoryEntry, sess *session.Session) string {
	var builder strings.Builder
	builder.WriteString("[Session Context]\n")
	builder.WriteString("memory:\n")
	for _, entry := range sortedMemory(memory) {
		builder.WriteString(fmt.Sprintf("  %s: %s\n", entry.Key, entry.Value))
	}
	builder.WriteString("\n")
	if len(hits) > 0 {
		builder.WriteString("MemPalace hits:\n")
		for _, hit := range hits {
			builder.WriteString(fmt.Sprintf("- %s/%s (relevance %.2f): %s\n", hit.Wing, hit.Room, hit.Relevance, strings.TrimSpace(hit.Content)))
			if strings.TrimSpace(hit.FactSummary) != "" {
				builder.WriteString(fmt.Sprintf("  summary: %s\n", strings.TrimSpace(hit.FactSummary)))
			}
		}
		builder.WriteString("\n")
	}
	builder.WriteString(buildConversationSection(sess))
	return strings.TrimSpace(builder.String())
}

func sortedMemory(memory []MemoryEntry) []MemoryEntry {
	cloned := append([]MemoryEntry(nil), memory...)
	sort.Slice(cloned, func(i, j int) bool {
		return cloned[i].Key < cloned[j].Key
	})
	return cloned
}

func buildConversationSection(sess *session.Session) string {
	if sess == nil || len(sess.Messages) == 0 {
		return "Conversation summary:\n- no prior conversation\n"
	}
	messages := sess.Messages
	heading := "Conversation summary:\n"
	if len(messages) > compactConversationThreshold {
		heading = fmt.Sprintf("Conversation summary (recent %d of %d turns):\n", recentConversationTurns, len(messages))
		messages = messages[len(messages)-recentConversationTurns:]
	}
	var builder strings.Builder
	builder.WriteString(heading)
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("- %s: %s\n", message.Role, content))
	}
	return builder.String()
}
