package session

import "time"

// Session stores one persisted milliways conversation.
type Session struct {
	ID        string        `json:"id"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Model     string        `json:"model"`
	Messages  []Message     `json:"messages,omitempty"`
	Tools     []ToolCall    `json:"tools,omitempty"`
	Memory    []MemoryEntry `json:"memory,omitempty"`
	Events    []Event       `json:"events,omitempty"`
	Tokens    TokenCount    `json:"tokens"`
}

// Message is one transcript item.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ToolCall records one tool invocation.
type ToolCall struct {
	Name       string                 `json:"name"`
	Args       map[string]interface{} `json:"args,omitempty"`
	Result     string                 `json:"result,omitempty"`
	StartedAt  time.Time              `json:"started_at"`
	DurationMS int                    `json:"duration_ms"`
	Hooked     bool                   `json:"hooked"`
}

// MemoryEntry stores one working-memory key/value pair.
type MemoryEntry struct {
	Key       string     `json:"key"`
	Value     string     `json:"value"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// Event records one runtime event.
type Event struct {
	ID             string            `json:"id"`
	ConversationID string            `json:"conversation_id"`
	BlockID        string            `json:"block_id"`
	SegmentID      string            `json:"segment_id"`
	Kind           string            `json:"kind"`
	Provider       string            `json:"provider"`
	Text           string            `json:"text"`
	At             time.Time         `json:"at"`
	Fields         map[string]string `json:"fields,omitempty"`
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
