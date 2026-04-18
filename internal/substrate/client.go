// Package substrate provides a typed MCP client wrapper for MemPalace conversation
// operations. It is a pure translation layer: it maps typed Go requests/responses to
// MemPalace MCP tool calls and does not contain business logic.
package substrate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/pantry"
)

// Caller is the interface satisfied by pantry.MCPClient (and by test fakes).
type Caller interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error)
}

// Client translates typed conversation operations into MemPalace MCP calls.
type Client struct {
	mcp Caller
	// wing is the MemPalace wing used for conversation storage.
	wing string
}

// New creates a Client that dials an MCP server via stdio.
func New(command string, args ...string) (*Client, error) {
	mcp, err := pantry.StartMCP(command, args...)
	if err != nil {
		return nil, fmt.Errorf("substrate: starting MCP: %w", err)
	}
	return &Client{mcp: mcp, wing: "milliways"}, nil
}

// NewWithCaller creates a Client backed by an existing Caller (useful in tests).
func NewWithCaller(caller Caller, wing string) *Client {
	return &Client{mcp: caller, wing: wing}
}

// --- Conversation ---

// ConversationRecord is the MemPalace serialised form of a canonical Conversation.
type ConversationRecord struct {
	ConversationID string                                `json:"conversation_id"`
	BlockID        string                                `json:"block_id"`
	Prompt         string                                `json:"prompt"`
	Status         string                                `json:"status"`
	CreatedAt      time.Time                             `json:"created_at"`
	UpdatedAt      time.Time                             `json:"updated_at"`
	Transcript     []conversation.Turn                   `json:"transcript"`
	Memory         conversation.MemoryState              `json:"memory"`
	Context        conversation.ContextBundle            `json:"context"`
	Segments       []conversation.ProviderSegment        `json:"segments"`
	Checkpoints    []conversation.ConversationCheckpoint `json:"checkpoints,omitempty"`
}

// SaveConversation persists (or updates) a Conversation into MemPalace.
func (c *Client) SaveConversation(ctx context.Context, conv *conversation.Conversation) error {
	rec := ConversationRecord{
		ConversationID: conv.ID,
		BlockID:        conv.BlockID,
		Prompt:         conv.Prompt,
		Status:         string(conv.Status),
		CreatedAt:      conv.CreatedAt,
		UpdatedAt:      conv.UpdatedAt,
		Transcript:     conv.Transcript,
		Memory:         conv.Memory,
		Context:        conv.Context,
		Segments:       conv.Segments,
		Checkpoints:    conv.Checkpoints,
	}
	content, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("substrate: marshal conversation: %w", err)
	}
	args := map[string]any{
		"wing":     c.wing,
		"room":     "conversations",
		"content":  string(content),
		"added_by": "milliways",
	}
	_, err = c.mcp.CallTool(ctx, "mempalace_add_drawer", args)
	if err != nil {
		return fmt.Errorf("substrate: save conversation %s: %w", conv.ID, err)
	}
	return nil
}

