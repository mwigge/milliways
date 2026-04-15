package tui

import (
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/glamour"
)

// highlightCode applies syntax highlighting to a code string using chroma.
// Returns the raw code if highlighting fails or language is empty.
func highlightCode(code, language string) string {
	if language == "" {
		return code
	}

	var buf strings.Builder
	err := quick.Highlight(&buf, code, language, "terminal256", "monokai")
	if err != nil {
		return code
	}

	// Trim trailing newline that chroma sometimes adds
	result := buf.String()
	result = strings.TrimRight(result, "\n")
	return result
}

// renderGlamour renders markdown text through glamour.
// Returns the raw text if rendering fails.
func renderGlamour(text string, width int) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return text
	}

	out, err := r.Render(text)
	if err != nil {
		return text
	}

	return strings.TrimRight(out, "\n")
}
