package memory

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/session"
)

const defaultCompactThreshold = 0.7

// Provider summarizes a transcript using a model-backed implementation.
type Provider interface {
	Summarize(ctx context.Context, transcript []session.Message) (string, error)
}

// ShouldCompact reports whether token usage exceeds the configured threshold.
func ShouldCompact(sess *session.Session, contextWindow int) bool {
	if sess == nil || contextWindow <= 0 {
		return false
	}
	total := sess.Tokens.InputTotal + sess.Tokens.OutputTotal
	return float64(total) > defaultCompactThreshold*float64(contextWindow)
}

// Compact summarizes a session and replaces the transcript with a compact form.
func Compact(sess *session.Session, provider Provider) (*session.Session, error) {
	if sess == nil {
		return nil, errors.New("nil session")
	}
	if provider == nil {
		return nil, errors.New("nil provider")
	}
	_, span := observability.StartSessionCompactSpan(context.Background(), sess.ID)
	defer span.End()

	summary, err := provider.Summarize(context.Background(), sess.Messages)
	if err != nil {
		return nil, err
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil, errors.New("empty compact summary")
	}

	compacted := *sess
	compacted.Messages = compactedMessages(sess.Messages, summary)
	compacted.Memory = append(append([]session.MemoryEntry(nil), sess.Memory...), session.MemoryEntry{
		Key:   "compact_summary",
		Value: summary,
	})
	compacted.Events = append(append([]observability.Event(nil), sess.Events...), observability.Event{
		ConversationID: sess.ID,
		Kind:           observability.EventKindSessionCompact,
		Provider:       "memory",
		Text:           "session compacted",
		At:             time.Now().UTC(),
	})
	compacted.UpdatedAt = time.Now().UTC()
	return &compacted, nil
}

func compactedMessages(messages []session.Message, summary string) []session.Message {
	lastUser, ok := lastUserMessage(messages)
	if !ok {
		return []session.Message{{Role: session.RoleAssistant, Content: summary}}
	}
	return []session.Message{
		lastUser,
		{Role: session.RoleAssistant, Content: summary},
	}
}

func lastUserMessage(messages []session.Message) (session.Message, bool) {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == session.RoleUser {
			return messages[index], true
		}
	}
	return session.Message{}, false
}
