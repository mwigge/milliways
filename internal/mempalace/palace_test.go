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
	"os"
	"reflect"
	"testing"
)

type fakeRPC struct {
	resultByTool map[string]json.RawMessage
	argsByTool   map[string]map[string]any
	err          error
}

func (f *fakeRPC) CallTool(_ context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	if f.argsByTool == nil {
		f.argsByTool = make(map[string]map[string]any)
	}
	cloned := make(map[string]any, len(args))
	for key, value := range args {
		cloned[key] = value
	}
	f.argsByTool[toolName] = cloned
	if f.err != nil {
		return nil, f.err
	}
	return f.resultByTool[toolName], nil
}

func (f *fakeRPC) Close() error { return nil }

func TestClientFromEnvAndOperations(t *testing.T) {
	oldStart := startMCP
	fake := &fakeRPC{resultByTool: map[string]json.RawMessage{
		"mempalace_search":     []byte(`{"content":[{"type":"text","text":"[{\"wing\":\"private\",\"room\":\"sessions\",\"content\":\"context\",\"relevance\":0.9}]"}]}`),
		"mempalace_list_wings": []byte(`{"content":[{"type":"text","text":"[\"private\",\"company\"]"}]}`),
		"mempalace_list_rooms": []byte(`{"content":[{"type":"text","text":"[\"sessions\"]"}]}`),
	}}
	startMCP = func(command string, args ...string) (rpcCaller, error) {
		if command != "mcp-server" {
			t.Fatalf("command = %q, want mcp-server", command)
		}
		if !reflect.DeepEqual(args, []string{"--stdio", "--verbose"}) {
			t.Fatalf("args = %v, want [--stdio --verbose]", args)
		}
		return fake, nil
	}
	t.Cleanup(func() { startMCP = oldStart })

	t.Setenv("MEMPALACE_MCP_CMD", "mcp-server")
	t.Setenv("MEMPALACE_MCP_ARGS", "--stdio --verbose")

	client, err := NewClientFromEnv()
	if err != nil {
		t.Fatalf("NewClientFromEnv() error = %v", err)
	}

	hits, err := client.Search(context.Background(), "context", 3)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(hits) != 1 || hits[0].Wing != "private" {
		t.Fatalf("Search() = %#v", hits)
	}
	if err := client.Write(context.Background(), "private", "sessions", "drawer-1", "note"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	wings, err := client.ListWings(context.Background())
	if err != nil {
		t.Fatalf("ListWings() error = %v", err)
	}
	if !reflect.DeepEqual(wings, []string{"private", "company"}) {
		t.Fatalf("ListWings() = %v", wings)
	}
	rooms, err := client.ListRooms(context.Background(), "private")
	if err != nil {
		t.Fatalf("ListRooms() error = %v", err)
	}
	if !reflect.DeepEqual(rooms, []string{"sessions"}) {
		t.Fatalf("ListRooms() = %v", rooms)
	}
	if got := fake.argsByTool["mempalace_add_drawer"]["drawer"]; got != "drawer-1" {
		t.Fatalf("drawer arg = %#v, want drawer-1", got)
	}
}

func TestNewClientFromEnvRequiresCommand(t *testing.T) {
	t.Parallel()

	os.Unsetenv("MEMPALACE_MCP_CMD")
	_, err := NewClientFromEnv()
	if err == nil {
		t.Fatal("expected missing env error")
	}
}

func TestMockPalaceSearchWriteAndList(t *testing.T) {
	t.Parallel()

	palace := NewMockPalace([]SearchResult{{Wing: "private", Room: "sessions", Content: "mouse selection", Relevance: 0.9}})
	results, err := palace.Search(context.Background(), "mouse", 5)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(Search()) = %d, want 1", len(results))
	}
	if err := palace.Write(context.Background(), "company", "decisions", "drawer-1", "recorded decision"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	wings, err := palace.ListWings(context.Background())
	if err != nil {
		t.Fatalf("ListWings() error = %v", err)
	}
	if !reflect.DeepEqual(wings, []string{"company", "private"}) {
		t.Fatalf("wings = %v", wings)
	}
	rooms, err := palace.ListRooms(context.Background(), "company")
	if err != nil {
		t.Fatalf("ListRooms() error = %v", err)
	}
	if !reflect.DeepEqual(rooms, []string{"decisions"}) {
		t.Fatalf("rooms = %v", rooms)
	}
}

func TestClientPropagatesRPCError(t *testing.T) {
	t.Parallel()

	client := &Client{rpc: &fakeRPC{err: errors.New("boom")}}
	if _, err := client.Search(context.Background(), "x", 1); err == nil {
		t.Fatal("expected search error")
	}
}
