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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShellOutputBuffer_Write_Snapshot verifies basic write and snapshot.
func TestShellOutputBuffer_Write_Snapshot(t *testing.T) {
	t.Parallel()

	buf := NewShellOutputBuffer(1024)
	input := "hello world\n"
	n, err := buf.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(input) {
		t.Errorf("Write() n = %d, want %d", n, len(input))
	}

	got := buf.Snapshot()
	if got != input {
		t.Errorf("Snapshot() = %q, want %q", got, input)
	}
}

// TestShellOutputBuffer_Overflow verifies ring buffer caps at capacity.
func TestShellOutputBuffer_Overflow(t *testing.T) {
	t.Parallel()

	cap := 10
	buf := NewShellOutputBuffer(cap)

	// Write more than capacity.
	first := []byte("AAAAAAAAAA") // 10 bytes — fills exactly
	second := []byte("BBBB")      // 4 bytes — pushes 4 A's out

	_, _ = buf.Write(first)
	_, _ = buf.Write(second)

	got := buf.Snapshot()
	if len(got) > cap {
		t.Errorf("Snapshot() length = %d, want <= %d", len(got), cap)
	}
	if !strings.HasSuffix(got, "BBBB") {
		t.Errorf("Snapshot() = %q; want suffix BBBB (most recent data)", got)
	}
}

// TestResolveContext_NoTokens verifies plain prompts pass through unchanged.
func TestResolveContext_NoTokens(t *testing.T) {
	t.Parallel()

	buf := NewShellOutputBuffer(1024)
	enriched, err := ResolveContext("just a plain prompt", buf)
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}
	if enriched.Text != "just a plain prompt" {
		t.Errorf("Text = %q, want %q", enriched.Text, "just a plain prompt")
	}
	if len(enriched.Fragments) != 0 {
		t.Errorf("Fragments len = %d, want 0", len(enriched.Fragments))
	}
}

// TestResolveContext_UnknownToken verifies unknown @-tokens are left in Text.
func TestResolveContext_UnknownToken(t *testing.T) {
	t.Parallel()

	buf := NewShellOutputBuffer(1024)
	enriched, err := ResolveContext("hello @unknown token here", buf)
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}
	if !strings.Contains(enriched.Text, "@unknown") {
		t.Errorf("Text = %q; want @unknown to remain", enriched.Text)
	}
	if len(enriched.Fragments) != 0 {
		t.Errorf("Fragments len = %d, want 0", len(enriched.Fragments))
	}
}

// TestResolveContext_AtFile verifies @file <path> reads file content.
func TestResolveContext_AtFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("file content here"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	buf := NewShellOutputBuffer(1024)
	prompt := "look at this @file " + path
	enriched, err := ResolveContext(prompt, buf)
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}

	if strings.Contains(enriched.Text, "@file") {
		t.Errorf("Text = %q; @file token should be removed", enriched.Text)
	}
	if strings.Contains(enriched.Text, path) {
		t.Errorf("Text = %q; path should be removed from text", enriched.Text)
	}
	if len(enriched.Fragments) != 1 {
		t.Fatalf("Fragments len = %d, want 1", len(enriched.Fragments))
	}
	if enriched.Fragments[0].Label != "@file:"+path {
		t.Errorf("Label = %q, want %q", enriched.Fragments[0].Label, "@file:"+path)
	}
	if enriched.Fragments[0].Content != "file content here" {
		t.Errorf("Content = %q, want %q", enriched.Fragments[0].Content, "file content here")
	}
}

// TestResolveContext_AtFileColon verifies @file:<path> syntax.
func TestResolveContext_AtFileColon(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "colon.txt")
	if err := os.WriteFile(path, []byte("colon syntax"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	buf := NewShellOutputBuffer(1024)
	prompt := "check @file:" + path + " please"
	enriched, err := ResolveContext(prompt, buf)
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}

	if strings.Contains(enriched.Text, "@file") {
		t.Errorf("Text = %q; @file token should be removed", enriched.Text)
	}
	if len(enriched.Fragments) != 1 {
		t.Fatalf("Fragments len = %d, want 1", len(enriched.Fragments))
	}
	if enriched.Fragments[0].Content != "colon syntax" {
		t.Errorf("Content = %q, want %q", enriched.Fragments[0].Content, "colon syntax")
	}
}

