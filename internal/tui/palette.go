package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PaletteItem is a command available in the command palette.
type PaletteItem struct {
	Command     string
	Description string
}

var paletteItems = []PaletteItem{
	{"status", "Show kitchen availability"},
	{"report", "Routing statistics"},
	{"cancel", "Cancel focused block"},
	{"collapse", "Collapse all blocks"},
	{"expand", "Expand focused block"},
	{"collapse all", "Collapse all blocks"},
	{"expand all", "Expand all blocks"},
	{"history", "Search dispatch history"},
	{"session save", "Save session to file"},
	{"session load", "Load a saved session"},
	{"summary", "Show session summary"},
}

// PaletteState holds the command palette UI state.
type PaletteState struct {
	Active    bool
	Query     string
	Matches   []PaletteItem
	Selected  int
}

// FilterPalette returns palette items matching the query via simple substring match.
func FilterPalette(query string) []PaletteItem {
	if query == "" {
		return paletteItems
	}
	query = strings.ToLower(query)
	var matches []PaletteItem
	for _, item := range paletteItems {
		if fuzzyMatch(strings.ToLower(item.Command), query) ||
			fuzzyMatch(strings.ToLower(item.Description), query) {
			matches = append(matches, item)
		}
	}
	return matches
}

// fuzzyMatch checks if all characters of pattern appear in s in order.
func fuzzyMatch(s, pattern string) bool {
	si := 0
	for pi := 0; pi < len(pattern) && si < len(s); si++ {
		if s[si] == pattern[pi] {
			pi++
			if pi == len(pattern) {
				return true
			}
		}
	}
	return false
}

// RenderPalette renders the command palette overlay.
func RenderPalette(matches []PaletteItem, selected int, query string, width int) string {
	paletteWidth := 40
	if width-4 < paletteWidth {
		paletteWidth = width - 4
	}

	lines := []string{mutedStyle.Render("/ " + query)}

	if len(matches) == 0 {
		lines = append(lines, mutedStyle.Render("  No matches"))
	} else {
		limit := len(matches)
		if limit > 8 {
			limit = 8
		}
		for i, item := range matches[:limit] {
			prefix := "  "
			if i == selected {
				prefix = "> "
			}
			cmd := item.Command
			desc := mutedStyle.Render(" — " + item.Description)
			if i == selected {
				cmd = lipgloss.NewStyle().Bold(true).Render(cmd)
			}
			lines = append(lines, prefix+cmd+desc)
		}
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#8B5CF6")).
		Width(paletteWidth).
		Padding(0, 1)

	return border.Render(strings.Join(lines, "\n"))
}
