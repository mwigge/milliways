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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MCPClient communicates with an MCP server via JSON-RPC over stdio.
type MCPClient struct {
	cmd    *exec.Cmd
	stdin  io.Writer
	reader *bufio.Reader
	mu     sync.Mutex
	nextID atomic.Int64
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// StartMCP spawns an MCP server process and connects via stdio.
func StartMCP(command string, args ...string) (*MCPClient, error) {
	cmd := exec.Command(command, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting MCP server %s: %w", command, err)
	}

	client := &MCPClient{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
	}

	// Initialize the MCP session
	initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer initCancel()

	_, err = client.Call(initCtx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "milliways",
			"version": "0.1.0",
		},
	})
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("MCP initialize: %w", err)
	}

	return client, nil
}

// mcpDefaultTimeout is the default timeout for MCP calls when no context deadline is set.
const mcpDefaultTimeout = 30 * time.Second

// mcpMaxResponseLines is the maximum number of response lines to read before giving up.
const mcpMaxResponseLines = 10000

// mcpMaxRetries is the maximum retry attempts for transient connection errors.
const mcpMaxRetries = 3

// callResult holds the result of a background read operation.
type callResult struct {
	data json.RawMessage
	err  error
}

// Call sends a JSON-RPC request and waits for the response.
// The context controls the overall timeout for the call.
// Transient connection errors (EPIPE, ECONNRESET, unexpected EOF) are retried
// up to mcpMaxRetries times with exponential backoff and jitter.
func (c *MCPClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var lastErr error
	for attempt := 0; attempt <= mcpMaxRetries; attempt++ {
		data, err := c.callOne(ctx, method, params)
		if err == nil {
			return data, nil
		}
		if !isRetryable(err) {
			return nil, err
		}
		lastErr = err
		if attempt < mcpMaxRetries {
			backoff := time.Duration(1<<attempt) * 100 * time.Millisecond
			if backoff > 10*time.Second {
				backoff = 10 * time.Second
			}
			// Add jitter: up to 25% randomness.
			jitter := time.Duration(attempt*13+7) * backoff / 100
			sleep := backoff + jitter
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("MCP call %s (retry %d/%d): %w", method, attempt+1, mcpMaxRetries, ctx.Err())
			case <-time.After(sleep):
			}
		}
	}
	return nil, fmt.Errorf("MCP call %s: %w", method, lastErr)
}

// callOne performs a single RPC call while holding the mutex.
// Returns retryable errors for transient connection failures.
func (c *MCPClient) callOne(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Apply default timeout if no deadline is set; always use a derived context
	// so that defer cancel() fires at the right scope regardless of which branch runs.
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		ctx, cancel = context.WithCancel(ctx)
	} else {
		ctx, cancel = context.WithTimeout(ctx, mcpDefaultTimeout)
	}
	defer cancel()

	id := c.nextID.Add(1)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("writing request: %w", err)
	}

	// Read response in a goroutine so we can respect context cancellation
	ch := make(chan callResult, 1)
	go func() {
		for linesRead := 0; linesRead < mcpMaxResponseLines; linesRead++ {
			line, readErr := c.reader.ReadBytes('\n')
			if readErr != nil {
				ch <- callResult{err: fmt.Errorf("reading response: %w", readErr)}
				return
			}

			var resp jsonRPCResponse
			if unmarshalErr := json.Unmarshal(line, &resp); unmarshalErr != nil {
				continue // skip non-JSON lines (notifications, logs)
			}

			if resp.ID == id {
				if resp.Error != nil {
					ch <- callResult{err: fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)}
					return
				}
				ch <- callResult{data: resp.Result}
				return
			}
			// Not our response — could be a notification, skip it
		}
		ch <- callResult{err: fmt.Errorf("MCP response not found after %d lines", mcpMaxResponseLines)}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("MCP call %s: %w", method, ctx.Err())
	case result := <-ch:
		return result.data, result.err
	}
}

// isRetryable returns true if err is a transient connection error worth retrying.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// EOF and unexpected EOF indicate a broken connection.
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	// EPIPE and ECONNRESET indicate the subprocess died.
	if strings.Contains(errStr, "epipe") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "econnreset") ||
		strings.Contains(errStr, "connection reset by peer") {
		return true
	}
	// "read from closed pipe" or "write to closed pipe" are also retryable.
	if strings.Contains(errStr, "closed pipe") {
		return true
	}
	return false
}

// CallTool invokes an MCP tool and returns the result.
func (c *MCPClient) CallTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	return c.Call(ctx, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": args,
	})
}

// Close terminates the MCP server process.
func (c *MCPClient) Close() error {
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}
