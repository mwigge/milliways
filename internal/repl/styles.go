package repl

import "strings"

const (
	// ResetColor resets ANSI styling.
	ResetColor = "\x1b[0m"
	// PrimaryColor highlights primary REPL content.
	PrimaryColor = "\x1b[38;5;141m"
	// SecondaryColor highlights secondary REPL content.
	SecondaryColor = "\x1b[38;5;110m"
	// MutedColor highlights subdued REPL content.
	MutedColor = "\x1b[38;5;244m"
	// ErrorColor highlights errors.
	ErrorColor = "\x1b[38;5;203m"
	// WarningColor highlights warnings.
	WarningColor = "\x1b[38;5;221m"
	// ClaudeAccentColor highlights claude runner output.
	ClaudeAccentColor = "\x1b[38;5;177m"
	// CodexAccentColor highlights codex runner output.
	CodexAccentColor = "\x1b[38;5;81m"
	// MiniMaxAccentColor highlights minimax runner output.
	MiniMaxAccentColor = "\x1b[38;5;212m"
)

// PrimaryText wraps text with the primary REPL ANSI color.
func PrimaryText(text string) string {
	return colorize(PrimaryColor, text)
}

// SecondaryText wraps text with the secondary REPL ANSI color.
func SecondaryText(text string) string {
	return colorize(SecondaryColor, text)
}

// MutedText wraps text with the muted REPL ANSI color.
func MutedText(text string) string {
	return colorize(MutedColor, text)
}

// ErrorText wraps text with the error ANSI color.
func ErrorText(text string) string {
	return colorize(ErrorColor, text)
}

// WarningText wraps text with the warning ANSI color.
func WarningText(text string) string {
	return colorize(WarningColor, text)
}

// RunnerAccentText wraps text with a runner-specific ANSI accent color.
func RunnerAccentText(runner, text string) string {
	return colorize(RunnerAccentColor(runner), text)
}

// RunnerAccentColor returns the ANSI accent color for a runner.
func RunnerAccentColor(runner string) string {
	switch strings.ToLower(strings.TrimSpace(runner)) {
	case "claude":
		return ClaudeAccentColor
	case "codex":
		return CodexAccentColor
	case "minimax":
		return MiniMaxAccentColor
	default:
		return SecondaryColor
	}
}

func colorize(color, text string) string {
	return color + text + ResetColor
}
