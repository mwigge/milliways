package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/observability"
)

func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	mainWidth := m.width - 28
	sideWidth := 24

	// Block-stack viewport (left panel).
	blockStack := m.renderBlockStack(mainWidth)
	m.output.SetContent(blockStack)
	outputPanel := panelBorder.
		Width(mainWidth).
		Height(m.height - 6).
		Render(m.output.View())

	// Block list panel (right top) — replaces process map.
	blockListHeight := (m.height - 6) / 2
	blockListPanel := RenderBlockList(m.blocks, m.focusedIdx, &m.queue, sideWidth, blockListHeight)

	// Ledger panel (right bottom).
	ledgerHeight := (m.height - 6) - blockListHeight
	ledgerPanel := panelBorder.
		Width(sideWidth).
		Height(ledgerHeight).
		Render(m.renderLedger())

	// Combine panels.
	mainArea := lipgloss.JoinHorizontal(lipgloss.Top,
		outputPanel,
		lipgloss.JoinVertical(lipgloss.Left, blockListPanel, ledgerPanel),
	)

	// Input at bottom — overlay-aware.
	var inputBar string
	switch {
	case m.overlayActive && m.overlayMode == OverlayRunIn:
		inputBar = RenderRunTargetChooser(m.runTargets, m.runTargetSelected, m.pendingPrompt, m.width)
	case m.overlayActive && m.overlayMode == OverlayFeedback:
		feedbackBorder := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#F59E0B")).
			Width(m.width - 2)
		inputBar = feedbackBorder.Render("Rate last dispatch: [g]ood  [b]ad  [s]kip")
	case m.overlayActive && m.overlayMode == OverlaySummary:
		inputBar = m.renderSummaryOverlay()
	case m.overlayActive && m.overlayMode == OverlayPalette:
		inputBar = RenderPalette(m.palette.Matches, m.palette.Selected, m.overlayInput.Value(), m.width)
	case m.overlayActive && m.overlayMode == OverlaySearch:
		inputBar = RenderSearch(m.search.Matches, m.search.Selected, m.overlayInput.Value(), m.width)
	case m.overlayActive:
		overlayBorder := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#F59E0B")).
			Width(m.width - 2)
		inputBar = overlayBorder.Render(m.overlayInput.View())
	default:
		inputBar = panelBorder.Width(m.width - 2).Render(m.input.View())
	}

	// Title with status bar.
	statusBar := m.renderStatusBar()
	titleText := "Milliways"
	if statusBar != "" {
		titleText += "  " + statusBar
	}
	title := titleStyle.Width(m.width).Render(titleText)

	return lipgloss.JoinVertical(lipgloss.Left, title, mainArea, inputBar)
}

// renderBlockStack renders all blocks as a vertical stack.
func (m Model) renderBlockStack(width int) string {
	if len(m.blocks) == 0 {
		return mutedStyle.Render("No dispatches yet. Type a task to begin.")
	}

	var sections []string
	for i := range m.blocks {
		b := &m.blocks[i]
		b.Focused = (i == m.focusedIdx)

		// Auto-collapse non-focused completed blocks when there are active ones.
		// (Don't auto-collapse the focused block.)
		if !b.Focused && b.isDone() && m.activeCount > 0 && len(m.blocks) > 2 {
			b.Collapsed = true
		}

		// Compute max body lines per block based on available height.
		maxBody := 0
		if b.Focused && len(m.blocks) > 1 {
			// Focused block gets more space.
			maxBody = (m.height - 6) / 2
		} else if len(m.blocks) > 3 {
			maxBody = 15
		}
		sections = append(sections, b.Render(width-4, maxBody, m.renderMode))
	}

	return strings.Join(sections, "\n")
}

// renderLedger renders the ledger panel showing completed dispatches.
func (m Model) renderLedger() string {
	completed := 0
	for _, b := range m.blocks {
		if b.isDone() {
			completed++
		}
	}
	lines := []string{mutedStyle.Render("Ledger")}
	if completed == 0 {
		lines = append(lines, mutedStyle.Render("No completed dispatches yet"))
	}

	// Show recent completed blocks (last 8).
	count := 0
	for i := len(m.blocks) - 1; i >= 0 && count < 8; i-- {
		b := &m.blocks[i]
		if !b.isDone() {
			continue
		}
		status := "done"
		if b.ExitCode != 0 {
			status = "failed"
		}
		dur := fmt.Sprintf("%.1fs", b.elapsed().Seconds())
		lines = append(lines, fmt.Sprintf("%s %s %s %s",
			mutedStyle.Render(b.StartedAt.Format("15:04")),
			KitchenBadge(b.Kitchen),
			dur,
			StatusIcon(status),
		))
		count++
	}

	activity := m.runtimeActivityLines(6)
	if len(activity) > 0 {
		lines = append(lines, "", mutedStyle.Render("Activity"))
		lines = append(lines, activity...)
	}

	return strings.Join(lines, "\n")
}

