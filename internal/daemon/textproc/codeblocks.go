package textproc

import (
	"strings"
)

// CodeBlock is a fenced code block extracted from agent output. The
// shape is deliberately small: callers (apply.extract over JSON-RPC,
// `milliwaysctl apply`) need a language tag, an optional filename, and
// the content. Diff-detection / index numbering is left to the legacy
// REPL where /apply still lives.
type CodeBlock struct {
	Language string `json:"language,omitempty"`
	Filename string `json:"filename,omitempty"` // from "```python title=foo.py" or similar
	Content  string `json:"content"`
}

// ExtractCodeBlocks returns every fenced code block (``` or ~~~) found
// in text. Supports info-strings of the form
// `language [filename] [key=value...]`, where:
//   - the first whitespace-separated token is treated as the language;
//   - any token containing `/` or a non-leading `.` is treated as a
//     filename;
//   - `filename=...` and `title=...` keys also populate Filename.
//
// The parser is deliberately forgiving: malformed fences are skipped
// and unknown info-string keys are ignored.
func ExtractCodeBlocks(text string) []CodeBlock {
	lines := strings.Split(text, "\n")
	var blocks []CodeBlock

	i := 0
	for i < len(lines) {
		fence, info, ok := parseFenceOpen(strings.TrimSpace(lines[i]))
		if !ok {
			i++
			continue
		}
		i++

		var contentLines []string
		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == fence {
				i++
				break
			}
			contentLines = append(contentLines, lines[i])
			i++
		}

		content := strings.TrimRight(strings.Join(contentLines, "\n"), "\n")
		lang, filename := parseInfoString(info)

		blocks = append(blocks, CodeBlock{
			Language: lang,
			Filename: filename,
			Content:  content,
		})
	}

	return blocks
}

// parseFenceOpen detects a fence opening line and returns the matching
// closing-fence delimiter, the info-string, and ok=true on success.
func parseFenceOpen(line string) (fence, info string, ok bool) {
	for _, ch := range []string{"```", "~~~"} {
		if strings.HasPrefix(line, ch) {
			return ch, strings.TrimSpace(strings.TrimPrefix(line, ch)), true
		}
	}
	return "", "", false
}

// parseInfoString splits the info-string into a language token and an
// optional filename. Examples:
//
//	"go internal/foo/bar.go"           → "go", "internal/foo/bar.go"
//	"python title=script.py"           → "python", "script.py"
//	"go filename=cmd/main.go"          → "go", "cmd/main.go"
//	"main.go"                          → "", "main.go"
//	""                                 → "", ""
func parseInfoString(info string) (lang, filename string) {
	if info == "" {
		return "", ""
	}
	parts := strings.Fields(info)

	first := parts[0]
	if looksLikePath(first) {
		filename = first
	} else {
		lang = first
	}

	for _, p := range parts[1:] {
		switch {
		case strings.HasPrefix(p, "filename="):
			filename = strings.TrimPrefix(p, "filename=")
		case strings.HasPrefix(p, "title="):
			filename = strings.TrimPrefix(p, "title=")
		case filename == "" && looksLikePath(p):
			filename = p
		}
	}
	return lang, filename
}

// looksLikePath returns true if s contains '/' or has a dot that is not
// at position 0 — heuristic match for "looks like a relative path".
func looksLikePath(s string) bool {
	if strings.Contains(s, "/") {
		return true
	}
	if idx := strings.Index(s, "."); idx > 0 {
		return true
	}
	return false
}
