package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- SearchReplaceParser tests ----

func TestSearchReplaceParser_Parse_SingleBlock(t *testing.T) {
	t.Parallel()

	content := `path/to/file.go
<<<<<<< SEARCH
func oldCode() {
    // old implementation
}
=======
func oldCode() {
    // new implementation
}
>>>>>>> REPLACE`

	p := NewEditParser()
	blocks, err := p.Parse("/repo", content)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("Parse() = %d blocks, want 1", len(blocks))
	}
	b := blocks[0]
	if b.FilePath != "path/to/file.go" {
		t.Errorf("FilePath = %q, want %q", b.FilePath, "path/to/file.go")
	}
	if !strings.Contains(b.Search, "old implementation") {
		t.Errorf("Search = %q, want to contain 'old implementation'", b.Search)
	}
	if !strings.Contains(b.Replace, "new implementation") {
		t.Errorf("Replace = %q, want to contain 'new implementation'", b.Replace)
	}
	if b.IsDiff {
		t.Errorf("IsDiff = true, want false for search/replace block")
	}
}

func TestSearchReplaceParser_Parse_MultipleBlocks(t *testing.T) {
	t.Parallel()

	content := `first/file.go
<<<<<<< SEARCH
func alpha() {}
=======
func alpha() { return }
>>>>>>> REPLACE

second/file.go
<<<<<<< SEARCH
func beta() {}
=======
func beta() { return }
>>>>>>> REPLACE`

	p := NewEditParser()
	blocks, err := p.Parse("/repo", content)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("Parse() = %d blocks, want 2", len(blocks))
	}
	if blocks[0].FilePath != "first/file.go" {
		t.Errorf("blocks[0].FilePath = %q, want %q", blocks[0].FilePath, "first/file.go")
	}
	if blocks[1].FilePath != "second/file.go" {
		t.Errorf("blocks[1].FilePath = %q, want %q", blocks[1].FilePath, "second/file.go")
	}
}

func TestSearchReplaceParser_Parse_NoBlocks(t *testing.T) {
	t.Parallel()

	content := `This is a prose response with no code changes.
It just describes what should be done without any edit blocks.`

	p := NewEditParser()
	blocks, err := p.Parse("/repo", content)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if len(blocks) != 0 {
		t.Errorf("Parse() = %d blocks, want 0", len(blocks))
	}
}

func TestSearchReplaceParser_Parse_ExtractsFilePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		wantPath string
	}{
		{
			name: "bare path",
			content: "internal/pkg/foo.go\n<<<<<<< SEARCH\nold\n=======\nnew\n>>>>>>> REPLACE",
			wantPath: "internal/pkg/foo.go",
		},
		{
			name: "backtick quoted path",
			content: "`internal/pkg/bar.go`\n<<<<<<< SEARCH\nold\n=======\nnew\n>>>>>>> REPLACE",
			wantPath: "internal/pkg/bar.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewEditParser()
			blocks, err := p.Parse("/repo", tt.content)
			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}
			if len(blocks) != 1 {
				t.Fatalf("Parse() = %d blocks, want 1", len(blocks))
			}
			if blocks[0].FilePath != tt.wantPath {
				t.Errorf("FilePath = %q, want %q", blocks[0].FilePath, tt.wantPath)
			}
		})
	}
}

func TestSearchReplaceParser_Parse_MalformedBlock(t *testing.T) {
	t.Parallel()

	// SEARCH marker with no ======= separator
	content := `path/to/file.go
<<<<<<< SEARCH
func oldCode() {}
>>>>>>> REPLACE`

	p := NewEditParser()
	_, err := p.Parse("/repo", content)
	if err == nil {
		t.Fatal("Parse() expected error for malformed block, got nil")
	}
}

func TestSearchReplaceParser_Parse_NoPrecedingFilePath(t *testing.T) {
	t.Parallel()

	// Block with no file path line before it — should still parse, FilePath empty
	content := `<<<<<<< SEARCH
old content
=======
new content
>>>>>>> REPLACE`

	p := NewEditParser()
	blocks, err := p.Parse("/repo", content)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("Parse() = %d blocks, want 1", len(blocks))
	}
	if blocks[0].FilePath != "" {
		t.Errorf("FilePath = %q, want empty when no preceding path line", blocks[0].FilePath)
	}
}

