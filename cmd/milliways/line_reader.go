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

//nolint:errcheck // Interactive terminal control writes are best-effort; input errors are returned explicitly.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

var errLineInterrupt = errors.New("line input interrupted")
var errLineCancel    = errors.New("line input cancelled (queue if busy)")
var errLineAbort     = errors.New("line input aborted (stop stream)")
var lineReaderTermWidth = termWidth

type completionProvider interface {
	Complete(line string, pos int) (suffixes []string, replace int)
}

type chatLineReaderConfig struct {
	Prompt          string
	HistoryFile     string
	InterruptPrompt string
	EOFPrompt       string
	AutoComplete    completionProvider
	ControlPoll     func() (string, bool)
	OnCancel        func() error // called when ESC pressed alone (not part of CSI sequence)
	AbortStream     func()       // called when ESC pressed alone to abort the active stream
}

type chatLineReader struct {
	in              *os.File
	out             io.Writer
	prompt          string
	historyFile     string
	interruptPrompt string
	eofPrompt       string
	completer       completionProvider
	controlPoll     func() (string, bool)
	onCancel        func() error
	AbortStream     func()
	pipeReader      *bufio.Reader

	mu               sync.Mutex
	closed           bool
	active           bool
	buf              []rune
	cursor           int
	rows             int
	promptHidden     bool
	inBracketedPaste bool
	pastePreview     string
	history          []string
	histPos          int
}

func newChatLineReader(cfg chatLineReaderConfig) (*chatLineReader, error) {
	r := &chatLineReader{
		in:              os.Stdin,
		out:             os.Stdout,
		prompt:          cfg.Prompt,
		historyFile:     cfg.HistoryFile,
		interruptPrompt: cfg.InterruptPrompt,
		eofPrompt:       cfg.EOFPrompt,
		completer:       cfg.AutoComplete,
		controlPoll:     cfg.ControlPoll,
		onCancel:        cfg.OnCancel,
		AbortStream:     cfg.AbortStream,
	}
	r.loadHistory()
	r.histPos = len(r.history)
	if !term.IsTerminal(int(r.in.Fd())) {
		r.pipeReader = bufio.NewReader(r.in)
	}
	return r, nil
}

func (r *chatLineReader) SetPrompt(prompt string) {
	r.mu.Lock()
	r.prompt = prompt
	r.mu.Unlock()
}

func (r *chatLineReader) Refresh() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || !r.active {
		return
	}
	r.redrawLocked()
}

func (r *chatLineReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return r.saveHistory()
}

