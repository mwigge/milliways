package review

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// cgCallFn is the injectable transport function for CodeGraph MCP calls.
// It receives the tool name and its arguments and returns the raw JSON result.
type cgCallFn func(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error)

// MCPCodeGraphClient calls CodeGraph tools via the milliwaysd JSON-RPC socket.
type MCPCodeGraphClient struct {
	SocketPath string
	callFn     cgCallFn
}

// NewCodeGraphClient returns a CodeGraphClient that connects to the milliwaysd
// Unix socket at socketPath. When socketPath is empty the default
// ~/.local/state/milliways/sock is used.
func NewCodeGraphClient(socketPath string) CodeGraphClient {
	if socketPath == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			socketPath = filepath.Join(home, ".local/state/milliways/sock")
		}
	}
	c := &MCPCodeGraphClient{SocketPath: socketPath}
	c.callFn = c.socketCall
	return c
}

// NewCodeGraphClientWithCaller returns a CodeGraphClient that uses callFn
// instead of the real Unix socket. Used in tests.
func NewCodeGraphClientWithCaller(callFn cgCallFn) CodeGraphClient {
	return &MCPCodeGraphClient{callFn: callFn}
}

// IsIndexed returns true if CodeGraph has indexed the repo (nodes > 0).
// On any error it returns false — the caller falls back to directory order.
func (c *MCPCodeGraphClient) IsIndexed(ctx context.Context) bool {
	raw, err := c.callFn(ctx, "mcp__codegraph__codegraph_status", map[string]any{})
	if err != nil {
		return false
	}

	var status struct {
		Nodes int `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return false
	}
	return status.Nodes > 0
}

// Files calls the codegraph_files tool with the given repoPath and returns the
// parsed file list. On any error it returns nil and the error.
func (c *MCPCodeGraphClient) Files(ctx context.Context, repoPath string) ([]CodeGraphFile, error) {
	raw, err := c.callFn(ctx, "mcp__codegraph__codegraph_files", map[string]any{
		"path": repoPath,
	})
	if err != nil {
		return nil, fmt.Errorf("codegraph files %s: %w", repoPath, err)
	}

	var resp struct {
		Files []struct {
			Path        string `json:"path"`
			SymbolCount int    `json:"symbolCount"`
			Language    string `json:"language"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("codegraph files parse: %w", err)
	}

	files := make([]CodeGraphFile, len(resp.Files))
	for i, f := range resp.Files {
		files[i] = CodeGraphFile{
			Path:        f.Path,
			SymbolCount: f.SymbolCount,
			Language:    f.Language,
		}
	}
	return files, nil
}

// Impact calls the codegraph_impact tool for the given symbol and depth.
// On any error it returns 0.0 and the error.
func (c *MCPCodeGraphClient) Impact(ctx context.Context, symbol string, depth int) (float64, error) {
	raw, err := c.callFn(ctx, "mcp__codegraph__codegraph_impact", map[string]any{
		"symbol": symbol,
		"depth":  depth,
	})
	if err != nil {
		return 0.0, fmt.Errorf("codegraph impact %s: %w", symbol, err)
	}

	var resp struct {
		ImpactScore float64 `json:"impact_score"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0.0, fmt.Errorf("codegraph impact parse: %w", err)
	}
	return resp.ImpactScore, nil
}

// socketCall is the real transport. It opens the Unix socket, sends a JSON-RPC
// 2.0 request using the mcp.call method, and reads the newline-terminated
// response, returning result as raw JSON.
func (c *MCPCodeGraphClient) socketCall(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("codegraph dial %s: %w", c.SocketPath, err)
	}
	defer conn.Close() //nolint:errcheck // best-effort close on Unix socket

	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  "mcp.call",
		"params": map[string]any{
			"tool": tool,
			"args": args,
		},
		"id": 1,
	}

	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("codegraph marshal request: %w", err)
	}
	encoded = append(encoded, '\n')

	if _, err := conn.Write(encoded); err != nil {
		return nil, fmt.Errorf("codegraph write request: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("codegraph read response: %w", err)
		}
		return nil, fmt.Errorf("codegraph read response: empty")
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
		return nil, fmt.Errorf("codegraph unmarshal envelope: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("codegraph rpc error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}

	// The result field from mcp.call wraps the tool output in a content array.
	// Try to unwrap: {"content":[{"type":"text","text":"<json>"}]}
	var mcpResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(envelope.Result, &mcpResult); err == nil && len(mcpResult.Content) > 0 {
		for _, c := range mcpResult.Content {
			if c.Type == "text" && len(c.Text) > 0 {
				return json.RawMessage(c.Text), nil
			}
		}
	}

	// Fall back: result is already the tool output directly.
	// Strip any leading/trailing whitespace bytes before returning.
	return bytes.TrimSpace(envelope.Result), nil
}
