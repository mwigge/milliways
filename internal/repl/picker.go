package repl

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

const pickerMaxVisible = 12

// pickFromList renders an inline arrow-key selector in the terminal.
// Returns the selected item, or "" if the user pressed Esc/q or the input
// is not a terminal.
//
// Controls: ↑/↓ or k/j to move, Enter to confirm, Esc/q to cancel.
func pickFromList(w io.Writer, items []string, current string) string {
	if len(items) == 0 {
		return ""
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return ""
	}

	// Find initial cursor position: default to current model, else index 0.
	cursor := 0
	for i, item := range items {
		if item == current {
			cursor = i
			break
		}
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return ""
	}
	defer func() {
		// Restore terminal state; error is best-effort cleanup.
		_ = term.Restore(fd, oldState)
	}()

	// scrollOffset is the index of the first visible item.
	scrollOffset := 0
	if cursor >= pickerMaxVisible {
		scrollOffset = cursor - pickerMaxVisible + 1
	}

	render := func() {
		visible := pickerMaxVisible
		if visible > len(items) {
			visible = len(items)
		}
		for row := 0; row < visible; row++ {
			idx := scrollOffset + row
			item := items[idx]

			bullet := " "
			if item == current {
				bullet = "*"
			}

			line := fmt.Sprintf("  %s ", bullet)
			if idx == cursor {
				line = fmt.Sprintf("\x1b[7m> %s %-30s\x1b[0m\r\n", bullet, item)
			} else {
				line = fmt.Sprintf("  %s %-30s\r\n", bullet, item)
			}
			fmt.Fprint(w, line)
		}
	}

	// clearLines moves the cursor up and clears the rendered rows.
	clearLines := func(n int) {
		for i := 0; i < n; i++ {
			fmt.Fprint(w, "\x1b[1A\x1b[2K")
		}
	}

	visibleCount := func() int {
		if pickerMaxVisible < len(items) {
			return pickerMaxVisible
		}
		return len(items)
	}

	render()

	buf := make([]byte, 6)
	for {
		n, readErr := os.Stdin.Read(buf)
		if readErr != nil || n == 0 {
			clearLines(visibleCount())
			return ""
		}

		b := buf[:n]

		switch {
		case n == 1 && b[0] == 13: // Enter
			clearLines(visibleCount())
			return items[cursor]

		case n == 1 && (b[0] == 27 || b[0] == 'q'): // Esc or q
			clearLines(visibleCount())
			return ""

		case n == 1 && b[0] == 'k': // vi up
			if cursor > 0 {
				cursor--
				if cursor < scrollOffset {
					scrollOffset = cursor
				}
			}

		case n == 1 && b[0] == 'j': // vi down
			if cursor < len(items)-1 {
				cursor++
				if cursor >= scrollOffset+pickerMaxVisible {
					scrollOffset = cursor - pickerMaxVisible + 1
				}
			}

		case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'A': // arrow up
			if cursor > 0 {
				cursor--
				if cursor < scrollOffset {
					scrollOffset = cursor
				}
			}

		case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'B': // arrow down
			if cursor < len(items)-1 {
				cursor++
				if cursor >= scrollOffset+pickerMaxVisible {
					scrollOffset = cursor - pickerMaxVisible + 1
				}
			}
		}

		clearLines(visibleCount())
		render()
	}
}
