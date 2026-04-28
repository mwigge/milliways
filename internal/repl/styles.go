// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package repl

import "strings"

const (
	ResetColor       = "\x1b[0m"
	BlackBackground  = "\x1b[48;2;0;0;0m"
	PromptBackground = "\x1b[48;2;0;0;0m"
	DimFG            = "\x1b[38;2;100;100;100m"

	// Claude: off-white / warm pearl — clearly distinct from the green terminal default
	ClaudeFG     = "\x1b[38;2;220;215;200m"
	ClaudeBG     = "\x1b[48;2;0;0;0m"
	ClaudeAccent = "\x1b[38;2;245;240;225m"

	// Codex: amber/orange
	CodexFG     = "\x1b[38;2;255;180;50m"
	CodexBG     = "\x1b[48;2;0;0;0m"
	CodexAccent = "\x1b[38;2;255;215;100m"

	// MiniMax: purple/violet
	MiniMaxFG     = "\x1b[38;2;190;100;255m"
	MiniMaxBG     = "\x1b[48;2;0;0;0m"
	MiniMaxAccent = "\x1b[38;2;220;160;255m"

	// Copilot: black/red
	CopilotFG     = "\x1b[38;2;255;80;80m"
	CopilotBG     = "\x1b[48;2;0;0;0m"
	CopilotAccent = "\x1b[38;2;255;80;80m"

	// Aliases for tests — map to Claude scheme
	PrimaryColor       = ClaudeFG
	SecondaryColor     = ClaudeFG
	MutedColor         = ClaudeFG
	ErrorColor         = ClaudeFG
	WarningColor       = ClaudeFG
	ClaudeAccentColor  = ClaudeAccent
	CodexAccentColor   = CodexAccent
	MiniMaxAccentColor = MiniMaxAccent
	CopilotAccentColor = CopilotAccent
)

type ColorScheme struct {
	FG     string
	Accent string
	Runner string
}

func ClaudeScheme() ColorScheme {
	return ColorScheme{FG: ClaudeFG, Accent: ClaudeAccent, Runner: "claude"}
}
func CodexScheme() ColorScheme { return ColorScheme{FG: CodexFG, Accent: CodexAccent, Runner: "codex"} }
func MiniMaxScheme() ColorScheme {
	return ColorScheme{FG: MiniMaxFG, Accent: MiniMaxAccent, Runner: "minimax"}
}
func CopilotScheme() ColorScheme {
	return ColorScheme{FG: CopilotFG, Accent: CopilotAccent, Runner: "copilot"}
}

func SchemeForRunner(name string) ColorScheme {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "claude":
		return ClaudeScheme()
	case "codex":
		return CodexScheme()
	case "minimax":
		return MiniMaxScheme()
	case "copilot":
		return CopilotScheme()
	default:
		return ClaudeScheme()
	}
}

func DefaultScheme() ColorScheme { return ClaudeScheme() }

func ColorText(scheme ColorScheme, text string) string {
	return BlackBackground + scheme.FG + text + ResetColor
}

func AccentColorText(scheme ColorScheme, text string) string {
	return BlackBackground + scheme.Accent + text + ResetColor
}

func PrimaryText(text string) string {
	return ColorText(DefaultScheme(), text)
}

func MutedText(text string) string {
	return DimFG + text + ResetColor
}

func ErrorText(text string) string {
	return ColorText(DefaultScheme(), text)
}

func WarningText(text string) string {
	return ColorText(DefaultScheme(), text)
}

func Primary(text string) string {
	return ColorText(DefaultScheme(), text)
}

func Secondary(text string) string {
	return ColorText(DefaultScheme(), text)
}

func colorize(color, text string) string {
	return color + text + ResetColor
}

func PhosphorText(text string) string {
	return ColorText(DefaultScheme(), text)
}

func PhosphorHeader(text string) string {
	return ColorText(DefaultScheme(), text)
}

func RunnerAccentColor(runner string) string {
	switch strings.ToLower(strings.TrimSpace(runner)) {
	case "claude":
		return ClaudeAccentColor
	case "codex":
		return CodexAccentColor
	case "minimax":
		return MiniMaxAccentColor
	case "copilot":
		return CopilotAccentColor
	default:
		return ClaudeAccentColor
	}
}

func RunnerAccentText(runner, text string) string {
	return BlackBackground + SchemeForRunner(runner).Accent + text + ResetColor
}
