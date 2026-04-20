package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mwigge/milliways/internal/provider"
	"github.com/mwigge/milliways/internal/tools"
)

// Server is one configured MCP server.
type Server struct {
	Name   string
	Config ServerConfig
	Client Caller
	Tools  []RemoteTool
}

// RemoteTool describes a tool exposed by an MCP server.
type RemoteTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// NewServer validates and constructs an MCP server.
func NewServer(name string, config ServerConfig) (*Server, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("server name is required")
	}
	if config.Type != "" && config.Type != "stdio" && config.Type != "sse" {
		return nil, fmt.Errorf("unsupported server type %q", config.Type)
	}
	return &Server{Name: name, Config: config}, nil
}

// RegisterTools adds remote MCP tools into the given registry.
func (s *Server) RegisterTools(registry *tools.Registry) error {
	if s == nil {
		return errors.New("nil server")
	}
	if registry == nil {
		return errors.New("nil registry")
	}
	for _, tool := range s.Tools {
		remoteName := "mcp:" + tool.Name
		name := tool.Name
		registry.Register(remoteName, func(ctx context.Context, args map[string]any) (string, error) {
			if s.Client == nil {
				return "", errors.New("nil mcp client")
			}
			result, err := s.Client.CallTool(ctx, name, args)
			if err != nil {
				return "", err
			}
			return string(result), nil
		}, provider.ToolDef{
			Name:        remoteName,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	return nil
}

func decodeTools(raw json.RawMessage) ([]RemoteTool, error) {
	var payload struct {
		Tools []RemoteTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && len(payload.Tools) > 0 {
		return payload.Tools, nil
	}
	var tools []RemoteTool
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("decode tools: %w", err)
	}
	return tools, nil
}
