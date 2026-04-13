package pantry

import (
	"encoding/json"
	"fmt"
)

// CodeGraphClient queries the CodeGraph MCP server for code structure knowledge.
type CodeGraphClient struct {
	mcp *MCPClient
}

// ContextResult holds the AI-ready context for a task from CodeGraph.
type ContextResult struct {
	Context string `json:"context"`
	Files   int    `json:"files"`
	Symbols int    `json:"symbols"`
}

// ImpactResult holds blast radius analysis from CodeGraph.
type ImpactResult struct {
	Symbol      string   `json:"symbol"`
	Callers     int      `json:"callers"`
	Callees     int      `json:"callees"`
	Files       []string `json:"files"`
	BlastRadius int      `json:"blast_radius"`
}

// SymbolInfo holds details about a code symbol.
type SymbolInfo struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Signature string `json:"signature"`
	Callers   int    `json:"callers"`
}

// NewCodeGraphClient creates a CodeGraph client backed by an MCP server.
func NewCodeGraphClient(command string, args ...string) (*CodeGraphClient, error) {
	mcp, err := StartMCP(command, args...)
	if err != nil {
		return nil, fmt.Errorf("starting CodeGraph MCP: %w", err)
	}
	return &CodeGraphClient{mcp: mcp}, nil
}

// Context builds AI-ready context for a task, including relevant symbols and files.
func (c *CodeGraphClient) Context(task string) (string, error) {
	result, err := c.mcp.CallTool("codegraph_context", map[string]any{
		"task": task,
	})
	if err != nil {
		return "", fmt.Errorf("codegraph_context: %w", err)
	}

	// Context returns a text blob, extract it
	text, err := extractText(result)
	if err != nil {
		return "", err
	}
	return text, nil
}

// Impact returns blast radius analysis for a symbol.
func (c *CodeGraphClient) Impact(symbol string, depth int) (*ImpactResult, error) {
	args := map[string]any{"symbol": symbol}
	if depth > 0 {
		args["depth"] = depth
	}

	result, err := c.mcp.CallTool("codegraph_impact", args)
	if err != nil {
		return nil, fmt.Errorf("codegraph_impact: %w", err)
	}

	ir, err := parseToolContent[*ImpactResult](result)
	if err != nil {
		return nil, err
	}
	return ir, nil
}

// Search finds symbols matching a query.
func (c *CodeGraphClient) Search(query string, limit int) ([]SymbolInfo, error) {
	args := map[string]any{"query": query}
	if limit > 0 {
		args["limit"] = limit
	}

	result, err := c.mcp.CallTool("codegraph_search", args)
	if err != nil {
		return nil, fmt.Errorf("codegraph_search: %w", err)
	}

	return parseToolContent[[]SymbolInfo](result)
}

// FileComplexity returns the number of symbols and callers for a file.
// Used by the sommelier to assess file risk.
func (c *CodeGraphClient) FileComplexity(filePath string) (symbols int, callers int, err error) {
	result, err := c.mcp.CallTool("codegraph_files", map[string]any{
		"path": filePath,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("codegraph_files: %w", err)
	}

	// Parse result for symbol count
	text, parseErr := extractText(result)
	if parseErr != nil {
		return 0, 0, parseErr
	}
	// Rough heuristic: count lines as proxy for complexity
	// A proper implementation would parse the structured response
	_ = text
	return 0, 0, nil // placeholder — enriched in MW-11
}

// Close terminates the MCP server.
func (c *CodeGraphClient) Close() error {
	return c.mcp.Close()
}

// extractText pulls plain text from an MCP tool response.
func extractText(raw json.RawMessage) (string, error) {
	// Try as plain string
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	// Try MCP content wrapper
	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return "", fmt.Errorf("parsing MCP text response: %w", err)
	}

	for _, c := range wrapper.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}

	return "", fmt.Errorf("no text content in MCP response")
}
