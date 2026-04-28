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
	"context"
	"encoding/json"
	"testing"

	"github.com/mwigge/milliways/internal/tools"
)

type fakeCaller struct {
	toolName string
	args     map[string]any
	result   json.RawMessage
}

func (f *fakeCaller) CallTool(_ context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	f.toolName = toolName
	f.args = args
	return f.result, nil
}

func TestLoadServers(t *testing.T) {
	t.Parallel()

	servers, err := LoadServers(map[string]ServerConfig{
		"filesystem": {Type: "stdio", Command: "npx", Args: []string{"server"}},
	})
	if err != nil {
		t.Fatalf("LoadServers() error = %v", err)
	}
	if len(servers) != 1 || servers[0].Name != "filesystem" {
		t.Fatalf("servers = %+v", servers)
	}
}

func TestServerRegisterTools(t *testing.T) {
	t.Parallel()

	fake := &fakeCaller{result: json.RawMessage(`{"ok":true}`)}
	server := &Server{
		Name:   "filesystem",
		Client: fake,
		Tools: []RemoteTool{{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: map[string]any{"type": "object"},
		}},
	}
	registry := tools.NewRegistry()
	if err := server.RegisterTools(registry); err != nil {
		t.Fatalf("RegisterTools() error = %v", err)
	}
	handler, ok := registry.Get("mcp:read_file")
	if !ok {
		t.Fatal("expected registered MCP tool")
	}
	result, err := handler(context.Background(), map[string]any{"path": "README.md"})
	if err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if fake.toolName != "read_file" {
		t.Fatalf("toolName = %q", fake.toolName)
	}
	if fake.args["path"] != "README.md" {
		t.Fatalf("args = %+v", fake.args)
	}
	if result != `{"ok":true}` {
		t.Fatalf("result = %q", result)
	}
}

func TestDecodeTools(t *testing.T) {
	t.Parallel()

	tools, err := decodeTools(json.RawMessage(`{"tools":[{"name":"search","description":"Search","inputSchema":{"type":"object"}}]}`))
	if err != nil {
		t.Fatalf("decodeTools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "search" {
		t.Fatalf("tools = %+v", tools)
	}
}
