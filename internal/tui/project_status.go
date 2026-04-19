package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const projectStatusCompactWidth = 100

// RenderProjectHeader renders the project info in the header.
func RenderProjectHeader(state ProjectState) string {
	if state.RepoName == "" {
		return ""
	}
	return RenderFullStatus(state)
}

// RenderCompactStatus renders compact status bar format.
func RenderCompactStatus(state ProjectState, kitchenName string, reposCount int) string {
	if state.RepoName == "" {
		return ""
	}

	parts := []string{lipgloss.NewStyle().Bold(true).Render(state.RepoName)}
	parts = append(parts, compactCodeGraphStatus(state))
	parts = append(parts, compactPalaceStatus(state))
	if kitchenName != "" {
		parts = append(parts, kitchenName)
	}
	if reposCount > 0 {
		parts = append(parts, fmt.Sprintf("repos: %d", reposCount))
	}
	return strings.Join(parts, " │ ")
}

// RenderFullStatus renders full status format for wide terminals.
func RenderFullStatus(state ProjectState) string {
	if state.RepoName == "" {
		return ""
	}

	lines := []string{
		lipgloss.NewStyle().Bold(true).Render("PROJECT: " + state.RepoName),
		"├── repo: " + displayRepoRoot(state),
	}

	if state.Branch != "" {
		lines = append(lines, "├── branch: "+state.Branch)
	}
	lines = append(lines, "├── codegraph: "+formatCodeGraphStatus(state))
	lines = append(lines, "├── palace: "+formatPalaceStatus(state))
	if state.LastAccessed != "" {
		lines = append(lines, "└── last accessed: "+state.LastAccessed)
	} else {
		lines = append(lines, "└── last accessed: unknown")
	}

	return strings.Join(lines, "\n")
}

// RenderReposList renders the recent repos section.
func RenderReposList(repos []string, activeRepo string) string {
	lines := []string{"Repositories accessed this session:"}
	if len(repos) == 0 {
		lines = append(lines, mutedStyle.Render("No repositories accessed"))
		return strings.Join(lines, "\n")
	}

	for _, repo := range repos {
		marker := "○"
		status := "(cited)"
		lineStyle := mutedStyle
		if repo == activeRepo {
			marker = successStyle.Render("●")
			status = "(active)"
			lineStyle = lipgloss.NewStyle()
		}
		lines = append(lines, lineStyle.Render(fmt.Sprintf("%s %s %s", marker, repo, status)))
	}

	return strings.Join(lines, "\n")
}

func compactAvailability(label string, ok bool) string {
	if ok {
		return label + " " + successStyle.Render("✓")
	}
	return label + " " + mutedStyle.Render("✗")
}

func compactCodeGraphStatus(state ProjectState) string {
	if state.CodeGraphIndexing {
		return "codegraph " + runningStyle.Render("indexing...")
	}
	return compactAvailability("codegraph", state.CodeGraphExists)
}

func compactPalaceStatus(state ProjectState) string {
	if state.PalaceExists {
		return compactAvailability("palace", true)
	}
	return "palace " + mutedStyle.Render(formatMissingPalaceHint(state))
}

func formatCodeGraphStatus(state ProjectState) string {
	if state.CodeGraphIndexing {
		return runningStyle.Render("indexing...")
	}
	if !state.CodeGraphExists {
		return mutedStyle.Render("not available")
	}
	status := fmt.Sprintf("%s symbols", formatCount(state.CodeGraphSymbols))
	if state.LastAccessed != "" {
		status += " │ last accessed: " + state.LastAccessed
	}
	return status
}

func formatPalaceStatus(state ProjectState) string {
	if !state.PalaceExists {
		return mutedStyle.Render(formatMissingPalaceHint(state))
	}
	status := fmt.Sprintf("%s drawers", formatCount(state.PalaceDrawers))
	if state.PalacePath != "" {
		status += " │ " + state.PalacePath
	}
	return status
}

func formatMissingPalaceHint(state ProjectState) string {
	if strings.TrimSpace(state.RepoRoot) != "" {
		return "(none — run /palace init)"
	}
	return "(none)"
}

func displayRepoRoot(state ProjectState) string {
	if state.RepoRoot != "" {
		return state.RepoRoot
	}
	if state.RepoName != "" {
		return filepath.Join(".", state.RepoName)
	}
	return "."
}

func formatCount(count int) string {
	if count <= 0 {
		return "0"
	}
	return formatThousands(count)
}

func formatThousands(n int) string {
	plain := fmt.Sprintf("%d", n)
	if len(plain) <= 3 {
		return plain
	}

	var parts []string
	for len(plain) > 3 {
		parts = append([]string{plain[len(plain)-3:]}, parts...)
		plain = plain[:len(plain)-3]
	}
	parts = append([]string{plain}, parts...)
	return strings.Join(parts, ",")
}
