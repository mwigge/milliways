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
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	chromaFmt "github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// urlRe matches http and https URLs in plain text.
var urlRe = regexp.MustCompile(`https?://[^\s\x1b<>"]+`)
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func ansiEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	return term != "dumb"
}

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
// is syntax-highlighted and written as boxed lines as soon as complete
// lines arrive, so streamed responses do not hide code until the closing
// fence arrives.
//
// Thread-safety: not safe for concurrent use — drainStream is single-goroutine.
type codeHighlighter struct {
	out        io.Writer
	pending    bytes.Buffer // holds incomplete (no trailing newline) input
	tableLines []string     // accumulates a possible markdown table outside fences
	lang       string       // language extracted from the opening fence line
	codeWidth  int          // content width for the currently open code panel
	inFence    bool
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
			h.flushTable()
			h.inFence = true
			h.lang = strings.TrimSpace(strings.TrimPrefix(line, "```"))
			h.codeWidth = streamingCodePanelWidth(h.lang)
			_, _ = io.WriteString(h.out, renderCodePanelTop(h.lang, h.codeWidth))
			return
		}
		if isMarkdownTableCandidate(line) {
			h.tableLines = append(h.tableLines, line)
			return
		}
		h.flushTable()
		if rendered, ok := renderActionLine(line); ok {
			_, _ = io.WriteString(h.out, rendered)
			return
		}
		// Plain text — linkify URLs then write through immediately.
		_, _ = io.WriteString(h.out, linkifyURLs(line)+"\n")
		return
	}

	// Inside a fence.
	if line == "```" {
		// Closing fence — close the streaming code panel.
		h.inFence = false
		_, _ = io.WriteString(h.out, renderCodePanelBottom(h.codeWidth))
		h.lang = ""
		h.codeWidth = 0
		return
	}

	_, _ = io.WriteString(h.out, renderCodePanelLine(line, h.lang, h.codeWidth))
}

// Flush writes any pending line and closes any unclosed streaming code panel.
// This handles streaming truncation where the model output ends before the
// closing fence arrives.
func (h *codeHighlighter) Flush() error {
	// Write any pending partial line first.
	if h.pending.Len() > 0 {
		line := h.pending.String()
		h.pending.Reset()
		if h.inFence {
			if _, err := io.WriteString(h.out, renderCodePanelLine(line, h.lang, h.codeWidth)); err != nil {
				return err
			}
		} else if isMarkdownTableCandidate(line) {
			h.tableLines = append(h.tableLines, line)
		} else {
			h.flushTable()
			if rendered, ok := renderActionLine(line); ok {
				_, err := io.WriteString(h.out, strings.TrimSuffix(rendered, "\n"))
				if err != nil {
					return err
				}
				return nil
			}
			_, err := io.WriteString(h.out, linkifyURLs(line))
			if err != nil {
				return err
			}
		}
	}
	h.flushTable()

	if h.inFence {
		_, err := io.WriteString(h.out, renderCodePanelBottom(h.codeWidth))
		h.inFence = false
		h.lang = ""
		h.codeWidth = 0
		return err
	}

	return nil
}

func (h *codeHighlighter) flushTable() {
	if len(h.tableLines) == 0 {
		return
	}
	lines := h.tableLines
	h.tableLines = nil
	if rendered, ok := renderMarkdownTable(lines); ok {
		_, _ = io.WriteString(h.out, rendered)
		return
	}
	for _, line := range lines {
		_, _ = io.WriteString(h.out, linkifyURLs(line)+"\n")
	}
}

func isMarkdownTableCandidate(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return false
	}
	cells := parseMarkdownTableRow(line)
	return len(cells) >= 2
}

