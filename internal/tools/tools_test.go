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
	"encoding/json"
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
	if len(defs) != 12 {
		t.Fatalf("tool count = %d, want 12", len(defs))
	}
	for _, name := range []string{"Read", "Write", "Edit", "ApplyPatch", "Delete", "Grep", "Glob", "Bash", "WebFetch", "Todo", "Question"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("missing tool %q", name)
		}
	}
}

func TestHandleReadWriteEditAndApplyPatch(t *testing.T) {
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
		"path":       path,
		"old_string": "world",
		"new_string": "gopher",
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

	_, err = handleApplyPatch(context.Background(), map[string]any{
		"path": path,
		"diff": "@@\n-gopher\n+agent\n",
	})
	if err != nil {
		t.Fatalf("handleApplyPatch() error = %v", err)
	}
	patched, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after patch error = %v", err)
	}
	if string(patched) != "hello\nagent\n" {
		t.Fatalf("patched = %q", string(patched))
	}
}

func TestHandleEditRequiresExactSingleReplacement(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", dir)
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("same\nsame\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := handleEdit(context.Background(), map[string]any{
		"path":       path,
		"old_string": "same",
		"new_string": "other",
	})
	if err == nil || !strings.Contains(err.Error(), "exactly once") {
		t.Fatalf("handleEdit() error = %v, want exactly-once error", err)
	}
}

func TestHandleDeleteRemovesWorkspaceFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", dir)
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("delete me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := handleDelete(context.Background(), map[string]any{"path": path}); err != nil {
		t.Fatalf("handleDelete() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("deleted file stat err = %v, want not exist", err)
	}
}

func TestHandleDeleteRejectsDenylist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", dir)
	secret := filepath.Join(dir, ".ssh", "id_rsa")
	if err := os.MkdirAll(filepath.Dir(secret), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := handleDelete(context.Background(), map[string]any{"path": secret}); err == nil {
		t.Fatal("handleDelete() allowed denylisted path")
	}
}

func TestTodoAndQuestionReturnJSON(t *testing.T) {
	t.Parallel()

	todo, err := handleTodo(context.Background(), map[string]any{"items": []any{"one", "two"}})
	if err != nil {
		t.Fatalf("handleTodo() error = %v", err)
	}
	var todoResult map[string]any
	if err := json.Unmarshal([]byte(todo), &todoResult); err != nil {
		t.Fatalf("todo result is not JSON: %v", err)
	}
	if todoResult["status"] != "recorded" {
		t.Fatalf("todo status = %v", todoResult["status"])
	}

	question, err := handleQuestion(context.Background(), map[string]any{"question": "Proceed?"})
	if err != nil {
		t.Fatalf("handleQuestion() error = %v", err)
	}
	if !strings.Contains(question, "Proceed?") {
		t.Fatalf("question result = %q", question)
	}
}

func TestHandleReadRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", dir)
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := handleRead(context.Background(), map[string]any{"path": link}); err == nil {
		t.Fatal("handleRead() allowed symlink escape")
	}
}

func TestHandleGlobFiltersSymlinkEscapes(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", dir)
	link := filepath.Join(dir, "outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	got, err := handleGlob(context.Background(), map[string]any{"path": dir, "pattern": "*/*.txt"})
	if err != nil {
		t.Fatalf("handleGlob() error = %v", err)
	}
	if strings.Contains(got, "secret.txt") || strings.Contains(got, outside) {
		t.Fatalf("handleGlob leaked symlink escape: %q", got)
	}
}

func TestHandleWriteRejectsBackupSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", dir)
	if err := os.Symlink(outside, path+".bak"); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := handleWrite(context.Background(), map[string]any{"path": path, "content": "new"}); err == nil {
		t.Fatal("handleWrite() allowed backup symlink escape")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("outside backup target was touched: %v", err)
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