func (r *chatLineReader) Readline() (string, error) {
	if !term.IsTerminal(int(r.in.Fd())) {
		if r.pipeReader == nil {
			r.pipeReader = bufio.NewReader(r.in)
		}
		line, err := r.pipeReader.ReadString('\n')
		if err != nil {
			return strings.TrimRight(line, "\r\n"), err
		}
		line = strings.TrimRight(line, "\r\n")
		r.addHistory(line)
		return line, nil
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", io.EOF
	}
	r.buf = nil
	r.cursor = 0
	r.histPos = len(r.history)
	r.active = true
	r.promptHidden = false
	r.inBracketedPaste = false
	r.redrawLocked()
	r.mu.Unlock()

	oldState, err := term.MakeRaw(int(r.in.Fd()))
	if err != nil {
		return "", err
	}
	defer func() { _ = term.Restore(int(r.in.Fd()), oldState) }()
	_, _ = fmt.Fprint(r.out, "\033[?2004h")
	defer func() { _, _ = fmt.Fprint(r.out, "\033[?2004l") }()

	br := bufio.NewReaderSize(r.in, 1<<16)
	for {
		if r.controlPoll != nil {
			if line, ok := r.controlPoll(); ok {
				r.mu.Lock()
				r.active = false
				r.promptHidden = false
				r.clearPromptLocked()
				if r.inBracketedPaste {
					r.inBracketedPaste = false
					if len(r.buf) > 0 {
						line = string(r.buf) + "\n" + line
					}
				}
				r.mu.Unlock()
				return line, nil
			}
		}
		if br.Buffered() == 0 {
			ready, err := waitReadable(int(r.in.Fd()), 100*time.Millisecond)
			if err != nil {
				return "", err
			}
			if !ready {
				continue
			}
		}
		ch, _, err := br.ReadRune()
		if err != nil {
			return "", err
		}
		switch ch {
		case '\r', '\n':
			r.mu.Lock()
			if r.inBracketedPaste {
				r.insertRunesLocked([]rune{'\n'})
				r.mu.Unlock()
			} else {
				line := string(r.buf)
				r.active = false
				r.promptHidden = false
				// Clear the wrapped input display and reprint as a single
				// newline-terminated line so the full submitted text is always
				// selectable as one logical string in the terminal scrollback.
				r.clearPromptLocked()
				r.writeSubmittedLineLocked(line)
				r.mu.Unlock()
				r.addHistory(line)
				return line, nil
			}
		case 3:
			r.mu.Lock()
			r.active = false
			r.promptHidden = false
			if r.interruptPrompt != "" {
				_, _ = fmt.Fprint(r.out, "\r\n"+r.interruptPrompt+"\r\n")
			} else {
				_, _ = fmt.Fprint(r.out, "\r\n")
			}
			r.mu.Unlock()
			return "", errLineInterrupt
		case 4:
			r.mu.Lock()
			inPaste := r.inBracketedPaste
			empty := len(r.buf) == 0
			r.mu.Unlock()
			if inPaste {
				r.deleteAtCursor()
			} else {
				if empty && r.eofPrompt != "" {
					r.mu.Lock()
					_, _ = fmt.Fprint(r.out, "\r\n"+r.eofPrompt+"\r\n") //nolint:errcheck // best-effort terminal prompt before EOF
					r.mu.Unlock()
				}
				if empty {
					r.mu.Lock()
					r.active = false
					r.promptHidden = false
					r.mu.Unlock()
					return "", io.EOF
				}
			}
		case 9:
			r.applyCompletion()
		case 27:
			// ESC pressed - queue current input if stream is busy, or start of CSI sequence
			queueIt, err := r.handleEscapeOrCancel(br)
			if queueIt {
				return "", errLineCancel
			}
			if err != nil {
				return "", err
			}
		case 8, 127:
			r.backspace()
		default:
			if ch >= 32 && ch != utf8.RuneError {
				r.insertRune(ch)
			}
		}
	}
}

func waitReadable(fd int, timeout time.Duration) (bool, error) {
	ms := int(timeout / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, ms)
	if err != nil {
		if err == unix.EINTR {
			return false, nil
		}
		return false, err
	}
	return n > 0 && fds[0].Revents&unix.POLLIN != 0, nil
}

func (r *chatLineReader) writeSubmittedLineLocked(line string) {
	// Readline runs while the terminal is in raw mode. In raw mode "\n" moves
	// down but does not return to column 0, which makes the next status or
	// streamed response start under the submitted prompt.
	display := strings.ReplaceAll(line, "\n", "\r\n")
	if r.pastePreview != "" {
		display = r.pastePreview
	}
	fmt.Fprintf(r.out, "%s%s\r\n", r.prompt, display)
}

// handleEscapeOrCancel handles ESC keypress.
// Returns (true, errLineAbort) if ESC was pressed alone (abort stream).
// Returns (false, nil) if it was the start of a CSI escape sequence.
func (r *chatLineReader) handleEscapeOrCancel(br *bufio.Reader) (bool, error) {
	// Wait a short time to see if more bytes arrive (CSI sequence)
	ready, _ := waitReadable(int(r.in.Fd()), 50*time.Millisecond)
	if !ready {
		// No more input within timeout - standalone ESC pressed → abort stream
		if r.AbortStream != nil {
			r.AbortStream()
		}
		return true, errLineAbort
	}
	// More input available - this is a CSI sequence
	r.handleEscape(br)
	return false, nil
}

