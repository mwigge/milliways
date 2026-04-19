package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHandleMouseTracksSelection(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.renderedLines = []string{"line0", "line1", "line2"}
	// Simulate real screen layout:
	// Row 0: title (non-content, -1)
	// Row 1: project header (non-content, -1)
	// Rows 2-4: block body content → maps to renderedLines[0..2]
	m.screenLineMap = []int{-1, -1, 0, 1, 2}

	m.handleMouse(tea.MouseMsg{X: 0, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !m.mouse.selecting {
		t.Fatal("selection should start on mouse press")
	}
	if m.mouse.selStartRow != 0 {
		t.Fatalf("selStartRow = %d, want 0", m.mouse.selStartRow)
	}

	// Y=4 maps to renderedLine 2 (line2)
	m.handleMouse(tea.MouseMsg{X: 3, Y: 4, Action: tea.MouseActionMotion})
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
