package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/orchestrator"
	"github.com/mwigge/milliways/internal/sommelier"
)

func TestHandleKey_EnterExecutesSwitchPaletteCommandWithArgument(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.kitchenStates = []KitchenState{{Name: "claude", Status: "ready", Remaining: -1, Trend: ""}}
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

func TestHandleKey_EnterExecutesStickPaletteCommand(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	conv := conversation.New("conv-1", "b1", "finish the task")
	m.blocks = []Block{{
		ID:             "b1",
		ConversationID: conv.ID,
		Prompt:         "finish the task",
		Kitchen:        "claude",
		State:          StateStreaming,
		StartedAt:      conv.CreatedAt,
		Conversation:   conv,
	}}
	m.focusedIdx = 0
	m.overlayActive = true
	m.overlayMode = OverlayPalette
	m.overlayInput = textinput.New()
	m.overlayInput.SetValue("stick")

	cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(cmds) != 0 {
		t.Fatalf("expected no dispatch command, got %d", len(cmds))
	}
	if m.overlayActive {
		t.Fatal("palette overlay should close after command execution")
	}
	if got := m.blocks[0].Conversation.Memory.StickyKitchen; got != "claude" {
		t.Fatalf("StickyKitchen = %q, want claude", got)
	}
	if got := m.blocks[0].Lines[len(m.blocks[0].Lines)-1].Text; !strings.Contains(got, "sticky mode enabled") {
		t.Fatalf("last line = %q, want sticky enabled message", got)
	}
}

func TestHandleSwitchCommand_TransitionsConversationAndRendersSwitchState(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.kitchenStates = []KitchenState{{Name: "gpt", Status: "ready", Remaining: -1, Trend: ""}}

	conv := conversation.New("conv-1", "b1", "finish the task")
	conv.AppendTurn(conversation.RoleAssistant, "claude", "working on it")
	ended := conv.StartSegment("claude", nil)

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
	if len(b.Lines) == 0 {
		t.Fatalf("lines = %+v, want switch execution confirmation", b.Lines)
	}
	lastLine := b.Lines[len(b.Lines)-1].Text
	for _, want := range []string{"switch: claude -> gpt", "reason: user requested", "Use /back to return"} {
		if !strings.Contains(lastLine, want) {
			t.Fatalf("last line = %q, want substring %q", lastLine, want)
		}
	}
	rendered := b.RenderBody(80, RenderRaw)
	for _, want := range []string{"[milliways] switch: claude -> gpt", "reason: user requested", "Use /back to return"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered body = %q, want substring %q", rendered, want)
		}
	}

	activity := m.runtimeActivityLines(6)
	if len(activity) != 1 {
		t.Fatalf("activity lines = %#v, want one line", activity)
	}
	for _, want := range []string{"switch", "claude → gpt", "user requested"} {
		if !strings.Contains(activity[0], want) {
			t.Fatalf("activity line = %q, want substring %q", activity[0], want)
		}
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
}

func TestHandleStickCommand_TogglesConversationWorkingMemory(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	conv := conversation.New("conv-1", "b1", "finish the task")
	m.blocks = []Block{{
		ID:             "b1",
		ConversationID: conv.ID,
		Prompt:         "finish the task",
		Kitchen:        "claude",
		State:          StateStreaming,
		StartedAt:      conv.CreatedAt,
		Conversation:   conv,
	}}
	m.focusedIdx = 0

	m.executePaletteCommand("stick")

	b := m.blocks[0]
	if got := b.Conversation.Memory.StickyKitchen; got != "claude" {
		t.Fatalf("StickyKitchen = %q, want claude", got)
	}
	if got := b.Lines[len(b.Lines)-1].Text; !strings.Contains(got, "sticky mode enabled for kitchen \"claude\"") {
		t.Fatalf("last line = %q, want sticky enabled message", got)
	}

	m.executePaletteCommand("stick")

	b = m.blocks[0]
	if got := b.Conversation.Memory.StickyKitchen; got != "" {
		t.Fatalf("StickyKitchen = %q, want empty", got)
	}
	if got := b.Lines[len(b.Lines)-1].Text; !strings.Contains(got, "sticky mode off") {
		t.Fatalf("last line = %q, want sticky disabled message", got)
	}
}

func TestHandleStickCommand_MissingConversationStateShowsHelpfulMessage(t *testing.T) {
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
			name: "no kitchen",
			setup: func(m *Model) {
				conv := conversation.New("conv-1", "b1", "task")
				m.blocks = []Block{{ID: "b1", ConversationID: conv.ID, Prompt: "task", State: StateStreaming, Conversation: conv}}
				m.focusedIdx = 0
			},
		},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := NewModel(nil)
			tc.setup(&m)

			m.executePaletteCommand("stick")

			if tc.wantBlock {
				if len(m.blocks) != 1 {
					t.Fatalf("blocks = %d, want 1", len(m.blocks))
				}
			}

			b := m.blocks[m.focusedIdx]
			if got := b.Lines[len(b.Lines)-1].Text; !strings.Contains(got, "cannot toggle sticky mode") {
				t.Fatalf("last line = %q, want sticky error message", got)
			}
		})
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
			m.kitchenStates = []KitchenState{{Name: "gpt", Status: "ready", Remaining: -1, Trend: ""}}
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
		{Name: "claude", Status: "exhausted", ResetsAt: "22:00", Remaining: -1, Trend: ""},
		{Name: "gpt", Status: "ready", Remaining: -1, Trend: ""},
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
		{Name: "claude", Status: "ready", Remaining: -1, Trend: ""},
		{Name: "gpt", Status: "warning", UsageRatio: 0.85, Remaining: -1, Trend: ""},
		{Name: "copilot", Status: "exhausted", ResetsAt: "22:00", Remaining: -1, Trend: ""},
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

