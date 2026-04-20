package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mwigge/milliways/internal/observability"
)

// Session stores one persisted milliways conversation.
type Session struct {
	ID        string                `json:"id"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at"`
	Model     string                `json:"model"`
	Messages  []Message             `json:"messages,omitempty"`
	Tools     []ToolCall            `json:"tools,omitempty"`
	Memory    []MemoryEntry         `json:"memory,omitempty"`
	Events    []observability.Event `json:"events,omitempty"`
	Tokens    TokenCount            `json:"tokens"`
}

// Role identifies a transcript speaker.
type Role string

const (
	// RoleUser is a user-authored message.
	RoleUser Role = "user"
	// RoleAssistant is an assistant-authored message.
	RoleAssistant Role = "assistant"
	// RoleSystem is a system-authored message.
	RoleSystem Role = "system"
)

// Message is one transcript item.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ToolCall records one tool invocation.
type ToolCall struct {
	Name string `json:"name"`
	// Args stores dynamic JSON-compatible tool arguments, so a concrete generic type cannot express all valid shapes.
	Args      map[string]any `json:"args,omitempty"`
	Result    string         `json:"result,omitempty"`
	StartedAt time.Time      `json:"started_at"`
	Duration  time.Duration  `json:"-"`
	Hooked    bool           `json:"hooked"`
}

// MemoryEntry stores one working-memory key/value pair.
type MemoryEntry struct {
	Key       string     `json:"key"`
	Value     string     `json:"value"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// TokenCount stores accumulated token totals.
type TokenCount struct {
	InputTotal  int `json:"input_total"`
	OutputTotal int `json:"output_total"`
}

// SessionSummary is lightweight metadata for listing saved sessions.
type SessionSummary struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Model     string    `json:"model"`
	Preview   string    `json:"preview"`
}

// Persister persists sessions to durable storage.
type Persister interface {
	Save(s Session) error
	Load(id string) (Session, error)
	List() ([]SessionSummary, error)
}

type toolCallJSON struct {
	Name       string         `json:"name"`
	Args       map[string]any `json:"args,omitempty"`
	Result     string         `json:"result,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	DurationMS int64          `json:"duration_ms"`
	Hooked     bool           `json:"hooked"`
}

// MarshalJSON encodes Duration as milliseconds for stable session files.
func (t ToolCall) MarshalJSON() ([]byte, error) {
	return json.Marshal(toolCallJSON{
		Name:       t.Name,
		Args:       t.Args,
		Result:     t.Result,
		StartedAt:  t.StartedAt,
		DurationMS: t.Duration.Milliseconds(),
		Hooked:     t.Hooked,
	})
}

// UnmarshalJSON decodes Duration from milliseconds.
func (t *ToolCall) UnmarshalJSON(data []byte) error {
	if t == nil {
		return fmt.Errorf("nil tool call")
	}
	var decoded toolCallJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	t.Name = decoded.Name
	t.Args = decoded.Args
	t.Result = decoded.Result
	t.StartedAt = decoded.StartedAt
	t.Duration = time.Duration(decoded.DurationMS) * time.Millisecond
	t.Hooked = decoded.Hooked
	return nil
}
