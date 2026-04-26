package repl

import "strings"

// InputKind identifies the supported REPL input forms.
type InputKind string

const (
	// InputPrompt represents plain prompt text.
	InputPrompt InputKind = "prompt"
	// InputCommand represents a slash command.
	InputCommand InputKind = "command"
	// InputShell represents a shell bang command.
	InputShell InputKind = "shell"
)

// Input stores one parsed REPL input.
type Input struct {
	Kind    InputKind
	Raw     string
	Command string
	Content string
}

// ParseInput parses one REPL line into command, shell, or prompt input.
func ParseInput(input string) Input {
	raw := input
	trimmed := strings.TrimSpace(input)
	parsed := Input{
		Kind:    InputPrompt,
		Raw:     raw,
		Content: trimmed,
	}

	if len(trimmed) <= 1 {
		if trimmed != "" {
			parsed.Content = trimmed
		}
		return parsed
	}

	if strings.HasPrefix(trimmed, "/") {
		body := strings.TrimSpace(trimmed[1:])
		if body == "" {
			return parsed
		}

		command, content := splitHead(body)
		return Input{
			Kind:    InputCommand,
			Raw:     raw,
			Command: command,
			Content: content,
		}
	}

	if strings.HasPrefix(trimmed, "!") {
		body := strings.TrimSpace(trimmed[1:])
		if body == "" {
			return parsed
		}

		return Input{
			Kind:    InputShell,
			Raw:     raw,
			Content: body,
		}
	}

	return parsed
}

func splitHead(input string) (string, string) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return "", ""
	}

	command := parts[0]
	if len(parts) == 1 {
		return command, ""
	}

	return command, strings.Join(parts[1:], " ")
}
