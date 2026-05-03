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

// Package rpc provides the milliwaysd <-> milliways-term JSON-RPC 2.0 client
// types. Generated message shapes live in types.go (see
// scripts/gen-rpc-types.sh). The hand-rolled Client is here.
package rpc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
)

// Socket returns the UDS path this client is connected to. Used by
// Subscribe to dial the sidecar against the same socket.
func (c *Client) Socket() string { return c.socket }

// Client is a synchronous newline-delimited JSON-RPC 2.0 client. One
// in-flight call at a time per Client. For concurrency, dial more clients.
type Client struct {
	socket string
	conn   net.Conn
	enc    *json.Encoder
	scan   *bufio.Scanner
	nextID atomic.Int64
}

// Dial connects to the milliwaysd UDS at socket.
func Dial(socket string) (*Client, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socket, err)
	}
	c := &Client{
		socket: socket,
		conn:   conn,
		enc:    json.NewEncoder(conn),
		scan:   bufio.NewScanner(conn),
	}
	c.scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return c, nil
}

// Close releases the underlying UDS connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

type clientRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      int64  `json:"id"`
}

type clientResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      int64           `json:"id"`
}

// RPCError is a JSON-RPC 2.0 error returned by the daemon. The numeric Code
// matches the catalogue in term-daemon-rpc/spec.md.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Call invokes method with params and decodes the result into result. result
// may be nil to discard the body.
// Subscribe calls a *.subscribe-style method, opens a sidecar connection,
// and returns a channel of NDJSON event lines (events) plus a cancel
// function. The events channel is closed when the daemon sends `{"t":"end"}`,
// the sidecar conn drops, or cancel() is invoked. Each event is the raw
// JSON line minus the trailing newline.
func (c *Client) Subscribe(method string, params any) (<-chan []byte, func(), error) {
	return c.subscribeImpl(c.socket, method, params)
}

func (c *Client) subscribeImpl(socket, method string, params any) (<-chan []byte, func(), error) {
	// 1. Unary call to get the stream_id.
	var resp struct {
		StreamID     int64 `json:"stream_id"`
		OutputOffset int64 `json:"output_offset"`
	}
	if err := c.Call(method, params, &resp); err != nil {
		return nil, nil, fmt.Errorf("subscribe call: %w", err)
	}
	// 2. Dial the sidecar.
	side, err := net.Dial("unix", socket)
	if err != nil {
		return nil, nil, fmt.Errorf("dial sidecar: %w", err)
	}
	// 3. Send the STREAM preamble.
	preamble := fmt.Sprintf("STREAM %d %d\n", resp.StreamID, resp.OutputOffset)
	if _, err := side.Write([]byte(preamble)); err != nil {
		side.Close()
		return nil, nil, fmt.Errorf("write preamble: %w", err)
	}
	events := make(chan []byte, 16)
	cancel := func() { side.Close() }
	go func() {
		defer close(events)
		defer side.Close()
		scan := bufio.NewScanner(side)
		scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scan.Scan() {
			line := append([]byte(nil), scan.Bytes()...)
			events <- line
		}
	}()
	return events, cancel, nil
}

func (c *Client) Call(method string, params any, result any) error {
	id := c.nextID.Add(1)
	req := clientRequest{JSONRPC: "2.0", Method: method, Params: params, ID: id}
	if err := c.enc.Encode(req); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if !c.scan.Scan() {
		if err := c.scan.Err(); err != nil {
			return err
		}
		return fmt.Errorf("connection closed")
	}
	var resp clientResponse
	if err := json.Unmarshal(c.scan.Bytes(), &resp); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if result != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}
