package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBuildRunTargetOptions(t *testing.T) {
	t.Parallel()

	options := buildRunTargetOptions([]KitchenState{
		{Name: "beta", Status: "exhausted", ResetsAt: "22:00", Remaining: -1, Trend: ""},
		{Name: "alpha", Status: "ready", Remaining: -1, Trend: ""},
		{Name: "gamma", Status: "warning", UsageRatio: 0.85, Remaining: -1, Trend: ""},
	})
	if len(options) != 4 {
		t.Fatalf("len(options) = %d", len(options))
	}
	if options[0].Label != "Auto" || !options[0].Selectable {
		t.Fatalf("auto option = %#v", options[0])
	}
	if options[1].Kitchen != "alpha" || !options[1].Selectable {
		t.Fatalf("alpha option = %#v", options[1])
	}
	if options[2].Kitchen != "beta" || !options[2].Selectable {
		t.Fatalf("beta option = %#v", options[2])
	}
	if options[3].Kitchen != "gamma" || !options[3].Selectable {
		t.Fatalf("gamma option = %#v", options[3])
	}
	if options[2].Reason == "" || options[2].Reason == "exhausted until 22:00" {
		t.Fatalf("expected override hint in exhausted reason, got %q", options[2].Reason)
	}
}

func TestHandleKey_EnterOpensRunTargetChooser(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.kitchenStates = []KitchenState{{Name: "alpha", Status: "ready", Remaining: -1, Trend: ""}}
	m.input.SetValue("ship it")

	cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(cmds) != 0 {
		t.Fatalf("expected no dispatch command yet, got %d", len(cmds))
	}
	if !m.overlayActive || m.overlayMode != OverlayRunIn {
		t.Fatalf("overlay = active:%t mode:%v", m.overlayActive, m.overlayMode)
	}
	if m.pendingPrompt != "ship it" {
		t.Fatalf("pendingPrompt = %q", m.pendingPrompt)
	}
	if len(m.runTargets) < 2 {
		t.Fatalf("runTargets = %#v", m.runTargets)
	}
}

func TestHandleRunTargetSelection_StartsDispatch(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.kitchenStates = []KitchenState{{Name: "alpha", Status: "ready", Remaining: -1, Trend: ""}}
	m.openRunTargetChooser("ship it")
	m.runTargetSelected = 1

	cmds := m.handleRunTargetSelection()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 dispatch command, got %d", len(cmds))
	}
	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d", len(m.blocks))
	}
	if m.blocks[0].Prompt != "ship it" {
		t.Fatalf("block prompt = %q", m.blocks[0].Prompt)
	}
	if m.blocks[0].Kitchen != "alpha" {
		t.Fatalf("block kitchen = %q", m.blocks[0].Kitchen)
	}
	if m.overlayActive {
		t.Fatal("overlay should close after selection")
	}
}
