package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractCodeBlocks_Basic(t *testing.T) {
	t.Parallel()

	text := "```go\nfunc main() {}\n```"
	blocks := ExtractCodeBlocks(text)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Lang != "go" {
		t.Errorf("Lang = %q, want %q", b.Lang, "go")
	}
	if b.FilePath != "" {
		t.Errorf("FilePath = %q, want empty", b.FilePath)
	}
	if b.Index != 0 {
		t.Errorf("Index = %d, want 0", b.Index)
	}
	if !strings.Contains(b.Content, "func main()") {
		t.Errorf("Content does not contain expected text: %q", b.Content)
	}
}

func TestExtractCodeBlocks_InfoStringPath(t *testing.T) {
	t.Parallel()

	text := "```go internal/foo/bar.go\npackage foo\n```"
	blocks := ExtractCodeBlocks(text)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Lang != "go" {
		t.Errorf("Lang = %q, want %q", blocks[0].Lang, "go")
	}
	if blocks[0].FilePath != "internal/foo/bar.go" {
		t.Errorf("FilePath = %q, want %q", blocks[0].FilePath, "internal/foo/bar.go")
	}
}

func TestExtractCodeBlocks_CommentPath(t *testing.T) {
	t.Parallel()

	text := "```go\n// file: main.go\npackage main\n```"
	blocks := ExtractCodeBlocks(text)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].FilePath != "main.go" {
		t.Errorf("FilePath = %q, want %q", blocks[0].FilePath, "main.go")
	}
}

func TestExtractCodeBlocks_CommentPathHash(t *testing.T) {
	t.Parallel()

	text := "```python\n# file: script.py\nprint('hello')\n```"
	blocks := ExtractCodeBlocks(text)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].FilePath != "script.py" {
		t.Errorf("FilePath = %q, want %q", blocks[0].FilePath, "script.py")
	}
}

func TestExtractCodeBlocks_Multi(t *testing.T) {
	t.Parallel()

	text := "First:\n```go\npackage a\n```\nSecond:\n```python\nprint(1)\n```"
	blocks := ExtractCodeBlocks(text)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Index != 0 {
		t.Errorf("blocks[0].Index = %d, want 0", blocks[0].Index)
	}
	if blocks[1].Index != 1 {
		t.Errorf("blocks[1].Index = %d, want 1", blocks[1].Index)
	}
	if blocks[0].Lang != "go" {
		t.Errorf("blocks[0].Lang = %q, want %q", blocks[0].Lang, "go")
	}
	if blocks[1].Lang != "python" {
		t.Errorf("blocks[1].Lang = %q, want %q", blocks[1].Lang, "python")
	}
}

func TestExtractCodeBlocks_IsDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "lang diff",
			text: "```diff\n--- a/file.go\n+++ b/file.go\n```",
			want: true,
		},
		{
			name: "content looks like diff",
			text: "```\n--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n```",
			want: true,
		},
		{
			name: "not a diff",
			text: "```go\npackage main\n```",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blocks := ExtractCodeBlocks(tt.text)
			if len(blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(blocks))
			}
			if blocks[0].IsDiff != tt.want {
				t.Errorf("IsDiff = %v, want %v", blocks[0].IsDiff, tt.want)
			}
		})
	}
}

func TestExtractCodeBlocks_Empty(t *testing.T) {
	t.Parallel()

	blocks := ExtractCodeBlocks("no fences here")
	if len(blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(blocks))
	}
}

func TestExtractCodeBlocks_TildesFence(t *testing.T) {
	t.Parallel()

	text := "~~~go\nfunc foo() {}\n~~~"
	blocks := ExtractCodeBlocks(text)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Lang != "go" {
		t.Errorf("Lang = %q, want %q", blocks[0].Lang, "go")
	}
	if !strings.Contains(blocks[0].Content, "func foo()") {
		t.Errorf("Content does not contain expected text: %q", blocks[0].Content)
	}
}

func TestExtractCodeBlocks_InfoStringPathOnly(t *testing.T) {
	t.Parallel()

	// No lang token, just a path in the info string.
	text := "``` internal/repl/foo.go\npackage repl\n```"
	blocks := ExtractCodeBlocks(text)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].FilePath != "internal/repl/foo.go" {
		t.Errorf("FilePath = %q, want %q", blocks[0].FilePath, "internal/repl/foo.go")
	}
}

func TestApplyCodeBlock_WritesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "out.go")
	block := CodeBlock{
		Content: "package main\n",
	}

	if err := ApplyCodeBlock(block, path); err != nil {
		t.Fatalf("ApplyCodeBlock returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != block.Content {
		t.Errorf("file content = %q, want %q", string(got), block.Content)
	}
}

func TestApplyCodeBlock_CreatesParentDirs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "file.go")
	block := CodeBlock{
		Content: "package c\n",
	}

	if err := ApplyCodeBlock(block, path); err != nil {
		t.Fatalf("ApplyCodeBlock returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != block.Content {
		t.Errorf("file content = %q, want %q", string(got), block.Content)
	}
}

func TestREPL_LastAssistantText_Empty(t *testing.T) {
	t.Parallel()

	r := &REPL{}
	if got := r.lastAssistantText(); got != "" {
		t.Errorf("lastAssistantText() = %q, want empty string", got)
	}
}

func TestREPL_LastAssistantText_Found(t *testing.T) {
	t.Parallel()

	r := &REPL{
		turnBuffer: []ConversationTurn{
			{Role: "user", Text: "hello", At: time.Now()},
			{Role: "assistant", Text: "world", At: time.Now()},
			{Role: "user", Text: "more", At: time.Now()},
		},
	}

	// No assistant turn after the last user turn — should still return "world".
	if got := r.lastAssistantText(); got != "world" {
		t.Errorf("lastAssistantText() = %q, want %q", got, "world")
	}
}

func TestREPL_LastAssistantText_ReturnsLast(t *testing.T) {
	t.Parallel()

	r := &REPL{
		turnBuffer: []ConversationTurn{
			{Role: "assistant", Text: "first", At: time.Now()},
			{Role: "user", Text: "question", At: time.Now()},
			{Role: "assistant", Text: "second", At: time.Now()},
		},
	}

	if got := r.lastAssistantText(); got != "second" {
		t.Errorf("lastAssistantText() = %q, want %q", got, "second")
	}
}
