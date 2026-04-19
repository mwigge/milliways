package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLoadSnippetsCreatesDefault(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "snippets.toml")

	snippets := loadSnippets(path)
	if len(snippets) == 0 {
		t.Fatal("loadSnippets returned empty slice")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("default snippets file missing: %v", err)
	}
	if len(snippets) != len(defaultSnippets) {
		t.Fatalf("len(snippets) = %d, want %d", len(snippets), len(defaultSnippets))
	}
}

func TestLoadSnippetsReadsExistingAndMergesDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "snippets.toml")
	content := `
[[snippet]]
name = "custom"
body = "custom body"
tags = ["custom"]
lang = "en"

[[snippet]]
name = "explain"
body = "override"
tags = ["override"]
lang = "en"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	snippets := loadSnippets(path)
	if len(snippets) != len(defaultSnippets)+1 {
		t.Fatalf("len(snippets) = %d, want %d", len(snippets), len(defaultSnippets)+1)
	}
	if snippets[0].Name != "custom" {
		t.Fatalf("first snippet = %q, want custom", snippets[0].Name)
	}
	if snippets[1].Name != "explain" || snippets[1].Body != "override" {
		t.Fatalf("override snippet = %+v, want explain override", snippets[1])
	}
}

func TestFilterSnippets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter string
		want   []string
	}{
		{name: "empty returns all", filter: "", want: []string{"explain", "test for", "refactor", "review"}},
		{name: "name match", filter: "explain", want: []string{"explain"}},
		{name: "tag match", filter: "pytest", want: []string{"test for"}},
		{name: "case insensitive", filter: "REFACTOR", want: []string{"refactor"}},
		{name: "no match", filter: "xyznotfound", want: nil},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := filterSnippets(defaultSnippets, tt.filter)
			if len(got) != len(tt.want) {
				t.Fatalf("len(filterSnippets(%q)) = %d, want %d", tt.filter, len(got), len(tt.want))
			}
			for i, want := range tt.want {
				if got[i].Name != want {
					t.Fatalf("filterSnippets(%q)[%d] = %q, want %q", tt.filter, i, got[i].Name, want)
				}
			}
		})
	}
}

func TestHandleKeySnippetPanel(t *testing.T) {
	t.Parallel()

	t.Run("enter inserts selected snippet body", func(t *testing.T) {
		t.Parallel()

		m := NewModel(nil)
		m.sidePanelIdx = int(SidePanelSnippets)
		m.snippetIndex = cloneSnippets(defaultSnippets)
		m.snippetSelected = 1

		cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		if len(cmds) != 0 {
			t.Fatalf("expected no commands, got %d", len(cmds))
		}
		if got, want := m.input.Value(), snippetBodyForInput(defaultSnippets[1].Body); got != want {
			t.Fatalf("input value = %q, want %q", got, want)
		}
		if m.sidePanelIdx != int(SidePanelLedger) {
			t.Fatalf("sidePanelIdx = %d, want %d", m.sidePanelIdx, int(SidePanelLedger))
		}
	})

	t.Run("typing filters snippets", func(t *testing.T) {
		t.Parallel()

		m := NewModel(nil)
		m.sidePanelIdx = int(SidePanelSnippets)
		m.snippetIndex = cloneSnippets(defaultSnippets)

		cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
		if len(cmds) != 0 {
			t.Fatalf("expected no commands, got %d", len(cmds))
		}
		if m.snippetFilter != "p" {
			t.Fatalf("snippetFilter = %q, want p", m.snippetFilter)
		}
		if len(m.snippetIndex) == 0 {
			t.Fatal("expected filtered snippets")
		}
		for _, item := range m.snippetIndex {
			nameOrTags := item.Name + " " + strings.Join(item.Tags, " ")
			if !strings.Contains(strings.ToLower(nameOrTags), "p") {
				t.Fatalf("filtered snippet %+v does not match filter", item)
			}
		}
	})

	t.Run("backspace clears filter", func(t *testing.T) {
		t.Parallel()

		m := NewModel(nil)
		m.sidePanelIdx = int(SidePanelSnippets)
		m.snippetFilter = "rev"
		m.snippetIndex = filterSnippets(defaultSnippets, m.snippetFilter)
		m.snippetSelected = 1

		cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
		if len(cmds) != 0 {
			t.Fatalf("expected no commands, got %d", len(cmds))
		}
		if m.snippetFilter != "re" {
			t.Fatalf("snippetFilter = %q, want re", m.snippetFilter)
		}
		if m.snippetSelected != 0 {
			t.Fatalf("snippetSelected = %d, want 0", m.snippetSelected)
		}
	})

	t.Run("cycling into snippets refreshes list", func(t *testing.T) {
		t.Parallel()

		m := NewModel(nil)
		m.sidePanelIdx = int(SidePanelOpenSpec)
		m.snippetFilter = "review"

		cmds := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
		if len(cmds) != 0 {
			t.Fatalf("expected no commands, got %d", len(cmds))
		}
		if m.sidePanelIdx != int(SidePanelSnippets) {
			t.Fatalf("sidePanelIdx = %d, want %d", m.sidePanelIdx, int(SidePanelSnippets))
		}
		if len(m.snippetIndex) != 1 || m.snippetIndex[0].Name != "review" {
			t.Fatalf("snippetIndex = %+v, want only review", m.snippetIndex)
		}
	})
}

func TestRenderSnippetsPanel(t *testing.T) {
	t.Parallel()

	t.Run("shows empty state with filter hint", func(t *testing.T) {
		t.Parallel()

		m := NewModel(nil)
		m.snippetFilter = "nomatch"
		m.snippetIndex = nil

		got := m.renderSnippetsPanel(24, 8)
		if !strings.Contains(got, "no snippets match") {
			t.Fatalf("renderSnippetsPanel() = %q, want no-match hint", got)
		}
	})

	t.Run("shows selected snippet and controls", func(t *testing.T) {
		t.Parallel()

		m := NewModel(nil)
		m.snippetIndex = cloneSnippets(defaultSnippets)
		m.snippetSelected = 2
		m.snippetFilter = "ref"

		got := m.renderSnippetsPanel(24, 8)
		for _, want := range []string{"> refactor", "Filter: ref", "[enter] insert"} {
			if !strings.Contains(got, want) {
				t.Fatalf("renderSnippetsPanel() = %q, want contains %q", got, want)
			}
		}
	})
}
