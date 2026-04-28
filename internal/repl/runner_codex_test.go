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

package repl

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCodexAssistantText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want string
		ok   bool
	}{
		{
			name: "item completed assistant message",
			line: `{"type":"item.completed","item":{"item_type":"assistant_message","text":"continuing work"}}`,
			want: "continuing work",
			ok:   true,
		},
		{
			name: "message content",
			line: `{"type":"message","content":"hello"}`,
			want: "hello",
			ok:   true,
		},
		{
			name: "tool event ignored",
			line: `{"type":"tool_call","tool":"shell"}`,
			ok:   false,
		},
		{
			name: "non json ignored",
			line: `not json`,
			ok:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := codexAssistantText(tt.line)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("codexAssistantText() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestCodexProgressText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want string
		ok   bool
	}{
		{
			name: "turn started",
			line: `{"type":"turn.started"}`,
			want: "* codex: started",
			ok:   true,
		},
		{
			name: "reasoning summary",
			line: `{"type":"reasoning.summary","summary":"Inspecting runner code"}`,
			want: "* codex: thinking - Inspecting runner code",
			ok:   true,
		},
		{
			name: "shell command item",
			line: `{"type":"item.started","item":{"item_type":"exec_command","command":"go test ./internal/repl"}}`,
			want: "* codex: shell - go test ./internal/repl",
			ok:   true,
		},
		{
			name: "file edit completed",
			line: `{"type":"item.completed","item":{"item_type":"file_change","path":"internal/repl/runner_codex.go"}}`,
			want: "ok codex: edit - internal/repl/runner_codex.go",
			ok:   true,
		},
		{
			name: "assistant item not progress",
			line: `{"type":"item.completed","item":{"item_type":"assistant_message","text":"done"}}`,
			ok:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := codexProgressText(tt.line, CodexReasoningVerbose)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("codexProgressText() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestRunCodexJSONWritesAssistantOutput(t *testing.T) {

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"type":"item.completed","item":{"item_type":"assistant_message","text":"6"}}'`)
	var buf bytes.Buffer

	if err := runCodexJSON(ctx, cmd, &buf, CodexReasoningVerbose); err != nil {
		t.Fatalf("runCodexJSON() = %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "6" {
		t.Fatalf("output = %q, want 6", got)
	}
}

func TestRunCodexJSONWritesProgressAndAssistantOutput(t *testing.T) {

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"type":"turn.started"}' '{"type":"item.started","item":{"item_type":"exec_command","command":"go test ./internal/repl"}}' '{"type":"item.completed","item":{"item_type":"assistant_message","text":"tests passed"}}'`)
	var buf bytes.Buffer

	if err := runCodexJSON(ctx, cmd, &buf, CodexReasoningVerbose); err != nil {
		t.Fatalf("runCodexJSON() = %v", err)
	}
	output := buf.String()
	for _, want := range []string{"* codex: started", "* codex: shell - go test ./internal/repl", "tests passed"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

func TestRunCodexJSONSuppressesProxyHTML(t *testing.T) {

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '2026-04-26T13:07:44Z ERROR failed unexpected status 403 Forbidden: <title>Internet Security by Zscaler</title>' >&2`)
	var buf bytes.Buffer

	err := runCodexJSON(ctx, cmd, &buf, CodexReasoningVerbose)
	if !errors.Is(err, ErrCodexProxyBlocked) {
		t.Fatalf("runCodexJSON() error = %v, want ErrCodexProxyBlocked", err)
	}
	output := buf.String()
	if !strings.Contains(output, "codex blocked by Zscaler/proxy") {
		t.Fatalf("output missing proxy hint: %q", output)
	}
	if strings.Contains(output, "<title>") || strings.Contains(output, "ERROR failed") {
		t.Fatalf("output leaked proxy diagnostics: %q", output)
	}
}

func TestCodexRunnerExecArgsIncludeSettings(t *testing.T) {
	t.Parallel()

	r := NewCodexRunner()
	r.SetModel("gpt-5.4")
	r.SetProfile("work")
	r.SetSandbox("workspace-write")
	r.SetApproval("on-request")
	r.SetSearch(true)
	r.AddImage("diagram.png")

	got := strings.Join(r.execArgs("fix bug"), "\x00")
	for _, want := range []string{
		"exec",
		"--json",
		"--color\x00never",
		"--model\x00gpt-5.4",
		"--profile\x00work",
		"--sandbox\x00workspace-write",
		"--ask-for-approval\x00on-request",
		"--search",
		"--image\x00diagram.png",
		"--\x00fix bug",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("execArgs missing %q in %#v", want, r.execArgs("fix bug"))
		}
	}
}

func TestNewCodexRunnerResolvesInstalledBinary(t *testing.T) {
	t.Setenv("MILLIWAYS_CODEX_BIN", "/custom/bin/codex")

	r := NewCodexRunner()
	if r.binary != "/custom/bin/codex" {
		t.Fatalf("binary = %q, want env override", r.binary)
	}
}

func TestCodexReasoningModeControlsProgress(t *testing.T) {
	t.Parallel()

	line := `{"type":"item.started","item":{"item_type":"generic_step","description":"checking repository"}}`

	if _, ok := codexProgressText(line, CodexReasoningOff); ok {
		t.Fatal("off mode emitted progress")
	}
	if _, ok := codexProgressText(line, CodexReasoningSummary); ok {
		t.Fatal("summary mode emitted generic verbose progress")
	}
	got, ok := codexProgressText(line, CodexReasoningVerbose)
	if !ok || got != "* codex: generic_step - checking repository" {
		t.Fatalf("verbose progress = (%q, %v)", got, ok)
	}
}
