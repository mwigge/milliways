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

package mempalace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/pantry"
)

var startMCP = func(command string, args ...string) (rpcCaller, error) {
	return pantry.StartMCP(command, args...)
}

type rpcCaller interface {
	// Dynamic JSON-RPC tool arguments vary by tool and cannot be expressed with a single concrete generic type.
	CallTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error)
	Close() error
}

// Client implements Palace using the MemPalace MCP server.
type Client struct {
	rpc rpcCaller
}

var _ Palace = (*Client)(nil)

// NewClientFromEnv starts a MemPalace MCP client from environment variables.
func NewClientFromEnv() (*Client, error) {
	command := strings.TrimSpace(os.Getenv("MEMPALACE_MCP_CMD"))
	if command == "" {
		return nil, errors.New("MEMPALACE_MCP_CMD is required")
	}
	args := strings.Fields(os.Getenv("MEMPALACE_MCP_ARGS"))
	return NewClient(command, args...)
}

// NewClient starts a MemPalace MCP client.
func NewClient(command string, args ...string) (*Client, error) {
	rpc, err := startMCP(command, args...)
	if err != nil {
		return nil, fmt.Errorf("start mempalace mcp: %w", err)
	}
	return &Client{rpc: rpc}, nil
}

// Search performs a semantic search.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if c == nil || c.rpc == nil {
		return nil, errors.New("nil mempalace client")
	}
	ctx, span := observability.StartMemorySearchSpan(ctx, query, limit)
	defer span.End()
	result, err := c.rpc.CallTool(ctx, "mempalace_search", map[string]any{"query": query, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("mempalace_search: %w", err)
	}
	return decodeToolResult[[]SearchResult](result)
}

// Write stores durable memory in MemPalace.
func (c *Client) Write(ctx context.Context, wing, room, drawer string, content string) error {
	if c == nil || c.rpc == nil {
		return errors.New("nil mempalace client")
	}
	ctx, span := observability.StartMemoryWriteSpan(ctx, wing, room)
	defer span.End()
	args := map[string]any{"wing": wing, "room": room, "content": content}
	if strings.TrimSpace(drawer) != "" {
		args["drawer"] = drawer
	}
	_, err := c.rpc.CallTool(ctx, "mempalace_add_drawer", args)
	if err != nil {
		return fmt.Errorf("mempalace_add_drawer: %w", err)
	}
	return nil
}

// ListWings lists available wings.
func (c *Client) ListWings(ctx context.Context) ([]string, error) {
	if c == nil || c.rpc == nil {
		return nil, errors.New("nil mempalace client")
	}
	result, err := c.rpc.CallTool(ctx, "mempalace_list_wings", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("mempalace_list_wings: %w", err)
	}
	return decodeToolResult[[]string](result)
}

// ListRooms lists rooms within a wing.
func (c *Client) ListRooms(ctx context.Context, wing string) ([]string, error) {
	if c == nil || c.rpc == nil {
		return nil, errors.New("nil mempalace client")
	}
	result, err := c.rpc.CallTool(ctx, "mempalace_list_rooms", map[string]any{"wing": wing})
	if err != nil {
		return nil, fmt.Errorf("mempalace_list_rooms: %w", err)
	}
	return decodeToolResult[[]string](result)
}

// Close stops the underlying MCP process.
func (c *Client) Close() error {
	if c == nil || c.rpc == nil {
		return nil
	}
	return c.rpc.Close()
}

func decodeToolResult[T any](raw json.RawMessage) (T, error) {
	var zero T
	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil {
		for _, content := range wrapper.Content {
			if content.Type != "text" {
				continue
			}
			var decoded T
			if err := json.Unmarshal([]byte(content.Text), &decoded); err == nil {
				return decoded, nil
			}
		}
	}
	if err := json.Unmarshal(raw, &zero); err != nil {
		return zero, fmt.Errorf("decode tool result: %w", err)
	}
	return zero, nil
}
