package repl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CodeBlock is a fenced code block extracted from markdown text.
type CodeBlock struct {
	Index    int
	Lang     string // "go", "python", "", etc.
	FilePath string // inferred from fence info string or first-line comment; empty if unknown
	Content  string // text inside the fence, trimmed
	IsDiff   bool   // true if lang == "diff" or content looks like a unified diff
}

// ExtractCodeBlocks parses all fenced code blocks (``` or ~~~) from markdown text.
// It infers FilePath from the info string after the language token
// (e.g. ```go path/to/file.go) or from a leading comment in the content
// (// file: path/to/file.go, # file: path/to/file.go).
func ExtractCodeBlocks(text string) []CodeBlock {
	lines := strings.Split(text, "\n")
	var blocks []CodeBlock
	index := 0

	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		fence, info, ok := parseFenceOpen(trimmed)
		if !ok {
			i++
			continue
		}

		// Collect content lines until we find the closing fence.
		i++
		var contentLines []string
		for i < len(lines) {
			cl := lines[i]
			if strings.TrimSpace(cl) == fence {
				i++ // consume closing fence
				break
			}
			contentLines = append(contentLines, cl)
			i++
		}

		content := strings.Join(contentLines, "\n")
		content = strings.TrimRight(content, "\n")

		lang, filePath := parseInfoString(info)
		if filePath == "" {
			filePath = inferPathFromContent(content)
		}

		isDiff := lang == "diff" || looksLikeDiff(content)

		blocks = append(blocks, CodeBlock{
			Index:    index,
			Lang:     lang,
			FilePath: filePath,
			Content:  content,
			IsDiff:   isDiff,
		})
		index++
	}

	return blocks
}

// parseFenceOpen detects a fence opening line.
// Returns (fenceDelimiter, infoString, true) on success, ("", "", false) otherwise.
// The fenceDelimiter is the minimal fence token ("```" or "~~~") used to close the block.
func parseFenceOpen(line string) (fence, info string, ok bool) {
	for _, ch := range []string{"```", "~~~"} {
		if strings.HasPrefix(line, ch) {
			rest := strings.TrimPrefix(line, ch)
			// The rest must not start with the same character (extended fence).
			// Simple rule: the fence token is ch itself.
			return ch, strings.TrimSpace(rest), true
		}
	}
	return "", "", false
}

// parseInfoString splits the info string into a language token and an optional file path.
// Examples:
//
//	"go internal/repl/foo.go"  → "go", "internal/repl/foo.go"
//	"go"                       → "go", ""
//	"internal/repl/foo.go"     → "", "internal/repl/foo.go"  (looks like a path)
//	""                         → "", ""
func parseInfoString(info string) (lang, filePath string) {
	if info == "" {
		return "", ""
	}
	parts := strings.Fields(info)
	first := parts[0]

	// Heuristic: if the first token contains a dot or slash it is likely a path.
	if looksLikePath(first) {
		return "", first
	}
	lang = first
	if len(parts) >= 2 {
		filePath = parts[1]
	}
	return lang, filePath
}

// looksLikePath returns true if the token resembles a file path
// (contains '/' or a dot that is not the first character).
func looksLikePath(s string) bool {
	if strings.Contains(s, "/") {
		return true
	}
	// token like "main.go" — dot not at position 0
	if idx := strings.Index(s, "."); idx > 0 {
		return true
	}
	return false
}

// inferPathFromContent looks at the first line of the content for a path hint.
// Supported formats:
//
//	// file: path/to/file.go
//	# file: path/to/file.go
func inferPathFromContent(content string) string {
	lines := strings.SplitN(content, "\n", 2)
	if len(lines) == 0 {
		return ""
	}
	first := strings.TrimSpace(lines[0])

	for _, prefix := range []string{"// file:", "# file:"} {
		if strings.HasPrefix(first, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(first, prefix))
		}
	}
	return ""
}

// looksLikeDiff returns true when the content starts with "---" and contains "+++".
func looksLikeDiff(content string) bool {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return false
	}
	return strings.Contains(content, "+++")
}

// ApplyCodeBlock writes block.Content to path.
// It creates parent directories as needed and overwrites any existing file.
func ApplyCodeBlock(block CodeBlock, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directories for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(block.Content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
