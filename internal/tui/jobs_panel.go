package tui

import (
	"fmt"
	"strings"

	"github.com/mwigge/milliways/internal/pantry"
)

const (
	jobsPanelMaxRows   = 8
	jobsPanelPromptLen = 20
	jobsPanelKitchLen  = 8
)

// jobStatusIcon returns a status symbol for a ticket.
func jobStatusIcon(status string) string {
	switch status {
	case "complete":
		return successStyle.Render("✓")
	case "failed":
		return failureStyle.Render("✗")
	case "running":
		return runningStyle.Render("⟳")
	case "timeout":
		return mutedStyle.Render("⚠")
	default:
		return mutedStyle.Render("?")
	}
}

// truncate shortens s to maxRunes, appending "…" if trimmed.
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-1]) + "…"
}

// RenderJobsPanel renders the jobs panel for the TUI right column.
// tickets is the list to display (should already be ordered most-recent first).
// width controls the panel inner width.
func RenderJobsPanel(tickets []pantry.Ticket, width int) string {
	if tickets == nil {
		return panelBorder.Width(width).Render(mutedStyle.Render("Jobs unavailable"))
	}

	lines := []string{mutedStyle.Render("Jobs")}

	if len(tickets) == 0 {
		lines = append(lines, mutedStyle.Render("No jobs"))
		return panelBorder.Width(width).Render(strings.Join(lines, "\n"))
	}

	limit := jobsPanelMaxRows
	if len(tickets) < limit {
		limit = len(tickets)
	}

	for _, t := range tickets[:limit] {
		icon := jobStatusIcon(t.Status)
		kitchen := truncate(t.Kitchen, jobsPanelKitchLen)
		prompt := truncate(t.Prompt, jobsPanelPromptLen)
		lines = append(lines, fmt.Sprintf("%s %-8s %s", icon, kitchen, mutedStyle.Render(prompt)))
	}

	return panelBorder.Width(width).Render(strings.Join(lines, "\n"))
}
