package tui

import (
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

type mouseState struct {
	selecting   bool
	selStartRow int
	selStartCol int
	selEndRow   int
	selEndCol   int
}

// screenRowToRenderedIdx translates an absolute screen row to a renderedLines index.
// Returns -1 if the screen row is not a content line (e.g., header, border, separator).
func (m *Model) screenRowToRenderedIdx(screenRow int) int {
	if m.screenLineMap == nil || screenRow < 0 {
		return -1
	}
	if screenRow >= len(m.screenLineMap) {
		return -1
	}
	return m.screenLineMap[screenRow]
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch {
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		// Check if click is on a collapsed block header → expand it.
		if m.collapsedHeaders != nil {
			if block, ok := m.collapsedHeaders[msg.Y]; ok {
				block.ToggleCollapse()
				return nil
			}
		}
		// Normal content click: start selection.
		m.mouse.selecting = true
		m.mouse.selStartRow = m.screenRowToRenderedIdx(msg.Y)
		m.mouse.selStartCol = msg.X
		m.mouse.selEndRow = m.mouse.selStartRow
		m.mouse.selEndCol = msg.X
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionRelease:
		if !m.mouse.selecting {
			return nil
		}
		m.mouse.selecting = false
		text := m.extractTextSelection(m.mouse.selStartRow, m.mouse.selStartCol, m.mouse.selEndRow, m.mouse.selEndCol)
		if text != "" {
			_ = clipboard.WriteAll(text)
		}
	case msg.Action == tea.MouseActionMotion || msg.Type == tea.MouseMotion:
		if !m.mouse.selecting {
			return nil
		}
		m.mouse.selEndRow = m.screenRowToRenderedIdx(msg.Y)
		m.mouse.selEndCol = msg.X
	}

	return nil
}

func (m *Model) extractTextSelection(r1, c1, r2, c2 int) string {
	if len(m.renderedLines) == 0 {
		return ""
	}
	// r1/r2 are renderedLines indices; -1 means non-content row (header/border).
	if r1 < 0 || r2 < 0 {
		return ""
	}
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, r2 = r2, r1
		c1, c2 = c2, c1
	}
	if r1 >= len(m.renderedLines) {
		r1 = len(m.renderedLines) - 1
	}
	if r2 >= len(m.renderedLines) {
		r2 = len(m.renderedLines) - 1
	}
	if r1 > r2 {
		return ""
	}

	if r1 == r2 {
		line := m.renderedLines[r1]
		c1 = clampColumn(c1, len(line))
		c2 = clampColumn(c2, len(line))
		if c2 < c1 {
			c1, c2 = c2, c1
		}
		return line[c1:c2]
	}

	lines := make([]string, 0, r2-r1+1)
	for row := r1; row <= r2; row++ {
		line := m.renderedLines[row]
		switch row {
		case r1:
			line = line[clampColumn(c1, len(line)):]
		case r2:
			line = line[:clampColumn(c2, len(line))]
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func buildRenderedLines(blocks []Block) []string {
	lines := make([]string, 0)
	for _, block := range blocks {
		for _, line := range block.Lines {
			lines = append(lines, strings.Split(line.Text, "\n")...)
		}
	}
	return lines
}

func clampColumn(col, length int) int {
	if col < 0 {
		return 0
	}
	if col > length {
		return length
	}
	return col
}