func (r *chatLineReader) handleEscape(br *bufio.Reader) {
	if br.Buffered() == 0 {
		return
	}
	next, _, err := br.ReadRune()
	if err != nil || next != '[' {
		return
	}
	// Read parameter bytes (0x30–0x3F) until the final byte (0x40–0x7E).
	var param strings.Builder
	for {
		ch, _, err := br.ReadRune()
		if err != nil {
			return
		}
		if ch >= 0x40 && ch <= 0x7E {
			r.handleCSI(param.String(), ch)
			return
		}
		param.WriteRune(ch)
	}
}

func (r *chatLineReader) handleCSI(param string, final rune) {
	switch {
	case final == '~' && param == "200":
		r.mu.Lock()
		r.inBracketedPaste = true
		r.mu.Unlock()
	case final == '~' && param == "201":
		r.mu.Lock()
		r.inBracketedPaste = false
		if n := len(r.buf); n > 200 || strings.Count(string(r.buf), "\n") > 2 {
			r.pastePreview = fmt.Sprintf("[pasted %d chars]", n)
		}
		r.redrawLocked()
		r.mu.Unlock()
	case final == 'A':
		r.historyMove(-1)
	case final == 'B':
		r.historyMove(1)
	case final == 'C':
		r.moveCursor(1)
	case final == 'D':
		r.moveCursor(-1)
	case final == 'H':
		r.moveCursorTo(0)
	case final == 'F':
		r.moveCursorToEnd()
	case final == '~' && param == "3":
		r.deleteAtCursor()
	}
}

func (r *chatLineReader) historyMove(delta int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.history) == 0 {
		return
	}
	next := r.histPos + delta
	if next < 0 {
		next = 0
	}
	if next > len(r.history) {
		next = len(r.history)
	}
	r.histPos = next
	r.pastePreview = ""
	if r.histPos == len(r.history) {
		r.buf = nil
	} else {
		r.buf = []rune(r.history[r.histPos])
	}
	r.cursor = len(r.buf)
	r.redrawLocked()
}

func (r *chatLineReader) applyCompletion() {
	if r.completer == nil {
		return
	}
	r.mu.Lock()
	line := string(r.buf)
	pos := r.cursor
	r.mu.Unlock()
	suffixes, _ := r.completer.Complete(line, pos)
	if len(suffixes) == 0 {
		return
	}
	if len(suffixes) == 1 {
		r.mu.Lock()
		r.insertRunesLocked([]rune(suffixes[0]))
		r.redrawLocked()
		r.mu.Unlock()
		return
	}
	common := commonPrefix(suffixes)
	if common != "" {
		r.mu.Lock()
		r.insertRunesLocked([]rune(common))
		r.redrawLocked()
		r.mu.Unlock()
		return
	}
	r.mu.Lock()
	_, _ = fmt.Fprint(r.out, "\r\n") //nolint:errcheck // best-effort terminal redraw
	for _, s := range suffixes {
		_, _ = fmt.Fprintln(r.out, s) //nolint:errcheck // best-effort terminal completion list
	}
	r.redrawLocked()
	r.mu.Unlock()
}

func (r *chatLineReader) BeginExternalOutput() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || !r.active || r.promptHidden {
		return
	}
	r.clearPromptLocked()
	r.promptHidden = true
}

func (r *chatLineReader) EndExternalOutput() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || !r.active {
		return
	}
	r.promptHidden = false
	r.redrawLocked()
}

func (r *chatLineReader) insertRune(ch rune) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pastePreview = ""
	r.insertRunesLocked([]rune{ch})
	if !r.inBracketedPaste {
		r.redrawLocked()
	}
}

