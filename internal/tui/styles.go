package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors per kitchen
	kitchenColors = map[string]lipgloss.Color{
		"claude":   lipgloss.Color("#7C3AED"), // purple
		"opencode": lipgloss.Color("#059669"), // green
		"gemini":   lipgloss.Color("#2563EB"), // blue
		"aider":    lipgloss.Color("#D97706"), // amber
		"goose":    lipgloss.Color("#DC2626"), // red
		"cline":    lipgloss.Color("#0891B2"), // cyan
	}

	// Status colors
	colorSuccess = lipgloss.Color("#10B981")
	colorFailure = lipgloss.Color("#EF4444")
	colorRunning = lipgloss.Color("#F59E0B")
	colorMuted   = lipgloss.Color("#6B7280")

	// Panel styles
	panelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F9FAFB")).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1)

	inputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F9FAFB"))

	promptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8B5CF6")).
			Bold(true)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	badgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5E7EB")).
			Background(lipgloss.Color("#374151"))

	successStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	failureStyle = lipgloss.NewStyle().
			Foreground(colorFailure)

	runningStyle = lipgloss.NewStyle().
			Foreground(colorRunning)

	warningStyle = lipgloss.NewStyle().
			Foreground(colorRunning)
)

// KitchenBadge returns a styled kitchen name badge.
func KitchenBadge(name string) string {
	color, ok := kitchenColors[name]
	if !ok {
		color = lipgloss.Color("#6B7280")
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(color).
		Padding(0, 1).
		Render(name)
}

// StatusIcon returns a colored status indicator.
func StatusIcon(status string) string {
	switch status {
	case "done", "complete", "success":
		return successStyle.Render("✓")
	case "failed", "failure":
		return failureStyle.Render("✗")
	case "running", "streaming":
		return runningStyle.Render("●")
	case "pending":
		return mutedStyle.Render("○")
	case "skipped":
		return mutedStyle.Render("⊘")
	default:
		return mutedStyle.Render("?")
	}
}
