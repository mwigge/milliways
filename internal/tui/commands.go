package tui

import (
	"fmt"
	"strings"
)

const projectCommandLabelWidth = 13

// HandleProjectCommand handles /project command output.
func (m *Model) HandleProjectCommand() string {
	state := m.projectState
	repoName := defaultProjectValue(state.RepoName, "(none)")
	lines := []string{
		"Project: " + repoName,
		strings.Repeat("─", 69),
		formatProjectCommandLine("Repository:", defaultProjectValue(state.RepoRoot, ".")),
		formatProjectCommandLine("Remote:", defaultProjectValue(state.RemoteURL, "(none)")),
		formatProjectCommandLine("Branch:", defaultProjectValue(state.Branch, "(detached)")),
		formatProjectCommandLine("CodeGraph:", fmt.Sprintf("%s symbols", formatCount(state.CodeGraphSymbols))),
		formatProjectCommandIndented("Last indexed: " + defaultCodeGraphIndexedAt(state)),
		formatProjectCommandLine("Palace:", formatPalaceSummary(state)),
		formatProjectCommandIndented(defaultProjectValue(state.PalacePath, "(none)")),
		formatProjectCommandLine("Access:", fmt.Sprintf("read: %s | write: %s", defaultProjectValue(state.AccessReadRule, "unknown"), defaultProjectValue(state.AccessWriteRule, "unknown"))),
	}

	return strings.Join(lines, "\n")
}

// HandlePalaceCommand handles /palace [init|search <query>] command.
func (m *Model) HandlePalaceCommand(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return m.renderPalaceStatus()
	}

	parts := strings.Fields(args)
	switch parts[0] {
	case "init":
		return m.handlePalaceInit()
	case "search":
		if len(parts) < 2 {
			return "Missing search query. Usage: /palace search <query>"
		}
		return m.handlePalaceSearch(strings.Join(parts[1:], " "))
	default:
		return fmt.Sprintf("Unknown subcommand '%s'. Usage: /palace [init|search <query>]", parts[0])
	}
}

// HandleCodeGraphCommand handles /codegraph [status|reindex|search <query>] command.
func (m *Model) HandleCodeGraphCommand(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return m.renderCodeGraphStatus()
	}

	parts := strings.Fields(args)
	switch parts[0] {
	case "status":
		return m.renderCodeGraphStatus()
	case "reindex":
		return m.handleCodeGraphReindex()
	case "search":
		if len(parts) < 2 {
			return "Missing search query. Usage: /codegraph search <query>"
		}
		return m.handleCodeGraphSearch(strings.Join(parts[1:], " "))
	default:
		return fmt.Sprintf("Unknown subcommand '%s'. Usage: /codegraph [status|reindex|search <query>]", parts[0])
	}
}

func (m *Model) renderPalaceStatus() string {
	state := m.projectState
	return strings.Join([]string{
		formatProjectCommandLine("Palace:", formatPalaceSummary(state)),
		formatProjectCommandIndented(defaultProjectValue(state.PalacePath, "(none)")),
	}, "\n")
}

func (m *Model) handlePalaceInit() string {
	return "Palace init is not yet wired to MCP integration."
}

func (m *Model) handlePalaceSearch(query string) string {
	return fmt.Sprintf("Palace search for %q is not yet wired to MCP integration.", query)
}

func (m *Model) renderCodeGraphStatus() string {
	state := m.projectState
	return strings.Join([]string{
		formatProjectCommandLine("CodeGraph:", fmt.Sprintf("%s symbols", formatCount(state.CodeGraphSymbols))),
		formatProjectCommandIndented("Last indexed: " + defaultCodeGraphIndexedAt(state)),
	}, "\n")
}

func (m *Model) handleCodeGraphReindex() string {
	return "CodeGraph reindex is not yet wired to MCP integration."
}

func (m *Model) handleCodeGraphSearch(query string) string {
	return fmt.Sprintf("CodeGraph search for %q is not yet wired to MCP integration.", query)
}

func formatProjectCommandLine(label, value string) string {
	return fmt.Sprintf("%-*s%s", projectCommandLabelWidth, label, value)
}

func formatProjectCommandIndented(value string) string {
	return strings.Repeat(" ", projectCommandLabelWidth) + value
}

func formatPalaceSummary(state ProjectState) string {
	if !state.PalaceExists {
		return formatMissingPalaceHint(state)
	}
	return fmt.Sprintf("%s drawers | %s wings | %s rooms", formatCount(state.PalaceDrawers), formatCount(state.PalaceWings), formatCount(state.PalaceRooms))
}

func defaultCodeGraphIndexedAt(state ProjectState) string {
	if strings.TrimSpace(state.CodeGraphLastIndexed) != "" {
		return state.CodeGraphLastIndexed
	}
	return "indexing..."
}

func defaultProjectValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