func (r *chatLineReader) insertRunesLocked(values []rune) {
	if len(values) == 0 {
		return
	}
	if r.cursor < 0 {
		r.cursor = 0
	}
	if r.cursor > len(r.buf) {
		r.cursor = len(r.buf)
	}
	next := make([]rune, 0, len(r.buf)+len(values))
	next = append(next, r.buf[:r.cursor]...)
	next = append(next, values...)
	next = append(next, r.buf[r.cursor:]...)
	r.buf = next
	r.cursor += len(values)
}

func (r *chatLineReader) backspace() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cursor <= 0 || len(r.buf) == 0 {
		return
	}
	r.pastePreview = ""
	r.buf = append(r.buf[:r.cursor-1], r.buf[r.cursor:]...)
	r.cursor--
	r.redrawLocked()
}

func (r *chatLineReader) deleteAtCursor() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cursor < 0 || r.cursor >= len(r.buf) {
		return
	}
	r.pastePreview = ""
	r.buf = append(r.buf[:r.cursor], r.buf[r.cursor+1:]...)
	r.redrawLocked()
}

func (r *chatLineReader) moveCursor(delta int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.moveCursorToLocked(r.cursor + delta)
	r.redrawLocked()
}

func (r *chatLineReader) moveCursorTo(pos int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.moveCursorToLocked(pos)
	r.redrawLocked()
}

func (r *chatLineReader) moveCursorToEnd() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.moveCursorToLocked(len(r.buf))
	r.redrawLocked()
}

func (r *chatLineReader) moveCursorToLocked(pos int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(r.buf) {
		pos = len(r.buf)
	}
	r.cursor = pos
}

