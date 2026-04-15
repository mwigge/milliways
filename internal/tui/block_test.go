package tui

import (
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/sommelier"
)

func newTestBlock(id, prompt, kitchen string, state DispatchState) *Block {
	return &Block{
		ID:        id,
		Prompt:    prompt,
		Kitchen:   kitchen,
		State:     state,
		StartedAt: time.Now().Add(-5 * time.Second),
		Duration:  5 * time.Second,
	}
}

func TestBlock_RenderHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		block   *Block
		wantSub string // substring that must appear
	}{
		{
			name:    "idle block shows prompt",
			block:   newTestBlock("b1", "explain auth", "", StateIdle),
			wantSub: "explain auth",
		},
		{
			name:    "streaming block shows kitchen",
			block:   newTestBlock("b1", "fix the bug", "claude", StateStreaming),
			wantSub: "claude",
		},
		{
			name:    "long prompt gets truncated",
			block:   newTestBlock("b1", "this is a very long prompt that exceeds the fifty character maximum length", "claude", StateStreaming),
			wantSub: "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			header := tt.block.RenderHeader()
			if header == "" {
				t.Fatal("RenderHeader returned empty string")
			}
			// lipgloss adds ANSI escapes, so we can't do exact match.
			// Just verify it's non-empty and contains expected substring.
			if tt.wantSub != "" && !containsPlain(header, tt.wantSub) {
				t.Errorf("header %q does not contain %q (after stripping ANSI)", stripAnsi(header), tt.wantSub)
			}
		})
	}
}

func TestBlock_RenderBody(t *testing.T) {
	t.Parallel()

	b := newTestBlock("b1", "explain auth", "claude", StateStreaming)
	b.AppendEvent(adapter.Event{
		Type:    adapter.EventText,
		Kitchen: "claude",
		Text:    "The auth middleware validates JWT tokens.",
	})
	b.AppendEvent(adapter.Event{
		Type:     adapter.EventCodeBlock,
		Kitchen:  "claude",
		Language: "go",
		Code:     "func Validate(token string) error {\n\treturn nil\n}",
	})

	body := b.RenderBody(80, RenderRaw)
	if body == "" {
		t.Fatal("RenderBody returned empty string")
	}
	if !containsPlain(body, "auth middleware validates JWT") {
		t.Errorf("body missing text content: %s", stripAnsi(body))
	}
	if !containsPlain(body, "func Validate") {
		t.Errorf("body missing code content: %s", stripAnsi(body))
	}
}

func TestBlock_RenderBody_Done(t *testing.T) {
	t.Parallel()

	b := newTestBlock("b1", "fix bug", "opencode", StateDone)
	b.AppendEvent(adapter.Event{
		Type:    adapter.EventText,
		Kitchen: "opencode",
		Text:    "Fixed.",
	})
	b.Cost = &adapter.CostInfo{USD: 0.05}

	body := b.RenderBody(80, RenderRaw)
	if !containsPlain(body, "done") {
		t.Error("done block body should contain 'done'")
	}
	if !containsPlain(body, "$0.05") {
		t.Error("done block body should contain cost")
	}
}

func TestBlock_RenderBody_Empty_Routing(t *testing.T) {
	t.Parallel()
	b := newTestBlock("b1", "test", "", StateRouting)
	body := b.RenderBody(80, RenderRaw)
	if !containsPlain(body, "routing") {
		t.Error("routing block with no lines should show 'routing...'")
	}
}

func TestBlock_AppendEvent(t *testing.T) {
	t.Parallel()

	b := newTestBlock("b1", "test", "claude", StateStreaming)

	b.AppendEvent(adapter.Event{Type: adapter.EventText, Kitchen: "claude", Text: "hello"})
	if len(b.Lines) != 1 || b.Lines[0].Type != LineText {
		t.Fatalf("expected 1 text line, got %d", len(b.Lines))
	}

	b.AppendEvent(adapter.Event{Type: adapter.EventCodeBlock, Kitchen: "claude", Code: "x=1", Language: "python"})
	if len(b.Lines) != 2 || b.Lines[1].Type != LineCode {
		t.Fatalf("expected 2 lines with code, got %d", len(b.Lines))
	}

	b.AppendEvent(adapter.Event{Type: adapter.EventToolUse, Kitchen: "claude", ToolName: "Edit", ToolStatus: "started"})
	if len(b.Lines) != 3 || b.Lines[2].Type != LineTool {
		t.Fatalf("expected 3 lines with tool, got %d", len(b.Lines))
	}

	b.AppendEvent(adapter.Event{Type: adapter.EventError, Kitchen: "claude", Text: "something broke"})
	if len(b.Lines) != 4 || b.Lines[3].Type != LineSystem {
		t.Fatalf("expected 4 lines with system, got %d", len(b.Lines))
	}
}

