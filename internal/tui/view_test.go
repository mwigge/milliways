package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/pantry"
)

func TestRenderJobsPanel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		model    Model
		want     []string
		notWant  []string
		wantSame string
	}{
		{
			name:     "hidden when terminal too short",
			model:    Model{height: 15},
			wantSame: "",
		},
		{
			name:     "hidden when there are no tickets",
			model:    Model{height: 30},
			wantSame: "",
		},
		{
			name: "renders milliways tickets only",
			model: Model{
				height:     30,
				jobTickets: []pantry.Ticket{{Status: "complete", Prompt: "test prompt", Kitchen: "k1"}},
			},
			want:    []string{"Jobs", "✓", "test prompt", "k1"},
			notWant: []string{"OpenHands", "no db", "no jobs yet"},
		},
		{
			name: "truncates long prompts",
			model: Model{
				height:     30,
				jobTickets: []pantry.Ticket{{Status: "pending", Prompt: "abcdefghijklmnopqrstuvwxyz", Kitchen: "k1"}},
			},
			want:    []string{"abcdefghijklmnopqrst…"},
			notWant: []string{"abcdefghijklmnopqrstuvwxyz"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.model.renderJobsPanel()
			if tt.wantSame != "" || got == "" {
				if got != tt.wantSame {
					t.Fatalf("renderJobsPanel() = %q, want %q", got, tt.wantSame)
				}
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("renderJobsPanel() = %q, want contains %q", got, want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(got, notWant) {
					t.Fatalf("renderJobsPanel() = %q, should not contain %q", got, notWant)
				}
			}
		})
	}
}

func TestRuntimeActivityLines_FocusedConversation(t *testing.T) {
	t.Parallel()

	m := Model{
		focusedIdx: 0,
		blocks: []Block{
			{ID: "b1", ConversationID: "conv-1"},
		},
		runtimeEvents: []observability.Event{
			{ConversationID: "conv-2", Kind: "route", Text: "other", At: time.Now()},
			{ConversationID: "conv-1", Kind: "failover", Provider: "claude", Text: "provider exhausted", At: time.Now()},
		},
	}

	lines := m.runtimeActivityLines(5)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "provider exhausted") {
		t.Fatalf("unexpected activity line: %q", lines[0])
	}
}

func TestRuntimeActivityLines_RendersSwitchDecisionPayload(t *testing.T) {
	t.Parallel()

	m := Model{
		runtimeEvents: []observability.Event{
			{
				Kind: "switch",
				At:   time.Now(),
				Fields: map[string]string{
					"from":    "claude",
					"to":      "gemini",
					"reason":  "quota",
					"trigger": "fallback",
					"tier":    "secondary",
				},
			},
		},
	}

	lines := m.runtimeActivityLines(5)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d (%q)", len(lines), lines)
	}
	if !strings.Contains(lines[0], "switch: claude → gemini (quota)") {
		t.Fatalf("unexpected switch line: %q", lines[0])
	}
	if !strings.Contains(lines[1], "trigger: fallback") {
		t.Fatalf("unexpected trigger line: %q", lines[1])
	}
	if !strings.Contains(lines[2], "tier: secondary") {
		t.Fatalf("unexpected tier line: %q", lines[2])
	}
	if !strings.HasPrefix(lines[1], "         ") {
		t.Fatalf("expected indented trigger line, got %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "         ") {
		t.Fatalf("expected indented tier line, got %q", lines[2])
	}
}
