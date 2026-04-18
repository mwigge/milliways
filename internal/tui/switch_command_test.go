package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func TestHandleKey_EnterExecutesSwitchPaletteCommandWithArgument(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.kitchenStates = []KitchenState{{Name: "claude", Status: "ready"}}
	m.overlayActive = true
	m.overlayMode = OverlayPalette
	m.palette = PaletteState{Matches: FilterPalette("switch claude")}
	m.overlayInput = textinput.New()
	m.overlayInput.SetValue("switch claude")

	cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(cmds) != 0 {
		t.Fatalf("expected no dispatch command, got %d", len(cmds))
	}
	if m.overlayActive {
		t.Fatal("palette overlay should close after command execution")
	}
	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d", len(m.blocks))
	}
	if m.blocks[0].Prompt != "/switch claude" {
		t.Fatalf("block prompt = %q", m.blocks[0].Prompt)
	}
	if got := m.blocks[0].Lines[0].Text; !strings.Contains(got, "accepted") {
		t.Fatalf("switch confirmation = %q", got)
	}
}

func TestExecutePaletteCommand_SwitchUnavailableKitchenListsReadyKitchens(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.kitchenStates = []KitchenState{
		{Name: "claude", Status: "exhausted", ResetsAt: "22:00"},
		{Name: "gpt", Status: "ready"},
	}

	m.executePaletteCommand("switch claude")

	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d", len(m.blocks))
	}
	got := m.blocks[0].Lines[0].Text
	if !strings.Contains(got, "unavailable") {
		t.Fatalf("switch error = %q, want unavailable message", got)
	}
	if !strings.Contains(got, "gpt") {
		t.Fatalf("switch error = %q, want ready kitchen list", got)
	}
	if !strings.Contains(got, "22:00") {
		t.Fatalf("switch error = %q, want reset time", got)
	}
}

func TestExecutePaletteCommand_KitchensListsStatuses(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.kitchenStates = []KitchenState{
		{Name: "claude", Status: "ready"},
		{Name: "gpt", Status: "warning", UsageRatio: 0.85},
		{Name: "copilot", Status: "exhausted", ResetsAt: "22:00"},
	}

	m.executePaletteCommand("kitchens")

	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d", len(m.blocks))
	}
	got := m.blocks[0].Lines[0].Text
	for _, want := range []string{"claude [ready]", "gpt [warning 85%]", "copilot [exhausted until 22:00]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("kitchens output = %q, want substring %q", got, want)
		}
	}
}
