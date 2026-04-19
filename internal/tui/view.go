package tui

import (
	"fmt"
	"math"
	"sort"
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

	// Swappable side panel (right bottom).
	bottomPanelHeight := (m.height - 6) - blockListHeight
	bottomPanel := m.renderActiveSidePanel(sideWidth, bottomPanelHeight)

	// Combine panels.
	mainArea := lipgloss.JoinHorizontal(lipgloss.Top,
		outputPanel,
		lipgloss.JoinVertical(lipgloss.Left, blockListPanel, bottomPanel),
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

	// Title with project and kitchen status.
	title := titleStyle.Width(m.width).Render("Milliways")
	sections := []string{title}
	if header := m.renderProjectHeader(); header != "" {
		sections = append(sections, header)
	}
	sections = append(sections, mainArea, inputBar)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
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

// renderActiveSidePanel renders the active bottom-right swappable panel.
func (m Model) renderActiveSidePanel(width, height int) string {
	if height < 4 {
		return ""
	}

	contentHeight := height - 2
	header := fmt.Sprintf("┌─ %s ␇ ctrl+[/ctrl+] ─", m.currentSidePanelName())
	content := m.renderCurrentPanel(width, contentHeight)

	return lipgloss.JoinVertical(lipgloss.Left,
		panelBorder.Width(width).Render(header),
		panelBorder.Width(width).Height(contentHeight).Render(content),
	)
}

// renderCurrentPanel dispatches to the active side panel renderer.
func (m Model) renderCurrentPanel(width, height int) string {
	switch SidePanelMode(m.sidePanelIdx) {
	case SidePanelLedger:
		return m.renderLedgerPanel(width, height)
	case SidePanelJobs:
		return m.renderJobsPanel(width, height)
	case SidePanelCost:
		return m.renderCostPanel(width, height)
	case SidePanelRouting:
		return m.renderRoutingPanel(width, height)
	case SidePanelSystem:
		return m.renderSystemPanel(width, height)
	case SidePanelSnippets:
		return m.renderSnippetsPanel(width, height)
	case SidePanelDiff:
		return m.renderDiffPanel(width, height)
	case SidePanelCompare:
		return m.renderComparePanel(width, height)
	default:
		return mutedStyle.Render("(no panel)")
	}
}

func (m Model) currentSidePanelName() string {
	if m.sidePanelIdx < 0 || m.sidePanelIdx >= len(sidePanelNames) {
		return "Panel"
	}

	return sidePanelNames[m.sidePanelIdx]
}

// renderLedgerPanel renders the ledger panel showing completed dispatches.
func (m Model) renderLedgerPanel(width, height int) string {
	innerWidth := max(1, width-4)
	completed := 0
	for _, b := range m.blocks {
		if b.isDone() {
			completed++
		}
	}
	lines := []string{mutedStyle.Render("Ledger")}
	if completed == 0 {
		lines = append(lines, truncate("No completed dispatches yet", innerWidth))
	}

	remaining := max(0, height-len(lines))

	// Show recent completed blocks.
	count := 0
	for i := len(m.blocks) - 1; i >= 0 && count < remaining; i-- {
		b := &m.blocks[i]
		if !b.isDone() {
			continue
		}
		status := "done"
		if b.ExitCode != 0 {
			status = "failed"
		}
		dur := fmt.Sprintf("%.1fs", b.elapsed().Seconds())
		entry := fmt.Sprintf("%s %s %s %s",
			mutedStyle.Render(b.StartedAt.Format("15:04")),
			KitchenBadge(b.Kitchen),
			dur,
			StatusIcon(status),
		)
		lines = append(lines, truncate(entry, innerWidth))
		count++
	}

	remaining = max(0, height-len(lines))
	activityLimit := min(6, max(0, remaining-2))
	activity := m.runtimeActivityLinesWidth(activityLimit, innerWidth)
	if len(activity) > 0 && remaining >= 2 {
		lines = append(lines, "", mutedStyle.Render("Activity"))
		lines = append(lines, activity...)
	}

	return strings.Join(lines, "\n")
}

// renderJobsPanel renders milliways async/detached tickets in the sidebar.
func (m Model) renderJobsPanel(width, height int) string {
	if height < 4 {
		return ""
	}

	if len(m.jobTickets) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, mutedStyle.Render("Jobs"))

	promptWidth := max(1, width-20)
	maxRows := min(6, height-3)
	for i, t := range m.jobTickets {
		if i >= maxRows {
			break
		}
		prompt := t.Prompt
		if len([]rune(prompt)) > promptWidth {
			prompt = string([]rune(prompt)[:promptWidth]) + "…"
		}
		lines = append(lines, fmt.Sprintf("%s %s %s", StatusIcon(t.Status), prompt, t.Kitchen))
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderCostPanel(width, height int) string {
	if len(m.costByKitchen) == 0 {
		return mutedStyle.Render("(no cost data yet)")
	}

	innerWidth := max(1, width-4)
	kitchens := make([]string, 0, len(m.costByKitchen))
	for kitchen := range m.costByKitchen {
		kitchens = append(kitchens, kitchen)
	}
	sort.Strings(kitchens)

	lines := make([]string, 0, len(kitchens)+2)
	for _, kitchen := range kitchens {
		acc := m.costByKitchen[kitchen]
		badge := KitchenBadge(kitchen)
		usd := formatUSD(acc.TotalUSD)
		toks := fmt.Sprintf("%dK/%dK", acc.InputToks/1000, acc.OutputToks/1000)
		line := fmt.Sprintf("%s %s %s tok", badge, usd, toks)
		lines = append(lines, truncate(line, innerWidth))
	}
	lines = append(lines, "", fmt.Sprintf("Total  %s", formatUSD(m.costTotalUSD)))

	return strings.Join(lines, "\n")
}

func formatUSD(amount float64) string {
	rounded := math.Round(amount*100) / 100
	return fmt.Sprintf("$%.2f", rounded)
}

func (m Model) renderRoutingPanel(width, height int) string {
	if len(m.routingHistory) == 0 {
		return mutedStyle.Render("(no routing decisions yet)")
	}

	innerWidth := max(1, width-4)
	lines := make([]string, 0, len(m.routingHistory))
	for _, entry := range m.routingHistory {
		tierBadgeStr := TierBadge(entry.Tier)
		reason := truncate(entry.Reason, max(1, innerWidth-15))
		lines = append(lines, fmt.Sprintf("%s %s  %s", tierBadgeStr, KitchenBadge(entry.Kitchen), reason))
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderSystemPanel(width, height int) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.procStats) == 0 {
		return mutedStyle.Render("(idle)")
	}

	kitchens := make([]string, 0, len(m.procStats))
	for kitchen := range m.procStats {
		kitchens = append(kitchens, kitchen)
	}
	sort.Strings(kitchens)

	lines := make([]string, 0, len(kitchens)*2)
	for _, kitchen := range kitchens {
		proc := m.procStats[kitchen]
		cpuStr := fmt.Sprintf("%.0f%%", proc.CPU)
		memStr := fmt.Sprintf("%.0fM", proc.MemMB)
		if proc.CPU > 80 || proc.MemMB > 500 {
			cpuStr = warningStyle.Render(cpuStr)
			memStr = warningStyle.Render(memStr)
		}
		lines = append(lines,
			fmt.Sprintf("%s  PID %d", KitchenBadge(kitchen), proc.PID),
			fmt.Sprintf("CPU %s  MEM %s", cpuStr, memStr),
		)
	}

	return strings.Join(lines, "\n")
}

// TierBadge returns a styled routing tier badge.
func TierBadge(tier string) string {
	switch tier {
	case "forced":
		return badgeStyle.Render("[forced]")
	case "keyword":
		return badgeStyle.Render("[kw]")
	case "enriched":
		return badgeStyle.Render("[enr]")
	case "learned":
		return badgeStyle.Render("[lrnd]")
	case "fallback":
		return mutedStyle.Render("[fallbk]")
	default:
		return mutedStyle.Render("[" + tier + "]")
	}
}

func (m Model) renderSnippetsPanel(width, height int) string {
	return mutedStyle.Render("(snippets panel)")
}

func (m Model) renderDiffPanel(width, height int) string {
	return mutedStyle.Render("(diff panel)")
}

func (m Model) renderComparePanel(width, height int) string {
	return mutedStyle.Render("(compare panel)")
}

func (m Model) runtimeActivityLines(limit int) []string {
	return m.runtimeActivityLinesWidth(limit, 40)
}

func (m Model) runtimeActivityLinesWidth(limit, width int) []string {
	if limit <= 0 || len(m.runtimeEvents) == 0 {
		return nil
	}

	// Filter out noisy event types that have dedicated rendering elsewhere.
	skipKinds := map[string]bool{
		"provider_output":       true, // too noisy, has dedicated block rendering
		"provider_output_start": true, // same
		"provider_output_end":   true, // same
		"token_usage":           true, // too frequent, shown in telemetry
	}
	var filtered []observability.Event
	if b := m.focusedBlock(); b != nil && b.ConversationID != "" {
		for _, evt := range m.runtimeEvents {
			if evt.ConversationID == b.ConversationID && !skipKinds[evt.Kind] {
				filtered = append(filtered, evt)
			}
		}
	} else {
		for _, evt := range m.runtimeEvents {
			if !skipKinds[evt.Kind] {
				filtered = append(filtered, evt)
			}
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}

	var lines []string
	const activityTimePrefix = "15:04:05 "
	continuationPrefix := strings.Repeat(" ", len(activityTimePrefix))
	for _, evt := range filtered {
		eventLines := formatRuntimeActivityEvent(evt)
		for i, line := range eventLines {
			if i == 0 {
				lines = append(lines, fmt.Sprintf("%s %s",
					mutedStyle.Render(evt.At.Format("15:04:05")),
					truncate(line, width),
				))
				continue
			}
			lines = append(lines, continuationPrefix+truncate(line, width))
		}
	}
	return lines
}

func formatRuntimeActivityEvent(evt observability.Event) []string {
	if evt.Kind == "switch" {
		fromKitchen := strings.TrimSpace(evt.Fields["from"])
		toKitchen := strings.TrimSpace(evt.Fields["to"])
		reason := strings.TrimSpace(evt.Fields["reason"])
		if fromKitchen != "" && toKitchen != "" {
			text := fromKitchen + " → " + toKitchen
			if reason != "" {
				text += " (" + reason + ")"
			}

			lines := []string{"switch: " + text}
			for _, key := range []string{"trigger", "tier"} {
				if value := strings.TrimSpace(evt.Fields[key]); value != "" {
					lines = append(lines, key+": "+value)
				}
			}
			return lines
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
	return []string{label + ": " + text}
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
			if ks.Remaining >= 0 {
				total := ks.Remaining
				if ks.UsageRatio > 0 && ks.UsageRatio < 1 {
					total = int(math.Round(float64(ks.Remaining) / (1 - ks.UsageRatio)))
				}
				label := fmt.Sprintf("%s %d/%d", ks.Name, ks.Remaining, total)
				if ks.Trend != "" {
					label += " " + ks.Trend
				}
				parts = append(parts, successStyle.Render(label))
			} else {
				parts = append(parts, successStyle.Render(ks.Name+" ✓"))
			}
		case "exhausted":
			label := ks.Name + " ✗"
			if ks.ResetsAt != "" {
				label += " (" + ks.ResetsAt + ")"
			}
			parts = append(parts, failureStyle.Render(label))
		case "warning":
			label := fmt.Sprintf("%s ⚠ %.0f%%", ks.Name, ks.UsageRatio*100)
			if ks.Remaining >= 0 {
				label += fmt.Sprintf(" (%d left", ks.Remaining)
				if ks.Trend != "" {
					label += " " + ks.Trend
				}
				label += ")"
			}
			parts = append(parts, runningStyle.Render(label))
		default:
			// not-installed, disabled — omit from status bar
		}
	}

	return strings.Join(parts, "  ")
}

func (m Model) renderProjectHeader() string {
	if m.projectState.RepoName == "" {
		return ""
	}

	if m.width >= projectStatusCompactWidth {
		parts := []string{RenderProjectHeader(m.projectState)}
		if status := m.renderStatusBar(); status != "" {
			parts = append(parts, "Kitchen: "+status)
		}
		return panelBorder.Width(m.width - 2).Render(strings.Join(parts, "\n"))
	}

	activeKitchen := ""
	if b := m.focusedBlock(); b != nil {
		if sticky := b.stickyKitchen(); sticky != "" {
			activeKitchen = sticky + " (sticky)"
		} else {
			activeKitchen = b.Kitchen
		}
	}
	compact := RenderCompactStatus(m.projectState, activeKitchen, len(m.recentRepos.List()))
	if status := m.renderStatusBar(); status != "" {
		compact += "  " + status
	}
	return panelBorder.Width(m.width - 2).Render(compact)
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
