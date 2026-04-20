package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mwigge/milliways/internal/observability"
)

func TestRenderTracePanelEvents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := NewModel(nil)
	m.runtimeEvents = []observability.Event{{Kind: "tool.called", Text: "Bash", At: time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)}}

	got := m.renderTracePanel(50, 10)
	for _, want := range []string{"View: Events", "tool.called", "Bash", "[tab] switch view"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderTracePanel() missing %q:\n%s", want, got)
		}
	}
}

func TestTracePanelTabCyclesViews(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.sidePanelIdx = int(SidePanelTrace)

	if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyTab}); len(cmds) != 0 {
		t.Fatalf("expected no commands, got %d", len(cmds))
	}
	if m.tracePanelView != tracePanelTimeline {
		t.Fatalf("tracePanelView = %v, want timeline", m.tracePanelView)
	}
	if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyTab}); len(cmds) != 0 {
		t.Fatalf("expected no commands, got %d", len(cmds))
	}
	if m.tracePanelView != tracePanelGraph {
		t.Fatalf("tracePanelView = %v, want graph", m.tracePanelView)
	}
}

func TestRenderTracePanelTimeline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := NewModel(nil)
	m.tracePanelView = tracePanelTimeline
	m.runtimeEvents = []observability.Event{{Kind: "switch", Text: "switch claude -> gemini", At: time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)}}

	got := m.renderTracePanel(60, 10)
	if !strings.Contains(got, "Mermaid source via") {
		t.Fatalf("renderTracePanel() = %q, want Mermaid hint", got)
	}
}

func TestRenderTracePanelReadsTraceFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	traceDir := filepath.Join(home, ".config", "milliways", "traces")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	content := strings.Join([]string{
		`{"session":"sess-a","ts":"2026-04-20T10:00:00Z","type":"agent.delegate","description":"delegate coder-go"}`,
		`{"session":"sess-a","ts":"2026-04-20T10:00:01Z","type":"agent.tool","description":"Bash","data":{"tool_name":"Bash"}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(traceDir, "sess-a.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	m := NewModel(nil)
	got := m.renderTracePanel(80, 10)
	for _, want := range []string{"Session: sess-a", "agent.delegate", "Bash"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderTracePanel() missing %q:\n%s", want, got)
		}
	}
}

func TestTracePanelUpDownChangesSelectedSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	traceDir := filepath.Join(home, ".config", "milliways", "traces")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(traceDir, name+".jsonl"), []byte(`{"session":"`+name+`","ts":"2026-04-20T10:00:00Z","type":"agent.observe"}`), 0o600); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	m := NewModel(nil)
	m.sidePanelIdx = int(SidePanelTrace)
	if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyDown}); len(cmds) != 0 {
		t.Fatalf("expected no commands, got %d", len(cmds))
	}
	if m.traceSessionSelected != 1 {
		t.Fatalf("traceSessionSelected = %d, want 1", m.traceSessionSelected)
	}
	if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyUp}); len(cmds) != 0 {
		t.Fatalf("expected no commands, got %d", len(cmds))
	}
	if m.traceSessionSelected != 0 {
		t.Fatalf("traceSessionSelected = %d, want 0", m.traceSessionSelected)
	}
}
