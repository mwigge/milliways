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

import (
	"fmt"
	"os"
	"regexp"
	"sync"

	"golang.org/x/term"
)

// ansiEscapePattern matches ANSI escape sequences for stripping.
var ansiEscapePattern = regexp.MustCompile(`\x1b(?:\[[0-9;]*[mKHJrsu]|[78])`)

// StatusBar renders a persistent one-line bar pinned to the bottom of the
// terminal via the ANSI scroll-region mechanism. The scrollable region is
// shrunk to [1, rows-1]; line `rows` is reserved for the bar and is never
// touched by terminal scrolling.
type StatusBar struct {
	mu      sync.Mutex
	tty     *os.File
	content string // current bar content (may include ANSI color codes)
	rows    int
	cols    int
	active  bool
}

// NewStatusBar opens /dev/tty and queries the terminal size.
// Returns an error if the output is not a TTY or the size cannot be determined.
func NewStatusBar() (*StatusBar, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("statusbar: open /dev/tty: %w", err)
	}
	sb, err := newStatusBarForTTY(tty)
	if err != nil {
		tty.Close()
		return nil, err
	}
	return sb, nil
}

// newStatusBarForTTY creates a StatusBar using an already-open file.
// Returns an error if the file is not a TTY or size cannot be determined.
func newStatusBarForTTY(f *os.File) (*StatusBar, error) {
	cols, rows, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return nil, fmt.Errorf("statusbar: get terminal size: %w", err)
	}
	if rows <= 1 {
		return nil, fmt.Errorf("statusbar: terminal too small (%d rows)", rows)
	}
	return &StatusBar{
		tty:  f,
		rows: rows,
		cols: cols,
	}, nil
}

// Fd returns the file descriptor of the underlying TTY, suitable for use with
// term.GetSize on SIGWINCH.
func (sb *StatusBar) Fd() uintptr {
	return sb.tty.Fd()
}

// Start establishes the scroll region and reserves the last line.
// Must be called before any output is written.
func (sb *StatusBar) Start() {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	// Set scroll region to lines 1..rows-1.
	fmt.Fprintf(sb.tty, "\x1b[1;%dr", sb.rows-1)
	// Move to last row and clear it.
	fmt.Fprintf(sb.tty, "\x1b[%d;1H\x1b[2K", sb.rows)
	// Return cursor to top-left of scroll region.
	fmt.Fprint(sb.tty, "\x1b[1;1H")
	sb.active = true
}

// SetContent updates the bar text and repaints immediately.
// content may contain ANSI escape codes; visual width is estimated by
// stripping them before truncation.
func (sb *StatusBar) SetContent(content string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.content = content
	if sb.active {
		sb.repaint()
	}
}

// Resize is called when SIGWINCH fires. Re-establishes the scroll region
// and repaints with the new dimensions.
func (sb *StatusBar) Resize(rows, cols int) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.rows = rows
	sb.cols = cols
	if !sb.active {
		return
	}
	// Re-establish scroll region with new dimensions.
	fmt.Fprintf(sb.tty, "\x1b[1;%dr", sb.rows-1)
	sb.repaint()
}

// Stop clears the bar, resets the scroll region to full screen, and
// positions the cursor above the (now-empty) last line.
func (sb *StatusBar) Stop() {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.active = false
	// Reset scroll region to full screen.
	fmt.Fprint(sb.tty, "\x1b[r")
	// Clear the reserved line.
	fmt.Fprintf(sb.tty, "\x1b[%d;1H\x1b[2K", sb.rows)
	// Move cursor to just above the cleared line so the prompt lands correctly.
	fmt.Fprintf(sb.tty, "\x1b[%d;1H", sb.rows-1)
}

// Close calls Stop and closes the /dev/tty file handle.
func (sb *StatusBar) Close() {
	sb.Stop()
	sb.tty.Close()
}

// repaint renders the current content into the reserved last row.
// Must be called with sb.mu held.
func (sb *StatusBar) repaint() {
	// Save cursor (DEC).
	fmt.Fprint(sb.tty, "\x1b7")
	// Move to reserved last row, clear it.
	fmt.Fprintf(sb.tty, "\x1b[%d;1H\x1b[2K", sb.rows)
	// Write content (visually truncated to cols).
	fmt.Fprint(sb.tty, visualTruncate(sb.content, sb.cols))
	// Restore cursor.
	fmt.Fprint(sb.tty, "\x1b8")
}

// visualTruncate returns s truncated so that its visual width (ignoring ANSI
// escape sequences) does not exceed cols. If s is already shorter, it is
// returned unchanged. A trailing ResetColor is appended when truncation occurs
// and the original string contained ANSI escape codes.
func visualTruncate(s string, cols int) string {
	if cols <= 0 {
		return ""
	}

	hasANSI := ansiEscapePattern.MatchString(s)

	// Walk through the string byte-by-byte tracking visual width.
	// When we encounter an ANSI escape sequence, skip it without counting.
	visualWidth := 0
	i := 0
	for i < len(s) {
		// Check if this is the start of an ANSI escape sequence.
		if s[i] == '\x1b' {
			loc := ansiEscapePattern.FindStringIndex(s[i:])
			if loc != nil && loc[0] == 0 {
				// It's an escape sequence starting at i — skip it entirely.
				i += loc[1]
				continue
			}
		}
		// Regular character — count it.
		if visualWidth >= cols {
			// We've reached the limit; truncate here.
			truncated := s[:i]
			if hasANSI {
				truncated += ResetColor
			}
			return truncated
		}
		visualWidth++
		i++
	}

	return s
}
