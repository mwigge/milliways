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

package main

import (
	"bytes"
	"io"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromaFmt "github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// urlRe matches http and https URLs in plain text.
var urlRe = regexp.MustCompile(`https?://[^\s\x1b<>"]+`)

// linkifyURLs wraps bare URLs in OSC 8 terminal hyperlink sequences so
// wezterm (and any OSC-8-capable terminal) renders them as clickable links
// without requiring a modifier key. Text that already contains ESC sequences
// (i.e. already-highlighted code) is returned unchanged to avoid mangling ANSI.
func linkifyURLs(text string) string {
	if strings.ContainsRune(text, '\x1b') {
		return text // already has ANSI — don't double-process
	}
	return urlRe.ReplaceAllStringFunc(text, func(url string) string {
		// OSC 8 ; params ; uri ST  text  OSC 8 ;; ST
		return "\x1b]8;;" + url + "\x1b\\" + url + "\x1b]8;;\x1b\\"
	})
}

// codeHighlighter wraps an io.Writer and intercepts markdown code fences.
// Text outside fences is passed through immediately. Text inside fences
// is buffered; when the closing fence arrives the buffer is syntax-highlighted
// and written as a single ANSI-colored block.
//
// Thread-safety: not safe for concurrent use — drainStream is single-goroutine.
type codeHighlighter struct {
	out     io.Writer
	pending bytes.Buffer // holds incomplete (no trailing newline) input
	buf     bytes.Buffer // accumulates lines inside an open code fence
	lang    string       // language extracted from the opening fence line
	inFence bool
}

// newCodeHighlighter returns a codeHighlighter that writes to out.
func newCodeHighlighter(out io.Writer) *codeHighlighter {
	return &codeHighlighter{out: out}
}

// Write implements io.Writer. It processes p line by line:
//   - complete lines (ending with '\n') are dispatched immediately
//   - partial lines (no trailing '\n') are held in the pending buffer
//     until the next Write completes them
//
// Write always returns len(p), nil so callers never see a short-write error.
func (h *codeHighlighter) Write(p []byte) (int, error) {
	total := len(p)
	if total == 0 {
		return 0, nil
	}

	h.pending.Write(p)

	// Process all complete lines available in pending.
	for {
		buf := h.pending.Bytes()
		idx := bytes.IndexByte(buf, '\n')
		if idx < 0 {
			break // no complete line yet — wait for more input
		}

		line := string(buf[:idx]) // line without the trailing newline
		// Consume the line + newline from pending.
		h.pending.Next(idx + 1)

		h.processLine(line)
	}

	return total, nil
}

// processLine handles a single complete line (without trailing newline).
func (h *codeHighlighter) processLine(line string) {
	if !h.inFence {
		// Check for an opening fence: three backticks, optionally followed
		// by a language identifier.
		if strings.HasPrefix(line, "```") {
			h.inFence = true
			h.lang = strings.TrimSpace(strings.TrimPrefix(line, "```"))
			h.buf.Reset()
			// Do not emit the opening fence to the output.
			return
		}
		// Plain text — linkify URLs then write through immediately.
		_, _ = io.WriteString(h.out, linkifyURLs(line)+"\n")
		return
	}

	// Inside a fence.
	if line == "```" {
		// Closing fence — highlight and flush the accumulated buffer.
		h.inFence = false
		highlighted := syntaxHighlight(h.buf.String(), h.lang)
		_, _ = io.WriteString(h.out, highlighted)
		h.buf.Reset()
		h.lang = ""
		return
	}

	// Accumulate within-fence content.
	h.buf.WriteString(line + "\n")
}

// Flush writes any content buffered inside an unclosed fence as plain text.
// This handles streaming truncation where the model output ends before the
// closing fence arrives.
func (h *codeHighlighter) Flush() error {
	// Write any pending partial line first.
	if h.pending.Len() > 0 {
		_, err := h.out.Write(h.pending.Bytes())
		h.pending.Reset()
		if err != nil {
			return err
		}
	}

	// If we are inside an open fence, emit buffered code as plain text.
	if h.inFence && h.buf.Len() > 0 {
		_, err := h.out.Write(h.buf.Bytes())
		h.buf.Reset()
		h.inFence = false
		h.lang = ""
		return err
	}

	return nil
}

// langAliases maps common markdown fence tags to their canonical chroma names.
// Chroma's own alias registry covers most cases; this map handles the shorthands
// that LLMs frequently emit but chroma doesn't register.
var langAliases = map[string]string{
	"ts":         "typescript",
	"tsx":        "tsx",
	"js":         "javascript",
	"jsx":        "jsx",
	"rs":         "rust",
	"py":         "python",
	"rb":         "ruby",
	"sh":         "bash",
	"shell":      "bash",
	"zsh":        "bash",
	"yml":        "yaml",
	"vue":        "vue",
	"dockerfile": "docker",
	"tf":         "hcl",
	"toml":       "toml",
	"toon":       "toml",
}

// syntaxHighlight applies chroma terminal256 highlighting to code using the
// monokai style. If lang is empty or unrecognised, chroma attempts auto-
// detection; if that also fails the source is returned unchanged. Errors
// during formatting fall back to the raw source so highlighting never breaks
// the stream.
func syntaxHighlight(code, lang string) string {
	canonical := strings.ToLower(strings.TrimSpace(lang))
	if alias, ok := langAliases[canonical]; ok {
		canonical = alias
	}
	lexer := lexers.Get(canonical)
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}

	formatter := chromaFmt.Get("terminal256")
	if formatter == nil {
		return code
	}

	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}

	var sb strings.Builder
	if err := formatter.Format(&sb, style, iterator); err != nil {
		return code
	}

	result := sb.String()
	if result == "" {
		return code
	}
	return result
}