func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) && prefix != "" {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func (r *chatLineReader) redrawLocked() {
	if r.cursor < 0 {
		r.cursor = 0
	}
	if r.cursor > len(r.buf) {
		r.cursor = len(r.buf)
	}
	if r.promptHidden {
		return
	}

	width := lineReaderWidth()
	if r.rows <= 0 {
		r.rows = 1
	}
	r.clearPromptLocked()
	_, _ = fmt.Fprint(r.out, r.prompt) //nolint:errcheck // best-effort terminal redraw
	displayBuf := r.buf
	displayCursor := r.cursor
	if r.pastePreview != "" {
		displayBuf = []rune(r.pastePreview)
		displayCursor = len(displayBuf)
	}
	// In raw mode \n moves down without returning to column 0; use \r\n.
	_, _ = fmt.Fprint(r.out, strings.ReplaceAll(string(displayBuf), "\n", "\r\n")) //nolint:errcheck // best-effort terminal redraw

	r.rows = bufTotalRows(r.prompt, displayBuf, width)
	cursorRow, cursorCol := bufCursorPos(r.prompt, displayBuf, displayCursor, width)
	endRow := r.rows - 1
	if endRow > cursorRow {
		_, _ = fmt.Fprintf(r.out, "\033[%dA", endRow-cursorRow) //nolint:errcheck // best-effort terminal cursor movement
	}
	_, _ = fmt.Fprint(r.out, "\r") //nolint:errcheck // best-effort terminal cursor movement
	if cursorCol > 0 {
		_, _ = fmt.Fprintf(r.out, "\033[%dC", cursorCol) //nolint:errcheck // best-effort terminal cursor movement
	}
}

func (r *chatLineReader) clearPromptLocked() {
	width := lineReaderWidth()
	// Use the greater of the stored row count and the row count derived from
	// the current content. r.rows may be stale (too small) when the buffer
	// grew since the last redraw, but we also need the stored value when the
	// buffer shrank so we clear the extra rows the previous draw occupied.
	displayBuf := r.buf
	if r.pastePreview != "" {
		displayBuf = []rune(r.pastePreview)
	}
	currentRows := bufTotalRows(r.prompt, displayBuf, width)
	rows := r.rows
	if currentRows > rows {
		rows = currentRows
	}
	if rows <= 0 {
		rows = 1
	}
	cursorRow, _ := bufCursorPos(r.prompt, r.buf, r.cursor, width)
	_, _ = fmt.Fprint(r.out, "\r") //nolint:errcheck // best-effort terminal cursor movement
	if cursorRow > 0 {
		_, _ = fmt.Fprintf(r.out, "\033[%dA", cursorRow) //nolint:errcheck // best-effort terminal cursor movement
	}
	for i := 0; i < rows; i++ {
		if i > 0 {
			_, _ = fmt.Fprint(r.out, "\033[1B") //nolint:errcheck // best-effort terminal cursor movement
		}
		_, _ = fmt.Fprint(r.out, "\r\033[2K") //nolint:errcheck // best-effort terminal redraw
	}
	if rows > 1 {
		_, _ = fmt.Fprintf(r.out, "\033[%dA", rows-1) //nolint:errcheck // best-effort terminal cursor movement
	}
	_, _ = fmt.Fprint(r.out, "\r") //nolint:errcheck // best-effort terminal cursor movement
}

// bufTotalRows returns the total visual rows occupied by prompt + buf at the given terminal width.
// Embedded newlines in buf each start a fresh visual line.
func bufTotalRows(prompt string, buf []rune, width int) int {
	segments := strings.Split(string(buf), "\n")
	total := 0
	for i, seg := range segments {
		w := displayWidth(seg)
		if i == 0 {
			w += displayWidth(prompt)
		}
		total += visualRows(w, width)
	}
	return total
}

// bufCursorPos returns the (row, col) of cursor within the rendered output.
// Embedded newlines in buf[:cursor] each advance to a new visual line.
func bufCursorPos(prompt string, buf []rune, cursor int, width int) (row, col int) {
	before := string(buf[:cursor])
	segments := strings.Split(before, "\n")
	for i, seg := range segments {
		w := displayWidth(seg)
		if i == 0 {
			w += displayWidth(prompt)
		}
		if i < len(segments)-1 {
			row += visualRows(w, width)
		} else {
			r2, c2 := cursorPosition(w, width)
			row += r2
			col = c2
		}
	}
	return row, col
}

func lineReaderWidth() int {
	width := lineReaderTermWidth()
	if width < 8 {
		return 80
	}
	return width
}

func visualRows(visibleWidth, termWidth int) int {
	if termWidth <= 0 {
		termWidth = 80
	}
	if visibleWidth <= 0 {
		return 1
	}
	rows := ((visibleWidth - 1) / termWidth) + 1
	if rows < 1 {
		return 1
	}
	return rows
}

func cursorPosition(visibleWidth, termWidth int) (row, col int) {
	if termWidth <= 0 {
		termWidth = 80
	}
	if visibleWidth <= 0 {
		return 0, 0
	}
	return (visibleWidth - 1) / termWidth, ((visibleWidth - 1) % termWidth) + 1
}

func (r *chatLineReader) addHistory(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.history) == 0 || r.history[len(r.history)-1] != line {
		r.history = append(r.history, line)
	}
	r.histPos = len(r.history)
}

func (r *chatLineReader) loadHistory() {
	if r.historyFile == "" {
		return
	}
	f, err := os.Open(r.historyFile)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			line = strings.ReplaceAll(line, `\n`, "\n")
			line = strings.ReplaceAll(line, `\\`, `\`)
			r.history = append(r.history, line)
		}
	}
}

func (r *chatLineReader) saveHistory() error {
	if r.historyFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.historyFile), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(r.historyFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	start := 0
	if len(r.history) > 1000 {
		start = len(r.history) - 1000
	}
	for _, line := range r.history[start:] {
		escaped := strings.ReplaceAll(line, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, "\n", `\n`)
		if _, err := fmt.Fprintln(f, escaped); err != nil {
			return err
		}
	}
	return nil
}
