package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mwigge/milliways/internal/conversation"
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
	if got := m.blocks[0].Lines[0].Text; !strings.Contains(got, "cannot switch") {
		t.Fatalf("switch confirmation = %q", got)
	}
}

func TestHandleSwitchCommand_TransitionsConversationAndRendersSwitchState(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.kitchenStates = []KitchenState{{Name: "gpt", Status: "ready"}}

	conv := conversation.New("conv-1", "b1", "finish the task")
	conv.AppendTurn(conversation.RoleAssistant, "claude", "working on it")
	ended := conv.StartSegment("claude")

	m.blocks = []Block{{
		ID:             "b1",
		ConversationID: conv.ID,
		Prompt:         "finish the task",
		Kitchen:        "claude",
		ProviderChain:  []string{"claude"},
		State:          StateStreaming,
		StartedAt:      conv.CreatedAt,
		Conversation:   conv,
	}}
	m.focusedIdx = 0

	m.handleSwitchCommand("gpt")

	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d", len(m.blocks))
	}
	b := m.blocks[0]
	if b.Kitchen != "gpt" {
		t.Fatalf("kitchen = %q, want gpt", b.Kitchen)
	}
	if !containsProvider(b.ProviderChain, "gpt") {
		t.Fatalf("provider chain = %v, want gpt", b.ProviderChain)
	}
	if b.ContinuationPrompt == "" {
		t.Fatal("continuation prompt should be stored on block")
	}
	if !strings.Contains(b.ContinuationPrompt, "Why you are taking over:\nuser requested") {
		t.Fatalf("continuation prompt = %q", b.ContinuationPrompt)
	}
	if !strings.Contains(b.ContinuationPrompt, "Continue from the current state in gpt.") {
		t.Fatalf("continuation prompt = %q", b.ContinuationPrompt)
	}
	if len(b.Lines) == 0 || !strings.Contains(b.Lines[len(b.Lines)-1].Text, "switch executed") {
		t.Fatalf("last line = %+v, want switch execution confirmation", b.Lines)
	}
	rendered := b.RenderBody(80, RenderRaw)
	if !strings.Contains(rendered, "[milliways] switch executed") {
		t.Fatalf("rendered body = %q", rendered)
	}

	if len(b.Conversation.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(b.Conversation.Segments))
	}
	if b.Conversation.Segments[0].ID != ended.ID {
		t.Fatalf("ended segment ID = %q, want %q", b.Conversation.Segments[0].ID, ended.ID)
	}
	if b.Conversation.Segments[0].Status != conversation.SegmentDone {
		t.Fatalf("ended segment status = %q", b.Conversation.Segments[0].Status)
	}
	if b.Conversation.Segments[0].EndReason != "user_switch" {
		t.Fatalf("ended segment reason = %q", b.Conversation.Segments[0].EndReason)
	}
	active := b.Conversation.ActiveSegment()
	if active == nil {
		t.Fatal("expected active segment after switch")
	}
	if active.Provider != "gpt" {
		t.Fatalf("active provider = %q, want gpt", active.Provider)
	}
	lastTurn := b.Conversation.Transcript[len(b.Conversation.Transcript)-1]
	if lastTurn.Role != conversation.RoleSystem {
		t.Fatalf("last turn role = %q, want system", lastTurn.Role)
	}
	if !strings.Contains(lastTurn.Text, "Prepared continuation payload") {
		t.Fatalf("last turn text = %q", lastTurn.Text)
	}

	if len(m.runtimeEvents) != 1 {
		t.Fatalf("runtime events = %d, want 1", len(m.runtimeEvents))
	}
	evt := m.runtimeEvents[0]
	if evt.Kind != "switch" {
		t.Fatalf("event kind = %q", evt.Kind)
	}
	if evt.Provider != "gpt" {
		t.Fatalf("event provider = %q", evt.Provider)
	}
	if evt.ConversationID != conv.ID || evt.BlockID != "b1" {
		t.Fatalf("event ids = %+v", evt)
	}
	if evt.SegmentID != active.ID {
		t.Fatalf("event segment ID = %q, want %q", evt.SegmentID, active.ID)
	}
	for key, want := range map[string]string{"from": "claude", "to": "gpt", "reason": "user requested"} {
		if got := evt.Fields[key]; got != want {
			t.Fatalf("event field %q = %q, want %q", key, got, want)
		}
	}
	activity := m.runtimeActivityLines(6)
	if len(activity) != 1 || !strings.Contains(activity[0], "switch") || !strings.Contains(activity[0], "gpt") {
		t.Fatalf("activity lines = %#v", activity)
	}
}

func TestHandleSwitchCommand_MissingConversationStateShowsHelpfulMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(*Model)
		wantBlock bool
	}{
		{
			name:      "no focused block",
			setup:     func(*Model) {},
			wantBlock: true,
		},
		{
			name: "no conversation",
			setup: func(m *Model) {
				m.blocks = []Block{{ID: "b1", Prompt: "task", Kitchen: "claude", State: StateStreaming}}
				m.focusedIdx = 0
			},
		},
		{
			name: "no active segment",
			setup: func(m *Model) {
				conv := conversation.New("conv-1", "b1", "task")
				m.blocks = []Block{{ID: "b1", ConversationID: conv.ID, Prompt: "task", Kitchen: "claude", State: StateStreaming, Conversation: conv}}
				m.focusedIdx = 0
			},
		},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := NewModel(nil)
			m.kitchenStates = []KitchenState{{Name: "gpt", Status: "ready"}}
			tc.setup(&m)

			m.handleSwitchCommand("gpt")

			if len(m.runtimeEvents) != 0 {
				t.Fatalf("runtime events = %d, want 0", len(m.runtimeEvents))
			}

			if tc.wantBlock {
				if len(m.blocks) != 1 {
					t.Fatalf("blocks = %d, want 1", len(m.blocks))
				}
			}

			b := m.blocks[m.focusedIdx]
			if len(b.Lines) == 0 || !strings.Contains(b.Lines[len(b.Lines)-1].Text, "cannot switch") {
				t.Fatalf("last line = %+v, want cannot switch message", b.Lines)
			}
		})
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
