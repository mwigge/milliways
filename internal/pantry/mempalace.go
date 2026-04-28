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

// AddDrawerRequest is the payload for promoting durable memory into MemPalace.
type AddDrawerRequest struct {
	Wing       string `json:"wing"`
	Room       string `json:"room"`
	Content    string `json:"content"`
	AddedBy    string `json:"added_by,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
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

// KGInvalidateRequest invalidates a temporal fact in MemPalace.
type KGInvalidateRequest struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
	Ended     string `json:"ended,omitempty"`
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

// KGInvalidate invalidates a temporal fact in MemPalace.
func (c *MemPalaceClient) KGInvalidate(ctx context.Context, req KGInvalidateRequest) error {
	args := map[string]any{
		"subject":   req.Subject,
		"predicate": req.Predicate,
		"object":    req.Object,
	}
	if req.Ended != "" {
		args["ended"] = req.Ended
	}
	_, err := c.mcp.CallTool(ctx, "mempalace_kg_invalidate", args)
	if err != nil {
		return fmt.Errorf("mempalace_kg_invalidate: %w", err)
	}
	return nil
}

// AddDrawer stores verbatim content in MemPalace.
func (c *MemPalaceClient) AddDrawer(ctx context.Context, req AddDrawerRequest) error {
	args := map[string]any{
		"wing":    req.Wing,
		"room":    req.Room,
		"content": req.Content,
	}
	if req.AddedBy != "" {
		args["added_by"] = req.AddedBy
	}
	if req.SourceFile != "" {
		args["source_file"] = req.SourceFile
	}
	_, err := c.mcp.CallTool(ctx, "mempalace_add_drawer", args)
	if err != nil {
		return fmt.Errorf("mempalace_add_drawer: %w", err)
	}
	return nil
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
