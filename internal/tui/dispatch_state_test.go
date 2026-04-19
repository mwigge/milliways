package tui

import "testing"

func TestStateIcon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state   DispatchState
		wantNon string // should NOT be empty
	}{
		{StateIdle, ""},
		{StateRouting, ""},
		{StateRouted, ""},
		{StateStreaming, ""},
		{StateDone, ""},
		{StateFailed, ""},
		{StateCancelled, ""},
		{StateAwaiting, ""},
		{StateConfirming, ""},
	}

	for _, tt := range tests {
		icon := stateIcon(tt.state)
		if icon == "" {
			t.Errorf("stateIcon(%d) returned empty string", tt.state)
		}
	}
}

func TestStateLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state DispatchState
		want  string
	}{
		{StateIdle, "idle"},
		{StateRouting, "routing..."},
		{StateRouted, "routed"},
		{StateStreaming, "streaming"},
		{StateDone, "done"},
		{StateFailed, "failed"},
		{StateCancelled, "cancelled"},
		{StateAwaiting, "waiting for you"},
		{StateConfirming, "confirm required"},
	}

	for _, tt := range tests {
		label := stateLabel(tt.state)
		if label != tt.want {
			t.Errorf("stateLabel(%d) = %q, want %q", tt.state, label, tt.want)
		}
	}
}

func TestStateLabelsUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for s := StateIdle; s <= StateConfirming; s++ {
		label := stateLabel(s)
		if seen[label] {
			t.Errorf("duplicate state label: %q", label)
		}
		seen[label] = true
	}
}

func TestRenderStatusBar_Ready(t *testing.T) {
	t.Parallel()

	m := Model{
		kitchenStates: []KitchenState{
			{Name: "claude", Status: "ready", Remaining: -1, Trend: ""},
			{Name: "opencode", Status: "ready", Remaining: -1, Trend: ""},
		},
	}
	bar := m.renderStatusBar()
	if bar == "" {
		t.Error("status bar should not be empty with ready kitchens")
	}
}

func TestRenderStatusBar_Exhausted(t *testing.T) {
	t.Parallel()

	m := Model{
		kitchenStates: []KitchenState{
			{Name: "claude", Status: "exhausted", ResetsAt: "20:00", Remaining: -1, Trend: ""},
		},
	}
	bar := m.renderStatusBar()
	if bar == "" {
		t.Error("status bar should not be empty")
	}
}

func TestRenderStatusBar_Empty(t *testing.T) {
	t.Parallel()

	m := Model{}
	bar := m.renderStatusBar()
	if bar != "" {
		t.Errorf("empty kitchenStates should return empty, got %q", bar)
	}
}

func TestRenderStatusBar_NotInstalled_Omitted(t *testing.T) {
	t.Parallel()

	m := Model{
		kitchenStates: []KitchenState{
			{Name: "aider", Status: "not-installed", Remaining: -1, Trend: ""},
		},
	}
	bar := m.renderStatusBar()
	if bar != "" {
		t.Errorf("not-installed kitchens should be omitted, got %q", bar)
	}
}