// GetConversation retrieves the latest persisted state for a conversation by ID.
func (c *Client) GetConversation(ctx context.Context, conversationID string) (*ConversationRecord, error) {
	args := map[string]any{
		"query": "conversation_id:" + conversationID,
		"wing":  c.wing,
		"room":  "conversations",
		"limit": 1,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_search", args)
	if err != nil {
		return nil, fmt.Errorf("substrate: get conversation %s: %w", conversationID, err)
	}
	drawers, err := parseContent[[]drawerResult](raw)
	if err != nil {
		return nil, fmt.Errorf("substrate: parse drawers for %s: %w", conversationID, err)
	}
	for i := range drawers {
		var rec ConversationRecord
		if err := json.Unmarshal([]byte(drawers[i].Content), &rec); err != nil {
			continue
		}
		if rec.ConversationID == conversationID {
			return &rec, nil
		}
	}
	return nil, fmt.Errorf("substrate: conversation %s not found", conversationID)
}

// ListConversations returns conversation IDs stored under the given wing.
func (c *Client) ListConversations(ctx context.Context, limit int) ([]string, error) {
	args := map[string]any{
		"wing":  c.wing,
		"room":  "conversations",
		"query": "conversation_id",
		"limit": limit,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_search", args)
	if err != nil {
		return nil, fmt.Errorf("substrate: list conversations: %w", err)
	}
	drawers, err := parseContent[[]drawerResult](raw)
	if err != nil {
		return nil, fmt.Errorf("substrate: parse drawer list: %w", err)
	}
	ids := make([]string, 0, len(drawers))
	for _, d := range drawers {
		var rec ConversationRecord
		if err := json.Unmarshal([]byte(d.Content), &rec); err != nil {
			continue
		}
		if rec.ConversationID != "" {
			ids = append(ids, rec.ConversationID)
		}
	}
	return ids, nil
}

// --- Working Memory ---

// GetMemory retrieves the working memory for a conversation.
func (c *Client) GetMemory(ctx context.Context, conversationID string) (conversation.MemoryState, error) {
	rec, err := c.GetConversation(ctx, conversationID)
	if err != nil {
		return conversation.MemoryState{}, err
	}
	return rec.Memory, nil
}

// SetMemory updates the working memory entry for a conversation.
func (c *Client) SetMemory(ctx context.Context, conversationID string, mem conversation.MemoryState) error {
	content, err := json.Marshal(mem)
	if err != nil {
		return fmt.Errorf("substrate: marshal memory: %w", err)
	}
	args := map[string]any{
		"wing":     c.wing,
		"room":     "working-memory",
		"content":  fmt.Sprintf(`{"conversation_id":%q,"memory":%s}`, conversationID, content),
		"added_by": "milliways",
	}
	_, err = c.mcp.CallTool(ctx, "mempalace_add_drawer", args)
	if err != nil {
		return fmt.Errorf("substrate: set memory %s: %w", conversationID, err)
	}
	return nil
}

// --- Context Bundle ---

// GetContextBundle retrieves the context bundle for a conversation.
func (c *Client) GetContextBundle(ctx context.Context, conversationID string) (conversation.ContextBundle, error) {
	rec, err := c.GetConversation(ctx, conversationID)
	if err != nil {
		return conversation.ContextBundle{}, err
	}
	return rec.Context, nil
}

// SetContextBundle persists a context bundle for a conversation.
func (c *Client) SetContextBundle(ctx context.Context, conversationID string, bundle conversation.ContextBundle) error {
	content, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("substrate: marshal context bundle: %w", err)
	}
	args := map[string]any{
		"wing":     c.wing,
		"room":     "context-bundles",
		"content":  fmt.Sprintf(`{"conversation_id":%q,"context":%s}`, conversationID, content),
		"added_by": "milliways",
	}
	_, err = c.mcp.CallTool(ctx, "mempalace_add_drawer", args)
	if err != nil {
		return fmt.Errorf("substrate: set context bundle %s: %w", conversationID, err)
	}
	return nil
}

// --- Events ---

// Event is a durable audit event appended to a conversation's event stream.
type Event struct {
	ConversationID string    `json:"conversation_id"`
	Kind           string    `json:"kind"`
	Payload        string    `json:"payload"`
	At             time.Time `json:"at"`
}

// AppendEvent records an event for a conversation.
func (c *Client) AppendEvent(ctx context.Context, ev Event) error {
	content, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("substrate: marshal event: %w", err)
	}
	args := map[string]any{
		"wing":     c.wing,
		"room":     "events",
		"content":  string(content),
		"added_by": "milliways",
	}
	_, err = c.mcp.CallTool(ctx, "mempalace_add_drawer", args)
	if err != nil {
		return fmt.Errorf("substrate: append event %s/%s: %w", ev.ConversationID, ev.Kind, err)
	}
	return nil
}

