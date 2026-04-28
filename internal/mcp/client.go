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

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Caller is the shared MCP client interface used by server registration.
type Caller interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error)
}

// Client communicates with an MCP server over JSON-RPC.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.Writer
	reader *bufio.Reader
	mu     sync.Mutex
	nextID atomic.Int64
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

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

// Start starts an stdio MCP server client.
func Start(command string, args ...string) (*Client, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mcp process: %w", err)
	}
	return &Client{cmd: cmd, stdin: stdin, reader: bufio.NewReader(stdout)}, nil
}

// Call sends one JSON-RPC request.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c == nil {
		return nil, fmt.Errorf("nil mcp client")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	id := c.nextID.Add(1)
	req := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')
	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	resultCh := make(chan struct {
		data json.RawMessage
		err  error
	}, 1)
	go func() {
		for {
			line, err := c.reader.ReadBytes('\n')
			if err != nil {
				resultCh <- struct {
					data json.RawMessage
					err  error
				}{err: fmt.Errorf("read response: %w", err)}
				return
			}
			var resp jsonRPCResponse
			if err := json.Unmarshal(line, &resp); err != nil {
				continue
			}
			if resp.ID != id {
				continue
			}
			if resp.Error != nil {
				resultCh <- struct {
					data json.RawMessage
					err  error
				}{err: fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)}
				return
			}
			resultCh <- struct {
				data json.RawMessage
				err  error
			}{data: resp.Result}
			return
		}
	}()

	select {
	case <-callCtx.Done():
		return nil, fmt.Errorf("mcp call %s: %w", method, callCtx.Err())
	case result := <-resultCh:
		return result.data, result.err
	}
}

// CallTool invokes a remote MCP tool.
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	return c.Call(ctx, "tools/call", map[string]any{"name": toolName, "arguments": args})
}

// Close terminates the underlying process.
func (c *Client) Close() error {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
	_ = c.cmd.Process.Kill()
	return c.cmd.Wait()
}

var _ Caller = (*Client)(nil)
