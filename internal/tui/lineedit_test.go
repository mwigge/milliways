package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHandleKeyCtrlUClearsInputInInsertMode(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.input.SetValue("hello world")
	m.vimMode = VimInsert

	cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	if len(cmds) != 0 {
		t.Fatalf("expected no commands, got %d", len(cmds))
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("input = %q, want empty string", got)
	}
}

func TestHandleKeyCtrlAMovesCursorToBeginning(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.input.SetValue("hello world")
	m.input.SetCursor(5)
	m.vimMode = VimInsert

	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if got := m.input.Value(); got != "Xhello world" {
		t.Fatalf("input = %q, want %q", got, "Xhello world")
	}
}

func TestHandleKeyCtrlEMovesCursorToEnd(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.input.SetValue("hello")
	m.vimMode = VimInsert

	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlE})
	m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if got := m.input.Value(); got != "helloX" {
		t.Fatalf("input = %q, want %q", got, "helloX")
	}
}

func TestHandleKeyEscTogglesVimModes(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)

	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.vimMode; got != VimNormal {
		t.Fatalf("vimMode = %v, want %v", got, VimNormal)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.vimMode; got != VimInsert {
		t.Fatalf("vimMode = %v, want %v", got, VimInsert)
	}
}
