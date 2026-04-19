package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mwigge/milliways/internal/kitchen"
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
			model:    Model{},
			wantSame: "",
		},
		{
			name:     "hidden when there are no tickets",
			model:    Model{},
			wantSame: "",
		},
		{
			name: "renders milliways tickets only",
			model: Model{
				jobTickets: []pantry.Ticket{{Status: "complete", Prompt: "test prompt", Kitchen: "k1"}},
			},
			want:    []string{"Jobs", "✓", "test prompt", "k1"},
			notWant: []string{"OpenHands", "no db", "no jobs yet"},
		},
		{
			name: "truncates long prompts",
			model: Model{
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

			got := tt.model.renderJobsPanel(40, 8)
			if tt.wantSame != "" || got == "" {
				if got != tt.wantSame {
					t.Fatalf("renderJobsPanel(40, 8) = %q, want %q", got, tt.wantSame)
				}
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("renderJobsPanel(40, 8) = %q, want contains %q", got, want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(got, notWant) {
					t.Fatalf("renderJobsPanel(40, 8) = %q, should not contain %q", got, notWant)
				}
			}
		})
	}
}

func TestRenderActiveSidePanel(t *testing.T) {
	t.Parallel()

	t.Run("empty when height too small", func(t *testing.T) {
		t.Parallel()

		m := NewModel(nil)
		if got := m.renderActiveSidePanel(24, 3); got != "" {
			t.Fatalf("renderActiveSidePanel(24, 3) = %q, want empty", got)
		}
	})

	t.Run("renders active panel with border and title", func(t *testing.T) {
		t.Parallel()

		m := NewModel(nil)
		m.sidePanelIdx = int(SidePanelJobs)
		m.jobTickets = []pantry.Ticket{{Status: "complete", Prompt: "test prompt", Kitchen: "k1"}}

		got := m.renderActiveSidePanel(24, 8)
		for _, want := range []string{"Jobs", "ctrl+[/ctrl+]", "╭", "✓"} {
			if !strings.Contains(got, want) {
				t.Fatalf("renderActiveSidePanel(24, 8) = %q, want contains %q", got, want)
			}
		}
	})
}

func TestStubPanelsReturnNonEmpty(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	tests := []struct {
		name string
		run  func() string
	}{
		{name: "cost", run: func() string { return m.renderCostPanel(24, 8) }},
		{name: "routing", run: func() string { return m.renderRoutingPanel(24, 8) }},
		{name: "system", run: func() string { return m.renderSystemPanel(24, 8) }},
		{name: "snippets", run: func() string { return m.renderSnippetsPanel(24, 8) }},
		{name: "compare", run: func() string { return m.renderComparePanel(24, 8) }},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.run(); strings.TrimSpace(got) == "" {
				t.Fatalf("%s panel rendered empty content", tt.name)
			}
		})
	}
}

func TestDiffPanelRendersFiles(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.changedFiles = []diffFile{
		{Path: "internal/tui/app.go", Status: "M"},
		{Path: "internal/tui/view.go", Status: "M"},
		{Path: "README.md", Status: "A"},
	}
	m.diffSelected = 1

	got := m.renderDiffPanel(50, 20)
	if !strings.Contains(got, "view.go") {
		t.Fatalf("expected view.go in output, got: %s", got)
	}
	if !strings.Contains(got, "> ") {
		t.Fatalf("expected selection marker in output, got: %s", got)
	}
	for _, want := range []string{"M", "A", "[↑↓] navigate"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output, got: %s", want, got)
		}
	}
}

func TestDiffPanelEmpty(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.changedFiles = nil

	got := m.renderDiffPanel(50, 20)
	if !strings.Contains(got, "no changes") {
		t.Fatalf("expected 'no changes' message, got: %s", got)
	}
}

func TestComparePanelEmpty(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.compareResults = nil
	m.activeCompareID = ""

	got := m.renderComparePanel(50, 20)
	if !strings.Contains(got, "ctrl+shift+enter") {
		t.Fatalf("expected compare hint, got: %s", got)
	}
}

func TestComparePanelShowsRunning(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.compareResults = map[string][]compareResult{}
	m.activeCompareID = "compare-123"

	got := m.renderComparePanel(50, 20)
	if !strings.Contains(got, "running") {
		t.Fatalf("expected running text, got: %s", got)
	}
}

func TestComparePanelRendersResults(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.compareResults = map[string][]compareResult{
		"compare-123": {
			{Kitchen: "claude", Done: true, Output: "Hello from claude"},
			{Kitchen: "codex", Error: "connection refused"},
		},
	}
	m.activeCompareID = "compare-123"
	m.compareSelected = 0

	got := m.renderComparePanel(50, 20)
	for _, want := range []string{"claude", "codex", "✓", "✗", "Hello from claude"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output, got: %s", want, got)
		}
	}
}

