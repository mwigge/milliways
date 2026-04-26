package repl

import "testing"

func TestParseInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  Input
	}{
		{
			name:  "plain prompt text",
			input: "explain the auth flow",
			want:  Input{Kind: InputPrompt, Raw: "explain the auth flow", Content: "explain the auth flow"},
		},
		{
			name:  "command with no args",
			input: "/status",
			want:  Input{Kind: InputCommand, Raw: "/status", Command: "status"},
		},
		{
			name:  "command with args and trimming",
			input: "  /switch codex  ",
			want:  Input{Kind: InputCommand, Raw: "  /switch codex  ", Command: "switch", Content: "codex"},
		},
		{
			name:  "shell bang command",
			input: " !git status ",
			want:  Input{Kind: InputShell, Raw: " !git status ", Content: "git status"},
		},
		{
			name:  "bare slash falls back to prompt",
			input: "/",
			want:  Input{Kind: InputPrompt, Raw: "/", Content: "/"},
		},
		{
			name:  "bare bang falls back to prompt",
			input: "!",
			want:  Input{Kind: InputPrompt, Raw: "!", Content: "!"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ParseInput(tt.input); got != tt.want {
				t.Fatalf("ParseInput(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestColorHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "primary helper", got: PrimaryText("ready"), want: PrimaryColor + "ready" + ResetColor},
		{name: "secondary helper", got: SecondaryText("sidecar"), want: SecondaryColor + "sidecar" + ResetColor},
		{name: "muted helper", got: MutedText("hint"), want: MutedColor + "hint" + ResetColor},
		{name: "error helper", got: ErrorText("failed"), want: ErrorColor + "failed" + ResetColor},
		{name: "warning helper", got: WarningText("careful"), want: WarningColor + "careful" + ResetColor},
		{name: "claude runner accent", got: RunnerAccentText("claude", "claude"), want: ClaudeAccentColor + "claude" + ResetColor},
		{name: "codex runner accent", got: RunnerAccentText("codex", "codex"), want: CodexAccentColor + "codex" + ResetColor},
		{name: "minimax runner accent", got: RunnerAccentText("minimax", "minimax"), want: MiniMaxAccentColor + "minimax" + ResetColor},
		{name: "unknown runner falls back to secondary", got: RunnerAccentText("unknown", "other"), want: SecondaryColor + "other" + ResetColor},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Fatalf("helper output = %q, want %q", tt.got, tt.want)
			}
		})
	}
}
