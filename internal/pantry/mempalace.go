package pantry

import (
	"context"
	"encoding/json"
	"fmt"
)

// MemPalaceClient queries the MemPalace MCP server for decisions and context.
type MemPalaceClient struct {
	mcp *MCPClient
}

// Drawer represents a MemPalace search result with semantic similarity score.
type Drawer struct {
	ID      string  `json:"id"`
	Text    string  `json:"text"`
	Wing    string  `json:"wing"`
	Room    string  `json:"room"`
	Score   float64 `json:"score"`
	FiledAt string  `json:"filed_at"`
}

// NewMemPalaceClient creates a MemPalace client backed by an MCP server.
func NewMemPalaceClient(command string, args ...string) (*MemPalaceClient, error) {
	mcp, err := StartMCP(command, args...)
	if err != nil {
		return nil, fmt.Errorf("starting MemPalace MCP: %w", err)
	}
	return &MemPalaceClient{mcp: mcp}, nil
}

// Search performs a semantic search across MemPalace drawers.
func (c *MemPalaceClient) Search(ctx context.Context, query, wing string, limit int) ([]Drawer, error) {
	args := map[string]any{
		"query": query,
		"limit": limit,
	}
	if wing != "" {
		args["wing"] = wing
	}

	result, err := c.mcp.CallTool(ctx, "mempalace_search", args)
	if err != nil {
		return nil, fmt.Errorf("mempalace_search: %w", err)
	}

	return parseToolContent[[]Drawer](result)
}

// KGTriple represents a knowledge graph triple.
type KGTriple struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to"`
}

// KGQuery queries the MemPalace temporal knowledge graph.
func (c *MemPalaceClient) KGQuery(ctx context.Context, subject, predicate string) ([]KGTriple, error) {
	args := map[string]any{}
	if subject != "" {
		args["subject"] = subject
	}
	if predicate != "" {
		args["predicate"] = predicate
	}

	result, err := c.mcp.CallTool(ctx, "mempalace_kg_query", args)
	if err != nil {
		return nil, fmt.Errorf("mempalace_kg_query: %w", err)
	}

	return parseToolContent[[]KGTriple](result)
}

// Close terminates the MCP server.
func (c *MemPalaceClient) Close() error {
	return c.mcp.Close()
}

// parseToolContent extracts typed content from an MCP tool result.
// MCP tool results wrap content in a {"content": [{"type": "text", "text": "..."}]} structure.
// The wrapper format is tried first because it is more specific; falling back to direct
// parse avoids masking actual content inside a wrapper when T is a slice or nullable type.
func parseToolContent[T any](raw json.RawMessage) (T, error) {
	var zero T

	// Try MCP content wrapper first (more specific format)
	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil {
		for _, c := range wrapper.Content {
			if c.Type == "text" {
				var result T
				if err := json.Unmarshal([]byte(c.Text), &result); err == nil {
					return result, nil
				}
			}
		}
	}

	// Fall back to direct parse (some servers return plain JSON)
	if err := json.Unmarshal(raw, &zero); err != nil {
		return zero, fmt.Errorf("parsing MCP response: %w", err)
	}
	return zero, nil
}
