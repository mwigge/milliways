package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHandleMouseTracksSelection(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.renderedLines = []string{"line0", "line1", "line2"}

	m.handleMouse(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !m.mouse.selecting {
		t.Fatal("selection should start on mouse press")
	}

	m.handleMouse(tea.MouseMsg{X: 3, Y: 2, Action: tea.MouseActionMotion})
	if m.mouse.selEndRow != 2 || m.mouse.selEndCol != 3 {
		t.Fatalf("selEnd = (%d,%d), want (2,3)", m.mouse.selEndRow, m.mouse.selEndCol)
	}
}

func TestExtractTextSelection(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.renderedLines = []string{"hello", "world"}

	if got := m.extractTextSelection(0, 0, 0, 3); got != "hel" {
		t.Fatalf("single-line selection = %q, want %q", got, "hel")
	}

	if got := m.extractTextSelection(0, 0, 1, 3); got != "hello\nwor" {
		t.Fatalf("multi-line selection = %q, want %q", got, "hello\nwor")
	}
}

func TestBuildRenderedLinesSplitsMultilineOutput(t *testing.T) {
	t.Parallel()

	blocks := []Block{{Lines: []OutputLine{{Text: "alpha\nbeta"}, {Text: "gamma"}}}}

	got := buildRenderedLines(blocks)
	if len(got) != 3 {
		t.Fatalf("len(renderedLines) = %d, want 3", len(got))
	}
	if got[0] != "alpha" || got[1] != "beta" || got[2] != "gamma" {
		t.Fatalf("renderedLines = %#v, want [alpha beta gamma]", got)
	}
}
