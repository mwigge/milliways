package repl

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// ContextFragment is one injected context block.
type ContextFragment struct {
	Label   string // "@file:main.go", "@git", "@shell"
	Content string
}

// EnrichedPrompt is the result of resolving all @-references.
type EnrichedPrompt struct {
	Text      string            // original text with resolved @-tokens removed
	Fragments []ContextFragment // ordered; each runner prepends these
}

// ShellOutputBuffer is a fixed-capacity ring buffer capturing recent shell output.
// handleBash tees into it; @shell resolves from it.
type ShellOutputBuffer struct {
	mu   sync.Mutex
	data []byte
	cap  int
}

// NewShellOutputBuffer returns a ShellOutputBuffer with the given byte capacity.
func NewShellOutputBuffer(capacity int) *ShellOutputBuffer {
	return &ShellOutputBuffer{
		cap: capacity,
	}
}

// Write appends bytes, dropping oldest data when full (ring buffer semantics).
func (b *ShellOutputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.data = append(b.data, p...)
	if len(b.data) > b.cap {
		b.data = b.data[len(b.data)-b.cap:]
	}
	return len(p), nil
}

// Snapshot returns all buffered content as a string.
func (b *ShellOutputBuffer) Snapshot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

// ResolveContext scans raw for @-tokens, resolves each, returns an EnrichedPrompt.
// Unrecognised @-tokens are left in place.
// Supported tokens (case-insensitive):
//
//	@file <path>  or  @file:<path>  — read file content
//	@git          — git diff HEAD (via exec)
//	@branch       — git rev-parse --abbrev-ref HEAD
//	@shell        — shellBuf.Snapshot() (may be empty)
//
// Errors from individual token resolution (e.g. file not found, git unavailable)
// are captured as the fragment content rather than failing the whole dispatch.
// The returned error is always nil; callers should never hard-fail on it.
func ResolveContext(raw string, shellBuf *ShellOutputBuffer) (EnrichedPrompt, error) {
	tokens := strings.Fields(raw)
	var kept []string
	var fragments []ContextFragment

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		lower := strings.ToLower(tok)

		switch {
		case lower == "@git":
			content, err := runGitForContext([]string{"diff", "HEAD"})
			if err != nil {
				content = fmt.Sprintf("error: %v", err)
			}
			fragments = append(fragments, ContextFragment{Label: "@git", Content: content})

		case lower == "@branch":
			content, err := runGitForContext([]string{"rev-parse", "--abbrev-ref", "HEAD"})
			if err != nil {
				content = fmt.Sprintf("error: %v", err)
			} else {
				content = strings.TrimSpace(content)
			}
			fragments = append(fragments, ContextFragment{Label: "@branch", Content: content})

		case lower == "@shell":
			content := shellBuf.Snapshot()
			fragments = append(fragments, ContextFragment{Label: "@shell", Content: content})

		case lower == "@file":
			// Next token is the path.
			if i+1 < len(tokens) {
				i++
				path := tokens[i]
				frag := resolveFileFragment(path)
				fragments = append(fragments, frag)
			} else {
				kept = append(kept, tok)
			}

		case strings.HasPrefix(lower, "@file:"):
			path := tok[len("@file:"):]
			frag := resolveFileFragment(path)
			fragments = append(fragments, frag)

		default:
			kept = append(kept, tok)
		}
	}

	text := strings.Join(kept, " ")
	return EnrichedPrompt{
		Text:      text,
		Fragments: fragments,
	}, nil
}

// resolveFileFragment reads the file at path and returns a ContextFragment.
// On error the fragment content is the error string so dispatch is not blocked.
func resolveFileFragment(path string) ContextFragment {
	label := "@file:" + path
	data, err := os.ReadFile(path)
	if err != nil {
		return ContextFragment{Label: label, Content: fmt.Sprintf("error reading file: %v", err)}
	}
	return ContextFragment{Label: label, Content: string(data)}
}

// runGitForContext executes a git command and returns its stdout.
func runGitForContext(args []string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