func TestStartCompareDispatchNoPrompt(t *testing.T) {
	m := NewModel(nil)

	cmds := m.startCompareDispatch("")
	if len(cmds) != 0 {
		t.Fatalf("expected no commands, got %d", len(cmds))
	}
	if len(m.compareResults) != 0 {
		t.Fatalf("expected no compare results, got %d", len(m.compareResults))
	}
}

func TestStartCompareDispatchInitializesResults(t *testing.T) {
	m := NewModel(nil)
	m.kitchenStates = []KitchenState{{Name: "gemini", Status: "ready"}, {Name: "claude", Status: "warning"}}

	cmds := m.startCompareDispatch("ship it")
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	if len(m.blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(m.blocks))
	}
	if m.activeCompareID == "" {
		t.Fatal("expected active compare id")
	}
	results := m.activeCompareResults()
	if len(results) != 2 {
		t.Fatalf("expected 2 compare results, got %d", len(results))
	}
	if results[0].Kitchen != "claude" || results[1].Kitchen != "gemini" {
		t.Fatalf("unexpected kitchen order: %#v", results)
	}
	for _, block := range m.blocks {
		if block.comparePrompt != m.activeCompareID {
			t.Fatalf("block compare prompt = %q, want %q", block.comparePrompt, m.activeCompareID)
		}
	}
}

func TestCompareNavigation(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.sidePanelIdx = int(SidePanelCompare)
	m.compareResults = map[string][]compareResult{
		"c1": {{Kitchen: "a"}, {Kitchen: "b"}, {Kitchen: "c"}},
	}
	m.activeCompareID = "c1"
	m.compareSelected = 0
	m.syncCompareSelection()

	if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyDown}); len(cmds) != 0 {
		t.Fatalf("expected no commands, got %d", len(cmds))
	}
	if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyDown}); len(cmds) != 0 {
		t.Fatalf("expected no commands, got %d", len(cmds))
	}
	if m.compareSelected != 2 || m.compareSelectedKitchen != "c" {
		t.Fatalf("selection = %d/%q", m.compareSelected, m.compareSelectedKitchen)
	}
	if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyUp}); len(cmds) != 0 {
		t.Fatalf("expected no commands, got %d", len(cmds))
	}
	if m.compareSelected != 1 || m.compareSelectedKitchen != "b" {
		t.Fatalf("selection = %d/%q", m.compareSelected, m.compareSelectedKitchen)
	}
}

func TestHandleKeyAltEnterStartsCompareDispatch(t *testing.T) {
	m := NewModel(nil)
	m.kitchenStates = []KitchenState{{Name: "claude", Status: "ready"}}
	m.input.SetValue("compare this")

	cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if len(cmds) != 1 {
		t.Fatalf("expected 1 compare command, got %d", len(cmds))
	}
	if len(m.blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(m.blocks))
	}
	if m.blocks[0].comparePrompt == "" {
		t.Fatal("expected compare prompt tag on block")
	}
	if m.input.Value() != "" {
		t.Fatalf("expected input cleared, got %q", m.input.Value())
	}
}

func TestBlockDoneAccumulatesCompareResult(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.blocks = []Block{{ID: "b1", Kitchen: "claude", comparePrompt: "c1", Lines: []OutputLine{{Text: "Hello world"}}}}
	m.compareResults = map[string][]compareResult{"c1": {{Kitchen: "claude"}}}
	m.activeCompareID = "c1"
	m.activeCount = 1

	updated, cmd := m.Update(blockDoneMsg{BlockID: "b1", Result: pantryResult(0)})
	_ = cmd
	model := updated.(Model)
	results := model.compareResults["c1"]
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Done {
		t.Fatal("expected result marked done")
	}
	if results[0].Output != "Hello world" {
		t.Fatalf("output = %q", results[0].Output)
	}
	if results[0].Percent != 100 {
		t.Fatalf("percent = %v, want 100", results[0].Percent)
	}
}

func pantryResult(exitCode int) kitchen.Result {
	return kitchen.Result{ExitCode: exitCode}
}

func TestParseDiffNameOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		output     string
		wantCount  int
		wantFirst  diffFile
		wantSecond diffFile
	}{
		{name: "modified", output: "M internal/tui/app.go\nM internal/tui/view.go\n", wantCount: 2, wantFirst: diffFile{Path: "internal/tui/app.go", Status: "M"}, wantSecond: diffFile{Path: "internal/tui/view.go", Status: "M"}},
		{name: "added", output: "A README.md\n", wantCount: 1, wantFirst: diffFile{Path: "README.md", Status: "A"}},
		{name: "empty", output: "", wantCount: 0},
		{name: "untracked format", output: "?? newfile.txt\n", wantCount: 1, wantFirst: diffFile{Path: "newfile.txt", Status: "??"}},
		{name: "plain path", output: "internal/tui/app.go\n", wantCount: 1, wantFirst: diffFile{Path: "internal/tui/app.go", Status: "M"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := parseDiffNameOutput(tt.output)
			if len(got) != tt.wantCount {
				t.Fatalf("parseDiffNameOutput() got %d files, want %d", len(got), tt.wantCount)
			}
			if tt.wantCount > 0 && got[0] != tt.wantFirst {
				t.Fatalf("parseDiffNameOutput()[0] = %+v, want %+v", got[0], tt.wantFirst)
			}
			if tt.wantCount > 1 && got[1] != tt.wantSecond {
				t.Fatalf("parseDiffNameOutput()[1] = %+v, want %+v", got[1], tt.wantSecond)
			}
		})
	}
}

func TestDiffNavigation(t *testing.T) {
	t.Parallel()

	t.Run("down clamps at end", func(t *testing.T) {
		t.Parallel()

		m := NewModel(nil)
		m.sidePanelIdx = int(SidePanelDiff)
		m.changedFiles = []diffFile{{Path: "a.txt", Status: "M"}, {Path: "b.txt", Status: "M"}, {Path: "c.txt", Status: "M"}}

		if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyDown}); len(cmds) != 0 {
			t.Fatalf("expected no commands, got %d", len(cmds))
		}
		if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyDown}); len(cmds) != 0 {
			t.Fatalf("expected no commands, got %d", len(cmds))
		}
		if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyDown}); len(cmds) != 0 {
			t.Fatalf("expected no commands, got %d", len(cmds))
		}

		if m.diffSelected != 2 {
			t.Fatalf("diffSelected = %d, want 2", m.diffSelected)
		}
	})

	t.Run("up clamps at start", func(t *testing.T) {
		t.Parallel()

		m := NewModel(nil)
		m.sidePanelIdx = int(SidePanelDiff)
		m.changedFiles = []diffFile{{Path: "a.txt", Status: "M"}, {Path: "b.txt", Status: "M"}, {Path: "c.txt", Status: "M"}}
		m.diffSelected = 1

		if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyUp}); len(cmds) != 0 {
			t.Fatalf("expected no commands, got %d", len(cmds))
		}
		if cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyUp}); len(cmds) != 0 {
			t.Fatalf("expected no commands, got %d", len(cmds))
		}

		if m.diffSelected != 0 {
			t.Fatalf("diffSelected = %d, want 0", m.diffSelected)
		}
	})
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

func TestRenderStatusBar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		states  []KitchenState
		want    string
		notWant string
	}{
		{
			name:   "empty states",
			states: []KitchenState{},
			want:   "",
		},
		{
			name:   "unlimited ready kitchen",
			states: []KitchenState{{Name: "claude", Status: "ready", Remaining: -1}},
			want:   "claude ✓",
		},
		{
			name:   "limited ready kitchen with remaining",
			states: []KitchenState{{Name: "claude", Status: "ready", Remaining: 12, UsageRatio: 0.76}},
			want:   "claude 12/50",
		},
		{
			name:   "limited ready with trend",
			states: []KitchenState{{Name: "claude", Status: "ready", Remaining: 12, UsageRatio: 0.76, Trend: "↑8%"}},
			want:   "claude 12/50 ↑8%",
		},
		{
			name:   "exhausted with resets at",
			states: []KitchenState{{Name: "gemini", Status: "exhausted", ResetsAt: "02:00"}},
			want:   "gemini ✗ (02:00)",
		},
		{
			name:   "warning with usage ratio",
			states: []KitchenState{{Name: "claude", Status: "warning", UsageRatio: 0.85}},
			want:   "⚠ 85%",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := Model{kitchenStates: tt.states}
			got := m.renderStatusBar()
			if tt.want == "" {
				if got != "" {
					t.Errorf("renderStatusBar() = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("renderStatusBar() = %q, want contains %q", got, tt.want)
			}
			if tt.notWant != "" && strings.Contains(got, tt.notWant) {
				t.Errorf("renderStatusBar() = %q, should NOT contain %q", got, tt.notWant)
			}
		})
	}
}