func TestExecutePaletteCommand_BackReversesMostRecentSwitch(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.kitchenStates = []KitchenState{{Name: "claude", Status: "ready", Remaining: -1, Trend: ""}, {Name: "gpt", Status: "ready", Remaining: -1, Trend: ""}}

	conv := conversation.New("conv-1", "b1", "finish the task")
	conv.AppendTurn(conversation.RoleAssistant, "claude", "working on it")
	conv.StartSegment("claude", nil)
	conv.EndActiveSegment(conversation.SegmentDone, "user_switch")
	switchedSegment := conv.StartSegment("gpt", nil)

	m.blocks = []Block{{
		ID:             "b1",
		ConversationID: conv.ID,
		Prompt:         "finish the task",
		Kitchen:        "gpt",
		ProviderChain:  []string{"claude", "gpt"},
		State:          StateStreaming,
		StartedAt:      conv.CreatedAt,
		Conversation:   conv,
	}}
	m.focusedIdx = 0
	m.runtimeEvents = []observability.Event{{
		ID:             "switch-b1-1",
		ConversationID: conv.ID,
		BlockID:        "b1",
		SegmentID:      switchedSegment.ID,
		Kind:           "switch",
		Provider:       "gpt",
		Text:           "switch claude -> gpt (user requested)",
		At:             time.Now().Add(-time.Minute),
		Fields: map[string]string{
			"from":   "claude",
			"to":     "gpt",
			"reason": "user requested",
		},
	}}

	m.executePaletteCommand("back")

	b := m.blocks[0]
	if b.Kitchen != "claude" {
		t.Fatalf("kitchen = %q, want claude", b.Kitchen)
	}
	if len(b.Lines) < 1 {
		t.Fatalf("lines = %+v, want reversal message", b.Lines)
	}
	if got := b.Lines[len(b.Lines)-1].Text; !strings.Contains(got, "reason: reversing most recent switch") || !strings.Contains(got, "Use /back to return") {
		t.Fatalf("last line = %q, want reversal confirmation with hint", got)
	}
	if len(m.runtimeEvents) != 2 {
		t.Fatalf("runtime events = %d, want 2", len(m.runtimeEvents))
	}
	lastEvent := m.runtimeEvents[len(m.runtimeEvents)-1]
	for key, want := range map[string]string{"from": "gpt", "to": "claude", "reason": "reversing most recent switch"} {
		if got := lastEvent.Fields[key]; got != want {
			t.Fatalf("event field %q = %q, want %q", key, got, want)
		}
	}
}