func (m Model) runtimeActivityLines(limit int) []string {
	if limit <= 0 || len(m.runtimeEvents) == 0 {
		return nil
	}

	var filtered []observability.Event
	if b := m.focusedBlock(); b != nil && b.ConversationID != "" {
		for _, evt := range m.runtimeEvents {
			if evt.ConversationID == b.ConversationID {
				filtered = append(filtered, evt)
			}
		}
	} else {
		filtered = append(filtered, m.runtimeEvents...)
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}

	var lines []string
	for _, evt := range filtered {
		label, text := formatRuntimeActivityEvent(evt)
		lines = append(lines, fmt.Sprintf("%s %s",
			mutedStyle.Render(evt.At.Format("15:04:05")),
			truncate(label+": "+text, 40),
		))
	}
	return lines
}

func formatRuntimeActivityEvent(evt observability.Event) (string, string) {
	if evt.Kind == "switch" {
		fromKitchen := strings.TrimSpace(evt.Fields["from"])
		toKitchen := strings.TrimSpace(evt.Fields["to"])
		reason := strings.TrimSpace(evt.Fields["reason"])
		if fromKitchen != "" && toKitchen != "" {
			text := fromKitchen + " → " + toKitchen
			if reason != "" {
				text += " (" + reason + ")"
			}
			return "switch", text
		}
	}

	label := evt.Kind
	if evt.Provider != "" && evt.Provider != "milliways" {
		label = evt.Provider + " " + evt.Kind
	}
	text := evt.Text
	if text == "" {
		text = evt.Kind
	}
	return label, text
}

// renderStatusBar renders kitchen availability in the title bar.
func (m Model) renderStatusBar() string {
	if len(m.kitchenStates) == 0 {
		return ""
	}

	var parts []string
	for _, ks := range m.kitchenStates {
		switch ks.Status {
		case "ready":
			parts = append(parts, successStyle.Render(ks.Name+" ✓"))
		case "exhausted":
			label := ks.Name + " ✗"
			if ks.ResetsAt != "" {
				label += " (" + ks.ResetsAt + ")"
			}
			parts = append(parts, failureStyle.Render(label))
		case "warning":
			label := fmt.Sprintf("%s ⚠ %.0f%%", ks.Name, ks.UsageRatio*100)
			parts = append(parts, runningStyle.Render(label))
		default:
			// not-installed, disabled — omit from status bar
		}
	}

	return strings.Join(parts, "  ")
}

// rateLastDispatch records a good/bad rating for the most recent completed block.
func (m *Model) rateLastDispatch(good bool) {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].isDone() {
			m.blocks[i].Rated = &good
			label := "good"
			if !good {
				label = "bad"
			}
			m.blocks[i].AppendEvent(adapter.Event{
				Type:    adapter.EventText,
				Kitchen: "milliways",
				Text:    fmt.Sprintf("[rated] %s dispatch as %s", m.blocks[i].Kitchen, label),
			})
			break
		}
	}
}

// renderSummaryOverlay renders the Ctrl+S session summary.
func (m Model) renderSummaryOverlay() string {
	totalDispatches := 0
	kitchenCounts := make(map[string]int)
	var totalDuration time.Duration
	totalCost := 0.0
	successCount := 0

	for _, b := range m.blocks {
		if !b.isDone() {
			continue
		}
		totalDispatches++
		kitchenCounts[b.Kitchen]++
		totalDuration += b.elapsed()
		if b.Cost != nil {
			totalCost += b.Cost.USD
		}
		if b.ExitCode == 0 {
			successCount++
		}
	}

	lines := []string{
		"Session Summary",
		"",
		fmt.Sprintf("Dispatches: %d", totalDispatches),
	}

	for name, count := range kitchenCounts {
		lines = append(lines, fmt.Sprintf("  %s: %d", name, count))
	}

	lines = append(lines,
		fmt.Sprintf("Duration:   %.1fs", totalDuration.Seconds()),
		fmt.Sprintf("Success:    %d/%d", successCount, totalDispatches),
	)
	if totalCost > 0 {
		lines = append(lines, fmt.Sprintf("Cost:       $%.2f", totalCost))
	}

	lines = append(lines, "", "[q] close")

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#6B7280")).
		Padding(1, 2).
		Width(40)
	return border.Render(strings.Join(lines, "\n"))
}