func TestBlock_Complete(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		b := newTestBlock("b1", "test", "claude", StateStreaming)
		b.Complete(0, &adapter.CostInfo{USD: 0.01})
		if b.State != StateDone {
			t.Errorf("expected StateDone, got %v", b.State)
		}
		if b.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", b.ExitCode)
		}
		if b.Cost == nil || b.Cost.USD != 0.01 {
			t.Error("cost not set correctly")
		}
	})

	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		b := newTestBlock("b1", "test", "claude", StateStreaming)
		b.Complete(1, nil)
		if b.State != StateFailed {
			t.Errorf("expected StateFailed, got %v", b.State)
		}
	})
}

func TestBlock_ToggleCollapse(t *testing.T) {
	t.Parallel()

	b := newTestBlock("b1", "test", "claude", StateDone)
	if b.Collapsed {
		t.Fatal("should start expanded")
	}
	b.ToggleCollapse()
	if !b.Collapsed {
		t.Fatal("should be collapsed after toggle")
	}
	b.ToggleCollapse()
	if b.Collapsed {
		t.Fatal("should be expanded after second toggle")
	}
}

func TestBlock_Render_Collapsed(t *testing.T) {
	t.Parallel()

	b := newTestBlock("b1", "explain auth", "claude", StateDone)
	b.AppendEvent(adapter.Event{Type: adapter.EventText, Kitchen: "claude", Text: "long explanation here"})
	b.Collapsed = true

	rendered := b.Render(80, 0, RenderRaw)
	if containsPlain(rendered, "long explanation") {
		t.Error("collapsed block should not show body content")
	}
	if !containsPlain(rendered, "explain auth") {
		t.Error("collapsed block should show prompt in header")
	}
}

func TestBlock_Render_Expanded(t *testing.T) {
	t.Parallel()

	b := newTestBlock("b1", "explain auth", "claude", StateDone)
	b.AppendEvent(adapter.Event{Type: adapter.EventText, Kitchen: "claude", Text: "The auth middleware"})

	rendered := b.Render(80, 0, RenderRaw)
	if !containsPlain(rendered, "auth middleware") {
		t.Error("expanded block should show body content")
	}
}

func TestBlock_IsActive(t *testing.T) {
	t.Parallel()

	activeStates := []DispatchState{StateRouting, StateRouted, StateStreaming, StateAwaiting, StateConfirming}
	for _, s := range activeStates {
		b := &Block{State: s}
		if !b.IsActive() {
			t.Errorf("state %v should be active", s)
		}
	}

	inactiveStates := []DispatchState{StateIdle, StateDone, StateFailed, StateCancelled}
	for _, s := range inactiveStates {
		b := &Block{State: s}
		if b.IsActive() {
			t.Errorf("state %v should not be active", s)
		}
	}
}

func TestBlock_BorderStyle(t *testing.T) {
	t.Parallel()

	b := newTestBlock("b1", "test", "claude", StateDone)
	b.Decision = sommelier.Decision{Kitchen: "claude"}

	// Focused overrides state-based border
	b.Focused = true
	style := b.borderStyle()
	if style.GetBorderBottomForeground() != focusedBlockBorder.GetBorderBottomForeground() {
		t.Error("focused block should use focused border")
	}

	b.Focused = false
	b.State = StateDone
	style = b.borderStyle()
	if style.GetBorderBottomForeground() != doneBlockBorder.GetBorderBottomForeground() {
		t.Error("done block should use done border")
	}

	b.State = StateFailed
	style = b.borderStyle()
	if style.GetBorderBottomForeground() != failedBlockBorder.GetBorderBottomForeground() {
		t.Error("failed block should use failed border")
	}
}

// containsPlain checks if s contains sub after stripping ANSI escape codes.
func containsPlain(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 &&
		len(stripAnsi(s)) > 0 &&
		contains(stripAnsi(s), sub)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// stripAnsi removes ANSI escape sequences from a string.
func stripAnsi(s string) string {
	var result []byte
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			// Skip until we find the terminating letter
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
					i++
				}
				if i < len(s) {
					i++ // skip the terminating letter
				}
			}
		} else {
			result = append(result, s[i])
			i++
		}
	}
	return string(result)
}