// QueryEvents retrieves events for a conversation matching an optional kind filter.
func (c *Client) QueryEvents(ctx context.Context, conversationID, kind string, limit int) ([]Event, error) {
	query := "conversation_id:" + conversationID
	if kind != "" {
		query += " kind:" + kind
	}
	args := map[string]any{
		"wing":  c.wing,
		"room":  "events",
		"query": query,
		"limit": limit,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_search", args)
	if err != nil {
		return nil, fmt.Errorf("substrate: query events: %w", err)
	}
	drawers, err := parseContent[[]drawerResult](raw)
	if err != nil {
		return nil, fmt.Errorf("substrate: parse events: %w", err)
	}
	events := make([]Event, 0, len(drawers))
	for _, d := range drawers {
		var ev Event
		if err := json.Unmarshal([]byte(d.Content), &ev); err != nil {
			continue
		}
		if ev.ConversationID == conversationID {
			events = append(events, ev)
		}
	}
	return events, nil
}

// --- Checkpoint / Resume ---

// SaveCheckpoint persists a conversation checkpoint.
func (c *Client) SaveCheckpoint(ctx context.Context, ckpt conversation.ConversationCheckpoint) error {
	content, err := json.Marshal(ckpt)
	if err != nil {
		return fmt.Errorf("substrate: marshal checkpoint: %w", err)
	}
	args := map[string]any{
		"wing":     c.wing,
		"room":     "checkpoints",
		"content":  string(content),
		"added_by": "milliways",
	}
	_, err = c.mcp.CallTool(ctx, "mempalace_add_drawer", args)
	if err != nil {
		return fmt.Errorf("substrate: save checkpoint %s: %w", ckpt.ID, err)
	}
	return nil
}

// LatestCheckpoint retrieves the most recent checkpoint for a conversation.
func (c *Client) LatestCheckpoint(ctx context.Context, conversationID string) (*conversation.ConversationCheckpoint, error) {
	args := map[string]any{
		"wing":  c.wing,
		"room":  "checkpoints",
		"query": "conversation_id:" + conversationID,
		"limit": 1,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_search", args)
	if err != nil {
		return nil, fmt.Errorf("substrate: latest checkpoint %s: %w", conversationID, err)
	}
	drawers, err := parseContent[[]drawerResult](raw)
	if err != nil {
		return nil, fmt.Errorf("substrate: parse checkpoints: %w", err)
	}
	for _, d := range drawers {
		var ckpt conversation.ConversationCheckpoint
		if err := json.Unmarshal([]byte(d.Content), &ckpt); err != nil {
			continue
		}
		if ckpt.ConversationID == conversationID {
			return &ckpt, nil
		}
	}
	return nil, fmt.Errorf("substrate: no checkpoint found for %s", conversationID)
}

// --- Lineage ---

// LineageEdge records a directed lineage relationship between conversations.
type LineageEdge struct {
	FromID    string    `json:"from_id"`
	ToID      string    `json:"to_id"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// AppendLineage records a lineage edge between two conversations.
func (c *Client) AppendLineage(ctx context.Context, edge LineageEdge) error {
	content, err := json.Marshal(edge)
	if err != nil {
		return fmt.Errorf("substrate: marshal lineage: %w", err)
	}
	args := map[string]any{
		"wing":     c.wing,
		"room":     "lineage",
		"content":  string(content),
		"added_by": "milliways",
	}
	_, err = c.mcp.CallTool(ctx, "mempalace_add_drawer", args)
	if err != nil {
		return fmt.Errorf("substrate: append lineage %s->%s: %w", edge.FromID, edge.ToID, err)
	}
	return nil
}

// --- internal helpers ---

// drawerResult is the MemPalace search result shape used internally.
type drawerResult struct {
	ID      string  `json:"id"`
	Content string  `json:"text"` // MemPalace returns drawer body in "text"
	Wing    string  `json:"wing"`
	Room    string  `json:"room"`
	Score   float64 `json:"score"`
}

// parseContent extracts typed content from an MCP tool result using the same
// dual-parse strategy as pantry.parseToolContent.
func parseContent[T any](raw json.RawMessage) (T, error) {
	var zero T

	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Content) > 0 {
		for _, item := range wrapper.Content {
			if item.Type == "text" {
				var result T
				if err := json.Unmarshal([]byte(item.Text), &result); err == nil {
					return result, nil
				}
			}
		}
	}

	if err := json.Unmarshal(raw, &zero); err != nil {
		return zero, fmt.Errorf("parsing MCP response: %w", err)
	}
	return zero, nil
}