func TestSearchReplaceParser_Parse_UnifiedDiff(t *testing.T) {
	t.Parallel()

	content := `--- a/path/to/file.go
+++ b/path/to/file.go
@@ -10,7 +10,7 @@
-    oldLine
+    newLine`

	p := NewEditParser()
	blocks, err := p.Parse("/repo", content)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("Parse() = %d blocks, want 1", len(blocks))
	}
	b := blocks[0]
	if !b.IsDiff {
		t.Errorf("IsDiff = false, want true for unified diff block")
	}
	if b.FilePath != "path/to/file.go" {
		t.Errorf("FilePath = %q, want %q", b.FilePath, "path/to/file.go")
	}
}

// ---- FSEditApplier tests ----

func TestFSEditApplier_Apply_ReplacesText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	original := "package main\n\nfunc oldFunc() {\n\treturn\n}\n"
	filePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(filePath, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	block := EditBlock{
		FilePath: filePath,
		Search:   "func oldFunc() {\n\treturn\n}",
		Replace:  "func newFunc() {\n\treturn\n}",
	}

	a := NewEditApplier(dir)
	modified, err := a.Apply(context.Background(), []EditBlock{block})
	if err != nil {
		t.Fatalf("Apply() unexpected error: %v", err)
	}
	if len(modified) != 1 {
		t.Fatalf("Apply() returned %d paths, want 1", len(modified))
	}

	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), "newFunc") {
		t.Errorf("file content = %q, want to contain 'newFunc'", string(got))
	}
	if strings.Contains(string(got), "oldFunc") {
		t.Errorf("file content = %q, want 'oldFunc' replaced", string(got))
	}
}

func TestFSEditApplier_Apply_SearchNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(filePath, []byte("package foo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	block := EditBlock{
		FilePath: filePath,
		Search:   "this text does not exist",
		Replace:  "something",
	}

	a := NewEditApplier(dir)
	_, err := a.Apply(context.Background(), []EditBlock{block})
	if err == nil {
		t.Fatal("Apply() expected error for missing search text, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestFSEditApplier_Apply_MultipleBlocks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	fileA := filepath.Join(dir, "a.go")
	fileB := filepath.Join(dir, "b.go")
	if err := os.WriteFile(fileA, []byte("package a\n\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile a.go: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("package b\n\nfunc Bar() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile b.go: %v", err)
	}

	blocks := []EditBlock{
		{FilePath: fileA, Search: "func Foo() {}", Replace: "func Foo() { return }"},
		{FilePath: fileB, Search: "func Bar() {}", Replace: "func Bar() { return }"},
	}

	a := NewEditApplier(dir)
	modified, err := a.Apply(context.Background(), blocks)
	if err != nil {
		t.Fatalf("Apply() unexpected error: %v", err)
	}
	if len(modified) != 2 {
		t.Fatalf("Apply() returned %d paths, want 2", len(modified))
	}

	gotA, _ := os.ReadFile(fileA)
	if !strings.Contains(string(gotA), "func Foo() { return }") {
		t.Errorf("a.go = %q, want updated Foo", string(gotA))
	}
	gotB, _ := os.ReadFile(fileB)
	if !strings.Contains(string(gotB), "func Bar() { return }") {
		t.Errorf("b.go = %q, want updated Bar", string(gotB))
	}
}

func TestFSEditApplier_Apply_ContextCancelled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "x.go")
	if err := os.WriteFile(filePath, []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	block := EditBlock{
		FilePath: filePath,
		Search:   "package x",
		Replace:  "package y",
	}
	a := NewEditApplier(dir)
	_, err := a.Apply(ctx, []EditBlock{block})
	if err == nil {
		t.Fatal("Apply() expected error for cancelled context, got nil")
	}
}
