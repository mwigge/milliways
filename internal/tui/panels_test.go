package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSidePanelNamesMatchPanelCount(t *testing.T) {
	t.Parallel()

	if got, want := len(sidePanelNames), int(sidePanelCount); got != want {
		t.Fatalf("len(sidePanelNames) = %d, want %d", got, want)
	}
}

func TestHandleKey_CyclesSidePanelsForward(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		key   tea.KeyMsg
		start int
		want  int
	}{
		{
			name:  "ctrl right arrow advances",
			key:   tea.KeyMsg{Type: tea.KeyCtrlRight},
			start: 0,
			want:  1,
		},
		{
			name:  "ctrl right arrow wraps",
			key:   tea.KeyMsg{Type: tea.KeyCtrlRight},
			start: int(sidePanelCount) - 1,
			want:  0,
		},
		{
			name:  "ctrl j advances",
			key:   tea.KeyMsg{Type: tea.KeyCtrlJ},
			start: 2,
			want:  3,
		},
		{
			name:  "ctrl j wraps",
			key:   tea.KeyMsg{Type: tea.KeyCtrlJ},
			start: int(sidePanelCount) - 1,
			want:  0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := NewModel(nil)
			m.sidePanelIdx = tt.start

			cmds := m.handleKey(tt.key)
			if len(cmds) != 0 {
				t.Fatalf("expected no commands, got %d", len(cmds))
			}
			if m.sidePanelIdx != tt.want {
				t.Fatalf("sidePanelIdx = %d, want %d", m.sidePanelIdx, tt.want)
			}
		})
	}
}

func TestHandleKey_CyclesSidePanelsBackward(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		key   tea.KeyMsg
		start int
		want  int
	}{
		{
			name:  "ctrl left arrow decrements",
			key:   tea.KeyMsg{Type: tea.KeyCtrlLeft},
			start: 3,
			want:  2,
		},
		{
			name:  "ctrl left arrow wraps",
			key:   tea.KeyMsg{Type: tea.KeyCtrlLeft},
			start: 0,
			want:  int(sidePanelCount) - 1,
		},
		{
			name:  "ctrl k decrements",
			key:   tea.KeyMsg{Type: tea.KeyCtrlK},
			start: 4,
			want:  3,
		},
		{
			name:  "ctrl k wraps",
			key:   tea.KeyMsg{Type: tea.KeyCtrlK},
			start: 0,
			want:  int(sidePanelCount) - 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := NewModel(nil)
			m.sidePanelIdx = tt.start

			cmds := m.handleKey(tt.key)
			if len(cmds) != 0 {
				t.Fatalf("expected no commands, got %d", len(cmds))
			}
			if m.sidePanelIdx != tt.want {
				t.Fatalf("sidePanelIdx = %d, want %d", m.sidePanelIdx, tt.want)
			}
		})
	}
}
