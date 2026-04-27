// Package textproc implements agent-agnostic preprocessors and
// postprocessors that the daemon applies to bytes flowing to/from a
// runner. The goal is to lift behaviour that used to live inside the
// REPL (`@`-context injection, `/apply` code-block extraction) into the
// daemon so every agent — Claude, Codex, MiniMax, Copilot, and any
// future runner — gets the same treatment without per-runner code.
package textproc

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ExpandContext takes the raw input bytes for an agent.send and returns
// them with @file/@git/@branch/@shell references expanded inline.
//
// Supported tokens (case-insensitive):
//
//	@file <relative-or-absolute-path>     — replaced with the file's contents
//	@file:<path>                          — same, colon form
//	@git                                  — replaced with `git diff HEAD` output
//	@branch                               — replaced with the current git branch name
//	@shell <command-up-to-end-of-line>    — replaced with the command's stdout
//
// Tokens are recognised at start-of-input OR after whitespace. Expansion
// happens in a single pass; nested @-expansions are NOT followed.
//
// Errors during expansion (file not found, command failure, not in a
// git repo) are reported by inserting a `[milliways: <error>]` marker
// at the token position; the rest of the prompt continues unmodified.
func ExpandContext(ctx context.Context, input []byte) []byte {
	if len(input) == 0 {
		return input
	}

	var out bytes.Buffer
	out.Grow(len(input))

	i := 0
	for i < len(input) {
		// Tokens are recognised at the start of input OR after whitespace.
		atBoundary := i == 0 || isASCIISpace(input[i-1])
		if atBoundary && input[i] == '@' {
			consumed, replacement, ok := tryExpandToken(ctx, input[i:])
			if ok {
				out.Write(replacement)
				i += consumed
				continue
			}
		}
		out.WriteByte(input[i])
		i++
	}
	return out.Bytes()
}

// tryExpandToken inspects b starting at an `@` and returns
// (bytesConsumed, replacement, true) if the token matches one of the
// supported patterns. Returns (0, nil, false) otherwise so the caller
// emits the `@` byte verbatim.
func tryExpandToken(ctx context.Context, b []byte) (int, []byte, bool) {
	if len(b) == 0 || b[0] != '@' {
		return 0, nil, false
	}

	// Identify the token name: ASCII letters after the `@`.
	end := 1
	for end < len(b) && isTokenLetter(b[end]) {
		end++
	}
	if end == 1 {
		return 0, nil, false
	}
	name := strings.ToLower(string(b[1:end]))

	switch name {
	case "git":
		return end, runGit("diff", "HEAD"), true

	case "branch":
		out := runGit("rev-parse", "--abbrev-ref", "HEAD")
		out = bytes.TrimRight(out, "\r\n")
		return end, out, true

	case "file":
		// Two forms: `@file <path>` or `@file:<path>`.
		if end < len(b) && b[end] == ':' {
			pathStart := end + 1
			pathEnd := pathStart
			for pathEnd < len(b) && !isASCIISpace(b[pathEnd]) {
				pathEnd++
			}
			if pathEnd == pathStart {
				return 0, nil, false
			}
			return pathEnd, readFileOrErr(string(b[pathStart:pathEnd])), true
		}
		// `@file <path>` form: skip exactly one whitespace then read the path.
		if end >= len(b) || (b[end] != ' ' && b[end] != '\t') {
			return 0, nil, false
		}
		pathStart := end + 1
		for pathStart < len(b) && (b[pathStart] == ' ' || b[pathStart] == '\t') {
			pathStart++
		}
		pathEnd := pathStart
		for pathEnd < len(b) && !isASCIISpace(b[pathEnd]) {
			pathEnd++
		}
		if pathEnd == pathStart {
			return 0, nil, false
		}
		return pathEnd, readFileOrErr(string(b[pathStart:pathEnd])), true

	case "shell":
		// `@shell <command...>` consumes up to the next newline.
		if end >= len(b) || (b[end] != ' ' && b[end] != '\t') {
			return 0, nil, false
		}
		cmdStart := end + 1
		for cmdStart < len(b) && (b[cmdStart] == ' ' || b[cmdStart] == '\t') {
			cmdStart++
		}
		cmdEnd := cmdStart
		for cmdEnd < len(b) && b[cmdEnd] != '\n' && b[cmdEnd] != '\r' {
			cmdEnd++
		}
		if cmdEnd == cmdStart {
			return 0, nil, false
		}
		return cmdEnd, runShell(ctx, string(b[cmdStart:cmdEnd])), true
	}

	return 0, nil, false
}

// readFileOrErr returns the file's contents or a [milliways: ...] marker.
func readFileOrErr(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return []byte(fmt.Sprintf("[milliways: %v]", err))
	}
	return data
}

// runGit runs `git <args>` in the current working directory. On error it
// returns a `[milliways: ...]` marker so the surrounding prompt is not
// destroyed.
func runGit(args ...string) []byte {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return []byte(fmt.Sprintf("[milliways: git %s: %v]", strings.Join(args, " "), err))
	}
	return out
}

// runShell executes command via `sh -c` in the current working directory.
// stdout is returned; failure becomes a `[milliways: ...]` marker.
func runShell(ctx context.Context, command string) []byte {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.Output()
	if err != nil {
		return []byte(fmt.Sprintf("[milliways: shell %q: %v]", command, err))
	}
	return out
}

// isASCIISpace returns true for ' ', '\t', '\n', '\r'. Token boundaries
// here are ASCII-only — unicode.IsSpace would over-match.
func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// isTokenLetter returns true for ASCII letters; supported token names
// (file / git / branch / shell) are all lowercase ASCII.
func isTokenLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
