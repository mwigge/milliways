package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HistoryEntry is a searchable prompt from the session or ledger.
type HistoryEntry struct {
	Prompt  string
	Kitchen string
	Status  string // "done", "failed", "cancelled"
}

// SearchState holds the fuzzy history search UI state.
type SearchState struct {
	Active   bool
	Query    string
	Matches  []HistoryEntry
	Selected int
}

// FilterHistory returns history entries matching the query.
func FilterHistory(entries []HistoryEntry, query string) []HistoryEntry {
	if query == "" {
		return entries
	}
	query = strings.ToLower(query)
	var matches []HistoryEntry
	for _, e := range entries {
		if fuzzyMatch(strings.ToLower(e.Prompt), query) {
			matches = append(matches, e)
		}
	}
	return matches
}

// BuildHistoryFromBlocks extracts searchable history from blocks.
func BuildHistoryFromBlocks(blocks []Block) []HistoryEntry {
	var entries []HistoryEntry
	// Reverse order — most recent first.
	for i := len(blocks) - 1; i >= 0; i-- {
		b := &blocks[i]
		if !b.isDone() {
			continue
		}
		status := "done"
		if b.ExitCode != 0 {
			status = "failed"
		}
		if b.State == StateCancelled {
			status = "cancelled"
		}
		entries = append(entries, HistoryEntry{
			Prompt:  b.Prompt,
			Kitchen: b.Kitchen,
			Status:  status,
		})
	}
	return entries
}

// RenderSearch renders the fuzzy search overlay.
func RenderSearch(matches []HistoryEntry, selected int, query string, width int) string {
	searchWidth := 50
	if width-4 < searchWidth {
		searchWidth = width - 4
	}

	lines := []string{mutedStyle.Render("Search: " + query)}

	if len(matches) == 0 {
		lines = append(lines, mutedStyle.Render("  No matches"))
	} else {
		limit := len(matches)
		if limit > 10 {
			limit = 10
		}
		promptWidth := searchWidth - 20
		if promptWidth < 10 {
			promptWidth = 10
		}
		for i, entry := range matches[:limit] {
			prefix := "  "
			if i == selected {
				prefix = "> "
			}
			prompt := entry.Prompt
			if len(prompt) > promptWidth {
				prompt = prompt[:promptWidth-3] + "..."
			}

			icon := StatusIcon(entry.Status)
			kitchen := ""
			if entry.Kitchen != "" {
				kitchen = KitchenBadge(entry.Kitchen) + " "
			}
			lines = append(lines, prefix+icon+" "+kitchen+prompt)
		}
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#2563EB")).
		Width(searchWidth).
		Padding(0, 1)

	return border.Render(strings.Join(lines, "\n"))
}
