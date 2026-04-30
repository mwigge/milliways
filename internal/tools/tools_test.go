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

package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/provider"
)

func TestNewBuiltInRegistryContainsAllTools(t *testing.T) {
	t.Parallel()

	registry := NewBuiltInRegistry()
	defs := registry.List()
	if len(defs) != 7 {
		t.Fatalf("tool count = %d, want 7", len(defs))
	}
	for _, name := range []string{"Read", "Write", "Edit", "Grep", "Glob", "Bash", "WebFetch"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("missing tool %q", name)
		}
	}
}

func TestHandleReadWriteAndEdit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", dir)
	path := filepath.Join(dir, "sample.txt")

	if _, err := handleWrite(context.Background(), map[string]any{"path": path, "content": "hello\nworld\n"}); err != nil {
		t.Fatalf("handleWrite() error = %v", err)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("unexpected backup existence err = %v", err)
	}
	content, err := handleRead(context.Background(), map[string]any{"path": path})
	if err != nil {
		t.Fatalf("handleRead() error = %v", err)
	}
	if content != "hello\nworld\n" {
		t.Fatalf("content = %q", content)
	}
	_, err = handleEdit(context.Background(), map[string]any{
		"path": path,
		"diff": "@@\n-world\n+gopher\n",
	})
	if err != nil {
		t.Fatalf("handleEdit() error = %v", err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(updated) != "hello\ngopher\n" {
		t.Fatalf("updated = %q", string(updated))
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup: %v", err)
	}
}

func TestHandleGrepAndGlob(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", dir)
	alpha := filepath.Join(dir, "alpha.txt")
	beta := filepath.Join(dir, "beta.md")
	if err := os.WriteFile(alpha, []byte("hello\nneedle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(beta, []byte("needle in markdown\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	grepResult, err := handleGrep(context.Background(), map[string]any{"path": dir, "pattern": "needle", "include": "*.txt"})
	if err != nil {
		t.Fatalf("handleGrep() error = %v", err)
	}
	if !strings.Contains(grepResult, "alpha.txt:2:needle") || strings.Contains(grepResult, "beta.md") {
		t.Fatalf("grep result = %q", grepResult)
	}

	globResult, err := handleGlob(context.Background(), map[string]any{"path": dir, "pattern": "*.txt"})
	if err != nil {
		t.Fatalf("handleGlob() error = %v", err)
	}
	if !strings.Contains(globResult, "alpha.txt") || strings.Contains(globResult, "beta.md") {
		t.Fatalf("glob result = %q", globResult)
	}
}

func TestHandleBash(t *testing.T) {
	// handleBash now pins cwd to the workspace root; test default.
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", t.TempDir())
	result, err := handleBash(context.Background(), map[string]any{"command": "printf 'hello'", "timeout": 1.0})
	if err != nil {
		t.Fatalf("handleBash() error = %v", err)
	}
	if result != "hello" {
		t.Fatalf("result = %q", result)
	}
}

func TestHandleWebFetch(t *testing.T) {
	// httptest.NewServer binds 127.0.0.1; the production SSRF block
	// rejects loopback. The opt-in env var allows it for testing.
	t.Setenv("MILLIWAYS_TOOLS_ALLOW_LOOPBACK", "1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer server.Close()

	result, err := handleWebFetch(context.Background(), map[string]any{"url": server.URL, "timeout": float64((1 * time.Second).Seconds())})
	if err != nil {
		t.Fatalf("handleWebFetch() error = %v", err)
	}
	if result != "payload" {
		t.Fatalf("result = %q", result)
	}
}

func TestRegistryExecToolEmitsTraceEvent(t *testing.T) {
	t.Parallel()

	emitter, err := observability.NewTraceEmitterForDir("tool-success", t.TempDir())
	if err != nil {
		t.Fatalf("NewTraceEmitterForDir() error = %v", err)
	}

	registry := NewRegistryWithEmitter(emitter)
	registry.Register("Read", func(context.Context, map[string]any) (string, error) {
		return "ok", nil
	}, providerTestToolDef("Read"))

	result, err := registry.ExecTool(context.Background(), "session-1", "Read", map[string]any{"path": "README.md"})
	if err != nil {
		t.Fatalf("ExecTool() error = %v", err)
	}
	if result != "ok" {
		t.Fatalf("result = %q, want ok", result)
	}

	events, err := observability.ReadTraceFile(emitter.TraceFilePath())
	if err != nil {
		t.Fatalf("ReadTraceFile() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Type != "agent.tool" {
		t.Fatalf("event type = %q, want agent.tool", events[0].Type)
	}
	if got := events[0].Data["dur_ms"]; got == nil {
		t.Fatal("expected dur_ms in trace event")
	}
	if got := events[0].Data["blocked"]; got != false {
		t.Fatalf("blocked = %v, want false", got)
	}
}

func TestRegistryExecToolMarksBlockedError(t *testing.T) {
	t.Parallel()

	emitter, err := observability.NewTraceEmitterForDir("tool-failure", t.TempDir())
	if err != nil {
		t.Fatalf("NewTraceEmitterForDir() error = %v", err)
	}

	registry := NewRegistryWithEmitter(emitter)
	registry.Register("Read", func(context.Context, map[string]any) (string, error) {
		return "", context.DeadlineExceeded
	}, providerTestToolDef("Read"))

	_, err = registry.ExecTool(context.Background(), "session-1", "Read", map[string]any{"path": "README.md"})
	if err == nil {
		t.Fatal("expected error")
	}

	events, err := observability.ReadTraceFile(emitter.TraceFilePath())
	if err != nil {
		t.Fatalf("ReadTraceFile() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if got := events[0].Data["dur_ms"]; got == nil {
		t.Fatal("expected dur_ms in trace event")
	}
	if got := events[0].Data["blocked"]; got != false {
		t.Fatalf("blocked = %v, want false for non-blocking error", got)
	}

	registry = NewRegistryWithEmitter(emitter)
	registry.Register("Bash", func(context.Context, map[string]any) (string, error) {
		return "", errBlockedTool
	}, providerTestToolDef("Bash"))

	_, _ = registry.ExecTool(context.Background(), "session-1", "Bash", map[string]any{"command": "ls"})
	events, err = observability.ReadTraceFile(emitter.TraceFilePath())
	if err != nil {
		t.Fatalf("ReadTraceFile() error = %v", err)
	}
	if got := events[len(events)-1].Data["blocked"]; got != true {
		t.Fatalf("blocked = %v, want true", got)
	}
}

var errBlockedTool = &toolErr{msg: "blocked by policy"}

type toolErr struct{ msg string }

func (e *toolErr) Error() string { return e.msg }

func providerTestToolDef(name string) provider.ToolDef {
	return provider.ToolDef{Name: name, Description: name}
}