func TestExecutePaletteCommand_BackReversesAutoSwitch(t *testing.T) {
	t.Parallel()

	claude := &switchTestAdapter{events: []adapter.Event{
		{Type: adapter.EventText, Kitchen: "claude", Text: "Checking local context first."},
		{Type: adapter.EventRateLimit, Kitchen: "claude", RateLimit: &adapter.RateLimitInfo{Status: "exhausted", IsExhaustion: true}},
		{Type: adapter.EventDone, Kitchen: "claude", ExitCode: 1},
	}}
	gemini := &switchTestAdapter{events: []adapter.Event{
		{Type: adapter.EventText, Kitchen: "gemini", Text: "Searching the web now."},
		{Type: adapter.EventDone, Kitchen: "gemini", ExitCode: 0},
	}}

	var runtimeEvents []observability.Event
	orch := orchestrator.Orchestrator{
		Factory: func(_ context.Context, _ string, exclude map[string]bool, _ string, _ map[string]string) (orchestrator.RouteResult, error) {
			if !exclude["claude"] {
				return orchestrator.RouteResult{
					Decision: sommelier.Decision{Kitchen: "claude", Tier: "keyword", Reason: "initial"},
					Adapter:  claude,
				}, nil
			}
			return orchestrator.RouteResult{
				Decision: sommelier.Decision{Kitchen: "gemini", Tier: "auto-switch", Reason: `task mentioned "search the web" (hard signal)`},
				Adapter:  gemini,
			}, nil
		},
		Sink: observability.FuncSink(func(evt observability.Event) {
			runtimeEvents = append(runtimeEvents, evt)
		}),
	}

	m := NewModel(nil)
	m.kitchenStates = []KitchenState{{Name: "claude", Status: "ready", Remaining: -1, Trend: ""}, {Name: "gemini", Status: "ready", Remaining: -1, Trend: ""}}

	block := Block{ID: "b1", Prompt: "search the web for the latest incident notes", StartedAt: time.Now(), State: StateStreaming}
	conv, err := orch.Run(context.Background(), orchestrator.RunRequest{
		ConversationID: "conv-auto-back",
		BlockID:        block.ID,
		Prompt:         block.Prompt,
	}, func(res orchestrator.RouteResult) {
		block.ConversationID = "conv-auto-back"
		block.Kitchen = res.Decision.Kitchen
		block.Decision = res.Decision
		if !containsProvider(block.ProviderChain, res.Decision.Kitchen) {
			block.ProviderChain = append(block.ProviderChain, res.Decision.Kitchen)
		}
	}, func(evt adapter.Event) {
		block.AppendEvent(evt)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	block.Conversation = conv
	if len(block.Conversation.Segments) == 0 {
		t.Fatal("expected conversation segments for auto-switch test")
	}
	last := &block.Conversation.Segments[len(block.Conversation.Segments)-1]
	last.Status = conversation.SegmentActive
	last.EndedAt = nil
	last.EndReason = ""
	block.Conversation.ActiveSegmentID = last.ID
	block.Conversation.Status = conversation.StatusActive
	m.runtimeEvents = runtimeEvents
	m.blocks = []Block{block}
	m.focusedIdx = 0

	body := m.blocks[0].RenderBody(100, RenderRaw)
	if !strings.Contains(body, "Use /back to return") {
		t.Fatalf("body = %q, want auto-switch reversal hint", body)
	}

	m.executePaletteCommand("back")

	updated := m.blocks[0]
	if updated.Kitchen != "claude" {
		t.Fatalf("kitchen = %q, want claude", updated.Kitchen)
	}
	if got := updated.Lines[len(updated.Lines)-1].Text; !strings.Contains(got, "reversing most recent switch") || !strings.Contains(got, "Use /back to return") {
		t.Fatalf("last line = %q, want reversal confirmation with hint", got)
	}
}

func TestRuntimeActivityLines_RenderSwitchAndRuntimeEventsInOrder(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	now := time.Now()
	m.runtimeEvents = []observability.Event{
		{
			Kind:     "status",
			Provider: "milliways",
			Text:     "dispatch queued",
			At:       now.Add(-3 * time.Second),
		},
		{
			Kind:     "switch",
			Provider: "gpt",
			At:       now.Add(-2 * time.Second),
			Fields: map[string]string{
				"from":   "claude",
				"to":     "gpt",
				"reason": "user requested",
			},
		},
		{
			Kind:     "status",
			Provider: "gpt",
			Text:     "continuing work",
			At:       now.Add(-1 * time.Second),
		},
	}

	lines := m.runtimeActivityLines(10)
	if len(lines) != 3 {
		t.Fatalf("activity lines = %#v, want 3", lines)
	}
	if !strings.Contains(lines[0], "dispatch queued") {
		t.Fatalf("first activity line = %q, want queued status", lines[0])
	}
	for _, want := range []string{"switch", "claude → gpt", "user requested"} {
		if !strings.Contains(lines[1], want) {
			t.Fatalf("switch activity line = %q, want substring %q", lines[1], want)
		}
	}
	if !strings.Contains(lines[2], "continuing work") {
		t.Fatalf("third activity line = %q, want trailing runtime status", lines[2])
	}
}

func TestExecutePaletteCommand_BackWithoutSwitchHistoryShowsHelpfulMessage(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)

	m.executePaletteCommand("back")

	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(m.blocks))
	}
	if got := m.blocks[0].Lines[0].Text; !strings.Contains(got, "no prior switch") {
		t.Fatalf("message = %q, want helpful no-history guidance", got)
	}
}