func parseMarkdownTableRow(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.Contains(line, "|") {
		return nil
	}
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func renderMarkdownTable(lines []string) (string, bool) {
	if len(lines) < 2 || !isMarkdownTableSeparator(lines[1]) {
		return "", false
	}
	header := parseMarkdownTableRow(lines[0])
	separator := parseMarkdownTableRow(lines[1])
	if len(header) == 0 || len(separator) != len(header) {
		return "", false
	}

	align := tableAlignments(separator)
	rows := make([][]string, 0, len(lines)-2)
	for _, line := range lines[2:] {
		row := parseMarkdownTableRow(line)
		if len(row) == 0 {
			continue
		}
		row = normalizeTableRow(row, len(header))
		rows = append(rows, row)
	}

	widths := make([]int, len(header))
	for i, cell := range header {
		widths[i] = displayWidth(cell)
	}
	for _, row := range rows {
		for i, cell := range row {
			if w := displayWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}

	const (
		border      = "\033[38;5;240m"
		headerStyle = "\033[1;38;5;253m"
		reset       = "\033[0m"
	)
	var b strings.Builder
	writeTableRule(&b, border, reset, "┌", "┬", "┐", widths)
	writeTableRow(&b, border, headerStyle, reset, header, widths, align)
	writeTableRule(&b, border, reset, "├", "┼", "┤", widths)
	for _, row := range rows {
		writeTableRow(&b, border, "", reset, row, widths, align)
	}
	writeTableRule(&b, border, reset, "└", "┴", "┘", widths)
	return b.String(), true
}

func isMarkdownTableSeparator(line string) bool {
	cells := parseMarkdownTableRow(line)
	if len(cells) < 2 {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			return false
		}
		stripped := strings.Trim(cell, ":")
		if len(stripped) < 3 || strings.Trim(stripped, "-") != "" {
			return false
		}
	}
	return true
}

func tableAlignments(separator []string) []string {
	align := make([]string, len(separator))
	for i, cell := range separator {
		left := strings.HasPrefix(cell, ":")
		right := strings.HasSuffix(cell, ":")
		switch {
		case left && right:
			align[i] = "center"
		case right:
			align[i] = "right"
		default:
			align[i] = "left"
		}
	}
	return align
}

func normalizeTableRow(row []string, cols int) []string {
	if len(row) > cols {
		return row[:cols]
	}
	for len(row) < cols {
		row = append(row, "")
	}
	return row
}

func writeTableRule(b *strings.Builder, border, reset, left, mid, right string, widths []int) {
	b.WriteString(border)
	b.WriteString(left)
	for i, width := range widths {
		b.WriteString(strings.Repeat("─", width+2))
		if i == len(widths)-1 {
			b.WriteString(right)
		} else {
			b.WriteString(mid)
		}
	}
	b.WriteString(reset)
	b.WriteByte('\n')
}

func writeTableRow(b *strings.Builder, border, style, reset string, cells []string, widths []int, align []string) {
	b.WriteString(border)
	b.WriteString("│")
	b.WriteString(reset)
	for i, width := range widths {
		if i >= len(cells) {
			cells = append(cells, "")
		}
		cell := alignedCell(cells[i], width, align[i])
		b.WriteByte(' ')
		if style != "" {
			b.WriteString(style)
		}
		b.WriteString(linkifyURLs(cell))
		if style != "" {
			b.WriteString(reset)
		}
		b.WriteByte(' ')
		b.WriteString(border)
		b.WriteString("│")
		b.WriteString(reset)
	}
	b.WriteByte('\n')
}

func alignedCell(cell string, width int, align string) string {
	cellWidth := displayWidth(cell)
	if cellWidth >= width {
		return cell
	}
	pad := width - cellWidth
	switch align {
	case "right":
		return strings.Repeat(" ", pad) + cell
	case "center":
		left := pad / 2
		right := pad - left
		return strings.Repeat(" ", left) + cell + strings.Repeat(" ", right)
	default:
		return cell + strings.Repeat(" ", pad)
	}
}

func displayWidth(s string) int {
	s = stripANSISequences(s)
	return utf8.RuneCountInString(s)
}

func stripANSISequences(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func renderActionLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	prefix, rest, ok := splitActionLine(trimmed)
	if !ok {
		return "", false
	}
	switch strings.ToLower(prefix) {
	case "edited":
		return renderEditedAction(rest), true
	case "ran", "run", "exec", "executed":
		return renderCommandAction(prefix, rest), true
	}
	return "", false
}

func splitActionLine(line string) (prefix, rest string, ok bool) {
	line = strings.TrimSpace(strings.TrimPrefix(line, "•"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
	if line == "" {
		return "", "", false
	}
	word, after, found := strings.Cut(line, " ")
	if !found {
		return "", "", false
	}
	switch strings.ToLower(word) {
	case "edited", "ran", "run", "exec", "executed":
		return word, strings.TrimSpace(after), true
	default:
		return "", "", false
	}
}

func renderEditedAction(rest string) string {
	const (
		reset  = "\033[0m"
		dim    = "\033[38;5;245m"
		path   = "\033[38;5;117m"
		add    = "\033[38;5;114m"
		del    = "\033[38;5;203m"
		action = "\033[1;38;5;75m"
	)
	file, stats := splitTrailingStats(rest)
	var b strings.Builder
	b.WriteString(dim)
	b.WriteString("✎ ")
	b.WriteString(action)
	b.WriteString("Edited ")
	b.WriteString(path)
	b.WriteString(file)
	if stats != "" {
		b.WriteByte(' ')
		b.WriteString(formatEditStats(stats, add, del, dim, reset))
	}
	b.WriteString(reset)
	b.WriteByte('\n')
	return b.String()
}

func splitTrailingStats(s string) (file, stats string) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, ")") {
		if i := strings.LastIndex(s, " ("); i >= 0 {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
		}
	}
	return s, ""
}

func formatEditStats(stats, add, del, dim, reset string) string {
	fields := strings.Fields(strings.Trim(stats, "()"))
	var parts []string
	for _, field := range fields {
		switch {
		case strings.HasPrefix(field, "+"):
			parts = append(parts, add+field+reset)
		case strings.HasPrefix(field, "-"):
			parts = append(parts, del+field+reset)
		default:
			parts = append(parts, dim+field+reset)
		}
	}
	if len(parts) == 0 {
		return dim + stats + reset
	}
	return dim + "(" + reset + strings.Join(parts, " ") + dim + ")" + reset
}

func renderCommandAction(prefix, rest string) string {
	const (
		reset  = "\033[0m"
		dim    = "\033[38;5;245m"
		action = "\033[1;38;5;178m"
	)
	cmd := strings.TrimSpace(rest)
	cmd = strings.Trim(cmd, "`")
	var b strings.Builder
	b.WriteString(dim)
	b.WriteString("▶ ")
	b.WriteString(action)
	b.WriteString(titleWord(prefix))
	b.WriteString(reset)
	if cmd != "" {
		b.WriteByte('\n')
		b.WriteString(renderCodePanel(cmd, "bash"))
		return b.String()
	}
	b.WriteByte('\n')
	return b.String()
}

func titleWord(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
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
	if !ansiEnabled() {
		return code
	}
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

	styleName := strings.TrimSpace(os.Getenv("MILLIWAYS_CHROMA_STYLE"))
	if styleName == "" {
		styleName = "monokai"
	}
	style := styles.Get(styleName)
	if style == nil {
		style = styles.Get("monokai")
	}
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

func renderCodePanel(code, lang string) string {
	code = strings.TrimRight(code, "\n")
	if code == "" {
		return ""
	}
	highlighted := strings.TrimRight(syntaxHighlight(code, lang), "\n")
	if highlighted == "" {
		highlighted = code
	}
	lines := strings.Split(highlighted, "\n")
	contentWidth := 0
	for _, line := range lines {
		if w := displayWidth(line); w > contentWidth {
			contentWidth = w
		}
	}
	if maxWidth := codePanelMaxContentWidth(); maxWidth > 0 && contentWidth > maxWidth {
		contentWidth = maxWidth
	}

	label := " code "
	if lang = strings.TrimSpace(lang); lang != "" {
		label = " code · " + lang + " "
	}
	if minWidth := displayWidth(label) - 2; contentWidth < minWidth {
		contentWidth = minWidth
	}

	var b strings.Builder
	b.WriteString(renderCodePanelTop(lang, contentWidth))
	for _, line := range lines {
		b.WriteString(renderHighlightedCodePanelLine(line, contentWidth))
	}
	b.WriteString(renderCodePanelBottom(contentWidth))
	return b.String()
}

func streamingCodePanelWidth(lang string) int {
	contentWidth := codePanelMaxContentWidth()
	if contentWidth <= 0 {
		contentWidth = 100
	}
	if minWidth := displayWidth(codePanelLabel(lang)) - 2; contentWidth < minWidth {
		contentWidth = minWidth
	}
	return contentWidth
}

func codePanelLabel(lang string) string {
	label := " code "
	if lang = strings.TrimSpace(lang); lang != "" {
		label = " code · " + lang + " "
	}
	return label
}

func renderCodePanelTop(lang string, contentWidth int) string {
	label := codePanelLabel(lang)
	const (
		border = "\033[38;5;238m"
		title  = "\033[2;38;5;250m"
		reset  = "\033[0m"
	)
	var b strings.Builder
	b.WriteString(border)
	b.WriteString("╭")
	b.WriteString(title)
	b.WriteString(label)
	b.WriteString(border)
	b.WriteString(strings.Repeat("─", contentWidth+2-displayWidth(label)))
	b.WriteString("╮")
	b.WriteString(reset)
	b.WriteByte('\n')
	return b.String()
}

func renderCodePanelLine(line, lang string, contentWidth int) string {
	highlighted := strings.TrimRight(syntaxHighlight(line, lang), "\n")
	if highlighted == "" && line != "" {
		highlighted = line
	}
	return renderHighlightedCodePanelLine(highlighted, contentWidth)
}

func renderHighlightedCodePanelLine(line string, contentWidth int) string {
	line = truncateANSIVisible(line, contentWidth)
	pad := contentWidth - displayWidth(line)
	const (
		border = "\033[38;5;238m"
		reset  = "\033[0m"
	)
	var b strings.Builder
	b.WriteString(border)
	b.WriteString("│")
	b.WriteString(reset)
	b.WriteByte(' ')
	b.WriteString(line)
	if pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	b.WriteByte(' ')
	b.WriteString(border)
	b.WriteString("│")
	b.WriteString(reset)
	b.WriteByte('\n')
	return b.String()
}

func renderCodePanelBottom(contentWidth int) string {
	const (
		border = "\033[38;5;238m"
		reset  = "\033[0m"
	)
	var b strings.Builder
	b.WriteString(border)
	b.WriteString("╰")
	b.WriteString(strings.Repeat("─", contentWidth+2))
	b.WriteString("╯")
	b.WriteString(reset)
	b.WriteByte('\n')
	return b.String()
}

func codePanelMaxContentWidth() int {
	if v := strings.TrimSpace(os.Getenv("MILLIWAYS_CODE_PANEL_WIDTH")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 20 {
			return n
		}
	}
	if cols := termWidth(); cols > 24 {
		return cols - 6
	}
	return 100
}

func truncateANSIVisible(s string, max int) string {
	if max <= 0 || displayWidth(s) <= max {
		return s
	}
	var b strings.Builder
	visible := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			j := i + 1
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			if j < len(s) {
				j++
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if visible+1 > max {
			break
		}
		b.WriteRune(r)
		visible++
		i += size
	}
	b.WriteString("\033[0m")
	return b.String()
}

func renderMarkdownForTerminal(text string) string {
	var out strings.Builder
	h := newCodeHighlighter(&out)
	_, _ = h.Write([]byte(text))
	_ = h.Flush()
	return out.String()
}

func writeRenderedMarkdown(out io.Writer, text string) {
	rendered := renderMarkdownForTerminal(text)
	if rendered == "" {
		return
	}
	_, _ = io.WriteString(out, rendered)
	if !strings.HasSuffix(rendered, "\n") {
		_, _ = io.WriteString(out, "\n")
	}
}

func writePrefixedRenderedMarkdown(out io.Writer, text, prefix string) {
	rendered := strings.TrimRight(renderMarkdownForTerminal(text), "\n")
	if rendered == "" {
		return
	}
	for _, line := range strings.Split(rendered, "\n") {
		_, _ = io.WriteString(out, prefix+line+"\n")
	}
}

func writeTerminalStatus(out io.Writer, line string) {
	if line == "" {
		return
	}
	if h, ok := out.(*codeHighlighter); ok {
		_, _ = io.WriteString(h.out, line+"\n")
		return
	}
	_, _ = io.WriteString(out, line+"\n")
}
