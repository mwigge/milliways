package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func buildRunTargetOptions(states []KitchenState) []RunTargetOption {
	options := []RunTargetOption{
		{
			Label:      "Auto",
			Status:     "recommended",
			Reason:     "let Milliways choose",
			Selectable: true,
		},
	}
	if len(states) == 0 {
		return options
	}

	sorted := append([]KitchenState(nil), states...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, state := range sorted {
		option := RunTargetOption{
			Label:      state.Name,
			Kitchen:    state.Name,
			Status:     state.Status,
			Selectable: state.Status == "ready" || state.Status == "warning",
		}
		switch state.Status {
		case "ready":
			option.Reason = "ready"
		case "warning":
			option.Reason = fmt.Sprintf("near limit %.0f%%", state.UsageRatio*100)
		case "exhausted":
			if state.ResetsAt != "" {
				option.Reason = "exhausted until " + state.ResetsAt
			} else {
				option.Reason = "exhausted"
			}
		case "needs-auth":
			option.Reason = "needs auth"
		case "not-installed":
			option.Reason = "not installed"
		case "disabled":
			option.Reason = "disabled"
		default:
			option.Reason = state.Status
		}
		options = append(options, option)
	}
	return options
}

func RenderRunTargetChooser(options []RunTargetOption, selected int, prompt string, width int) string {
	boxWidth := 52
	if width-4 < boxWidth {
		boxWidth = width - 4
	}
	if boxWidth < 24 {
		boxWidth = 24
	}

	header := promptStyle.Render("Run In")
	subtitle := mutedStyle.Render(truncate(prompt, boxWidth-6))
	lines := []string{header, subtitle, ""}
	for i, option := range options {
		prefix := "  "
		if i == selected {
			prefix = runningStyle.Render("▶ ")
		}
		label := option.Label
		if option.Kitchen != "" {
			label = KitchenBadge(option.Kitchen)
		} else {
			label = lipgloss.NewStyle().Bold(true).Render(option.Label)
		}
		reason := option.Reason
		if !option.Selectable && option.Status != "recommended" {
			reason = mutedStyle.Render(reason)
		}
		line := fmt.Sprintf("%s%s  %s", prefix, label, reason)
		if !option.Selectable && option.Kitchen != "" {
			line = mutedStyle.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", mutedStyle.Render("Enter launch · Esc cancel"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#F59E0B")).
		Width(boxWidth).
		Render(strings.Join(lines, "\n"))
}