func TestExecutePaletteCommand_BackReusesUnavailableKitchenHandling(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.kitchenStates = []KitchenState{
		{Name: "claude", Status: "exhausted", ResetsAt: "22:00", Remaining: -1, Trend: ""},
		{Name: "gpt", Status: "ready", Remaining: -1, Trend: ""},
	}
	m.runtimeEvents = []observability.Event{{
		Kind: "switch",
		Fields: map[string]string{
			"from": "claude",
			"to":   "gpt",
		},
	}}

	m.executePaletteCommand("back")

	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d", len(m.blocks))
	}
	got := m.blocks[0].Lines[0].Text
	if !strings.Contains(got, "unavailable") {
		t.Fatalf("back error = %q, want unavailable message", got)
	}
	if !strings.Contains(got, "gpt") {
		t.Fatalf("back error = %q, want ready kitchen list", got)
	}
	if !strings.Contains(got, "22:00") {
		t.Fatalf("back error = %q, want reset time", got)
	}
}

func TestRenderPalette_IncludesBackCommand(t *testing.T) {
	t.Parallel()

	rendered := RenderPalette(FilterPalette("back"), 0, "back", 80)

	if !strings.Contains(rendered, "back") {
		t.Fatalf("rendered palette = %q, want back command", rendered)
	}
	if !strings.Contains(rendered, "Reverse the most recent") || !strings.Contains(rendered, "switch") {
		t.Fatalf("rendered palette = %q, want back description", rendered)
	}
}

type switchTestAdapter struct {
	events []adapter.Event
}

func (s *switchTestAdapter) Exec(_ context.Context, _ kitchen.Task) (<-chan adapter.Event, error) {
	ch := make(chan adapter.Event, len(s.events))
	for _, evt := range s.events {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func (s *switchTestAdapter) Send(context.Context, string) error { return nil }
func (s *switchTestAdapter) SupportsResume() bool               { return false }
func (s *switchTestAdapter) SessionID() string                  { return "" }
func (s *switchTestAdapter) ProcessID() int                     { return 0 }
func (s *switchTestAdapter) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{
		StructuredEvents:    true,
		InteractiveSend:     true,
		ExhaustionDetection: "structured",
	}
}
