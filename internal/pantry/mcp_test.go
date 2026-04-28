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
	"encoding/json"
	"testing"
)

func TestParseToolContent_DirectJSON(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`[{"id": "d1", "text": "decision about auth", "wing": "cls", "room": "auth", "score": 0.95}]`)

	drawers, err := parseToolContent[[]Drawer](raw)
	if err != nil {
		t.Fatalf("parseToolContent: %v", err)
	}
	if len(drawers) != 1 {
		t.Fatalf("expected 1 drawer, got %d", len(drawers))
	}
	if drawers[0].Text != "decision about auth" {
		t.Errorf("expected 'decision about auth', got %q", drawers[0].Text)
	}
	if drawers[0].Score != 0.95 {
		t.Errorf("expected score 0.95, got %f", drawers[0].Score)
	}
}

func TestParseToolContent_MCPWrapper(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"content": [{"type": "text", "text": "[{\"id\": \"d1\", \"text\": \"wrapped\", \"wing\": \"test\", \"room\": \"r\", \"score\": 0.8}]"}]}`)

	drawers, err := parseToolContent[[]Drawer](raw)
	if err != nil {
		t.Fatalf("parseToolContent: %v", err)
	}
	if len(drawers) != 1 {
		t.Fatalf("expected 1 drawer, got %d", len(drawers))
	}
	if drawers[0].Text != "wrapped" {
		t.Errorf("expected 'wrapped', got %q", drawers[0].Text)
	}
}

func TestParseToolContent_EmptyContent(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"content": []}`)

	_, err := parseToolContent[[]Drawer](raw)
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestExtractText_PlainString(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`"hello world"`)
	text, err := extractText(raw)
	if err != nil {
		t.Fatalf("extractText: %v", err)
	}
	if text != "hello world" {
		t.Errorf("expected 'hello world', got %q", text)
	}
}

func TestExtractText_MCPWrapper(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"content": [{"type": "text", "text": "context for task"}]}`)
	text, err := extractText(raw)
	if err != nil {
		t.Fatalf("extractText: %v", err)
	}
	if text != "context for task" {
		t.Errorf("expected 'context for task', got %q", text)
	}
}

func TestExtractText_NoTextContent(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"content": [{"type": "image", "data": "..."}]}`)
	_, err := extractText(raw)
	if err == nil {
		t.Error("expected error for no text content")
	}
}

func TestJSONRPCRequest_Marshal(t *testing.T) {
	t.Parallel()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      "mempalace_search",
			"arguments": map[string]any{"query": "auth", "limit": 5},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %v", parsed["jsonrpc"])
	}
	if parsed["method"] != "tools/call" {
		t.Errorf("expected method tools/call, got %v", parsed["method"])
	}
}

func TestJSONRPCResponse_WithError(t *testing.T) {
	t.Parallel()

	raw := `{"jsonrpc": "2.0", "id": 1, "error": {"code": -32601, "message": "method not found"}}`

	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", resp.Error.Code)
	}
}

func TestJSONRPCResponse_WithResult(t *testing.T) {
	t.Parallel()

	raw := `{"jsonrpc": "2.0", "id": 1, "result": {"content": [{"type": "text", "text": "ok"}]}}`

	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("expected result")
	}
}
