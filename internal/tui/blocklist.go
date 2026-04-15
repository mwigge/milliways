package tui

import (
	"fmt"
	"strings"
)

// truncate shortens s to maxRunes, appending "…" if trimmed.
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-1]) + "…"
}

// RenderBlockList renders the block list panel for the TUI right column.
// Shows each block's state, prompt, and duration with focus indicator.
func RenderBlockList(blocks []Block, focusedIdx int, queue *taskQueue, width, height int) string {
	lines := []string{mutedStyle.Render("Blocks")}

	if len(blocks) == 0 && (queue == nil || queue.Len() == 0) {
		lines = append(lines, mutedStyle.Render("No dispatches"))
		return panelBorder.Width(width).Height(height).Render(strings.Join(lines, "\n"))
	}

	// Active and completed block count.
	activeCount := 0
	for _, b := range blocks {
		if b.IsActive() {
			activeCount++
		}
	}

	// Show recent blocks (last N that fit in height).
	maxRows := height - 4 // leave room for header, footer, borders
	if maxRows < 3 {
		maxRows = 3
	}
	start := 0
	if len(blocks) > maxRows {
		start = len(blocks) - maxRows
	}

	promptWidth := width - 14 // icon + duration + spacing
	if promptWidth < 8 {
		promptWidth = 8
	}

	for i, b := range blocks[start:] {
		idx := start + i
		icon := stateIcon(b.State)
		prompt := truncate(b.Prompt, promptWidth)

		dur := ""
		elapsed := b.elapsed()
		if elapsed > 0 {
			dur = fmt.Sprintf("%.0fs", elapsed.Seconds())
		}

		prefix := " "
		if idx == focusedIdx {
			prefix = ">"
		}

		lines = append(lines, fmt.Sprintf("%s%s %-*s %s",
			prefix, icon, promptWidth, prompt, mutedStyle.Render(dur)))
	}

	// Queue entries.
	if queue != nil && queue.Len() > 0 {
		lines = append(lines, "")
		limit := queue.Len()
		if limit > 3 {
			limit = 3
		}
		for j := 0; j < limit; j++ {
			qt := queue.items[j]
			lines = append(lines, mutedStyle.Render(
				fmt.Sprintf(" · queued: %s #%d", truncate(qt.Prompt, promptWidth-10), j+1)))
		}
		if queue.Len() > 3 {
			lines = append(lines, mutedStyle.Render(
				fmt.Sprintf(" · ...+%d more", queue.Len()-3)))
		}
	}

	// Footer.
	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render(
		fmt.Sprintf("Active: %d  Total: %d", activeCount, len(blocks))))
	if queue != nil && queue.Len() > 0 {
		lines = append(lines, mutedStyle.Render(
			fmt.Sprintf("Queued: %d", queue.Len())))
	}

	return panelBorder.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}
