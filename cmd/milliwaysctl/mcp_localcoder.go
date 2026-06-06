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

package main

// `milliwaysctl mcp-localcoder` — minimal JSON-RPC 2.0 MCP server over
// stdio that exposes a `local_code` tool backed by the milliways local
// LLM endpoint (MILLIWAYS_LOCAL_ENDPOINT).
//
// Claude CLI invokes this as a subprocess when --mcp-config references it.
// All communication is over stdin/stdout; stderr is silent in normal use.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func runMCPLocalCoder(_ []string) int {
	endpoint := strings.TrimRight(os.Getenv("MILLIWAYS_LOCAL_ENDPOINT"), "/")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8765/v1"
	}
	model := os.Getenv("MILLIWAYS_LOCAL_MODEL")
	if model == "" {
		model = "default"
	}
	srv := &mcpLocalCoderSrv{endpoint: endpoint, model: model}
	srv.serve(os.Stdin, os.Stdout)
	return 0
}

type mcpLocalCoderSrv struct {
	endpoint string
	model    string
}

// mcpRawMsg is used for both requests and responses so the numeric or
// string ID passes through unchanged.
type mcpRawReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // nil/absent = notification
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpRawResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *mcpLocalCoderSrv) serve(r io.Reader, w io.Writer) {
	enc := json.NewEncoder(w)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req mcpRawReq
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		// MCP notifications have no id — don't reply.
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue
		}
		resp := s.handle(req)
		_ = enc.Encode(resp)
	}
}

func (s *mcpLocalCoderSrv) handle(req mcpRawReq) mcpRawResp {
	switch req.Method {
	case "initialize":
		return mcpRawResp{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{
					"name":    "milliways-local-coder",
					"version": "1.0.0",
				},
			},
		}

	case "tools/list":
		return mcpRawResp{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": []any{
					map[string]any{
						"name":        "local_code",
						"description": "Generate code using the local on-device LLM. Fast for boilerplate, implementation, and repetitive coding tasks. If the tool takes more than 15s or returns low-quality output, fall back to your own reasoning.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"prompt": map[string]any{
									"type":        "string",
									"description": "The coding task, specification, or question to implement",
								},
							},
							"required": []string{"prompt"},
						},
					},
				},
			},
		}

	case "tools/call":
		return s.toolCall(req)

	default:
		return mcpRawResp{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &mcpRPCError{Code: -32601, Message: "method not found: " + req.Method},
		}
	}
}

func (s *mcpLocalCoderSrv) toolCall(req mcpRawReq) mcpRawResp {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return mcpRawResp{JSONRPC: "2.0", ID: req.ID,
			Error: &mcpRPCError{Code: -32602, Message: "invalid params: " + err.Error()}}
	}
	if p.Name != "local_code" {
		return mcpRawResp{JSONRPC: "2.0", ID: req.ID,
			Error: &mcpRPCError{Code: -32602, Message: "unknown tool: " + p.Name}}
	}
	var args struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(p.Arguments, &args); err != nil || args.Prompt == "" {
		return mcpRawResp{JSONRPC: "2.0", ID: req.ID,
			Error: &mcpRPCError{Code: -32602, Message: "prompt argument required"}}
	}

	result, elapsed, err := s.callLocal(args.Prompt)
	if err != nil {
		msg := fmt.Sprintf("[local_code failed after %s: %v — use your own reasoning instead]",
			elapsed.Round(time.Millisecond), err)
		return mcpRawResp{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []any{map[string]any{"type": "text", "text": msg}},
				"isError": true,
			},
		}
	}

	text := result
	if elapsed > 10*time.Second {
		text += fmt.Sprintf(
			"\n\n[local_code: took %s — if this is too slow, handle faster tasks locally]",
			elapsed.Round(time.Second))
	}
	return mcpRawResp{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []any{map[string]any{"type": "text", "text": text}},
		},
	}
}

type lcChatReq struct {
	Model    string      `json:"model"`
	Messages []lcMessage `json:"messages"`
}

type lcMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type lcChatResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (s *mcpLocalCoderSrv) callLocal(prompt string) (string, time.Duration, error) {
	body, _ := json.Marshal(lcChatReq{
		Model:    s.model,
		Messages: []lcMessage{{Role: "user", Content: prompt}},
	})
	start := time.Now()
	hc := &http.Client{Timeout: 120 * time.Second}
	resp, err := hc.Post(s.endpoint+"/chat/completions", "application/json", bytes.NewReader(body))
	elapsed := time.Since(start)
	if err != nil {
		return "", elapsed, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", elapsed, fmt.Errorf("HTTP %d from local server", resp.StatusCode)
	}
	var out lcChatResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", elapsed, err
	}
	if len(out.Choices) == 0 {
		return "", elapsed, fmt.Errorf("empty choices from local server")
	}
	return out.Choices[0].Message.Content, elapsed, nil
}