// TestResolveContext_AtGit verifies @git produces a fragment labelled "@git".
func TestResolveContext_AtGit(t *testing.T) {
	t.Parallel()

	buf := NewShellOutputBuffer(1024)
	enriched, err := ResolveContext("show me @git", buf)
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}

	if strings.Contains(enriched.Text, "@git") {
		t.Errorf("Text = %q; @git token should be removed", enriched.Text)
	}
	if len(enriched.Fragments) != 1 {
		t.Fatalf("Fragments len = %d, want 1", len(enriched.Fragments))
	}
	if enriched.Fragments[0].Label != "@git" {
		t.Errorf("Label = %q, want %q", enriched.Fragments[0].Label, "@git")
	}
}

// TestResolveContext_AtBranch verifies @branch produces a fragment labelled "@branch".
func TestResolveContext_AtBranch(t *testing.T) {
	t.Parallel()

	buf := NewShellOutputBuffer(1024)
	enriched, err := ResolveContext("on @branch fix this", buf)
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}

	if strings.Contains(enriched.Text, "@branch") {
		t.Errorf("Text = %q; @branch token should be removed", enriched.Text)
	}
	if len(enriched.Fragments) != 1 {
		t.Fatalf("Fragments len = %d, want 1", len(enriched.Fragments))
	}
	if enriched.Fragments[0].Label != "@branch" {
		t.Errorf("Label = %q, want %q", enriched.Fragments[0].Label, "@branch")
	}
}

// TestResolveContext_AtShell verifies @shell reads from the ShellOutputBuffer.
func TestResolveContext_AtShell(t *testing.T) {
	t.Parallel()

	buf := NewShellOutputBuffer(1024)
	_, _ = buf.Write([]byte("ls output\n"))

	enriched, err := ResolveContext("based on @shell output", buf)
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}

	if strings.Contains(enriched.Text, "@shell") {
		t.Errorf("Text = %q; @shell token should be removed", enriched.Text)
	}
	if len(enriched.Fragments) != 1 {
		t.Fatalf("Fragments len = %d, want 1", len(enriched.Fragments))
	}
	if enriched.Fragments[0].Label != "@shell" {
		t.Errorf("Label = %q, want %q", enriched.Fragments[0].Label, "@shell")
	}
	if enriched.Fragments[0].Content != "ls output\n" {
		t.Errorf("Content = %q, want %q", enriched.Fragments[0].Content, "ls output\n")
	}
}

// TestResolveContext_MultipleTokens verifies a prompt with @branch and @file.
func TestResolveContext_MultipleTokens(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "multi.txt")
	if err := os.WriteFile(path, []byte("multi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	buf := NewShellOutputBuffer(1024)
	prompt := "branch @branch and file @file " + path + " done"
	enriched, err := ResolveContext(prompt, buf)
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}

	if strings.Contains(enriched.Text, "@branch") || strings.Contains(enriched.Text, "@file") {
		t.Errorf("Text = %q; tokens should be removed", enriched.Text)
	}
	if len(enriched.Fragments) != 2 {
		t.Fatalf("Fragments len = %d, want 2", len(enriched.Fragments))
	}

	labels := make(map[string]bool)
	for _, f := range enriched.Fragments {
		labels[f.Label] = true
	}
	if !labels["@branch"] {
		t.Error("missing @branch fragment")
	}
	if !labels["@file:"+path] {
		t.Errorf("missing @file:%s fragment; got labels %v", path, labels)
	}
}

// TestBuildTextPrompt_WithContext verifies context fragments appear before the prompt.
func TestBuildTextPrompt_WithContext(t *testing.T) {
	t.Parallel()

	req := DispatchRequest{
		Prompt: "my prompt",
		Context: []ContextFragment{
			{Label: "@git", Content: "diff content"},
			{Label: "@file:foo.go", Content: "package foo"},
		},
	}

	got := buildTextPrompt(req)

	gitIdx := strings.Index(got, "## @git")
	fileIdx := strings.Index(got, "## @file:foo.go")
	promptIdx := strings.Index(got, "my prompt")

	if gitIdx < 0 {
		t.Error("missing ## @git section")
	}
	if fileIdx < 0 {
		t.Error("missing ## @file:foo.go section")
	}
	if promptIdx < 0 {
		t.Error("missing prompt text")
	}
	if gitIdx > promptIdx {
		t.Error("@git section should appear before prompt")
	}
	if fileIdx > promptIdx {
		t.Error("@file section should appear before prompt")
	}
	if !strings.Contains(got, "diff content") {
		t.Error("missing fragment content: diff content")
	}
	if !strings.Contains(got, "package foo") {
		t.Error("missing fragment content: package foo")
	}
}
