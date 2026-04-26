package repl

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// Shell is the top-level terminal surface manager.
//
// Phase 1 (current): manages a single active Pane with direct I/O to os.Stdout.
// Future: N panes, alternate-screen buffer, tab bar, PTY ownership via PTYManager.
type Shell struct {
	panes  []Pane
	active int
	stdout io.Writer
	stderr io.Writer
}

// NewShell creates a Shell with the given output streams.
func NewShell(stdout, stderr io.Writer) *Shell {
	return &Shell{stdout: stdout, stderr: stderr}
}

// AddPane appends a pane. The first pane added becomes the active one.
func (s *Shell) AddPane(p Pane) {
	s.panes = append(s.panes, p)
}

// Run executes the active pane's event loop.
func (s *Shell) Run(ctx context.Context) error {
	if len(s.panes) == 0 {
		return fmt.Errorf("shell: no panes registered")
	}

	// Try to set up persistent status bar. Fails silently on non-TTY.
	sb, err := NewStatusBar()
	if err == nil {
		sb.Start()
		defer sb.Close()

		// Handle terminal resize.
		winch := make(chan os.Signal, 1)
		signal.Notify(winch, syscall.SIGWINCH)
		go func() {
			for range winch {
				if rows, cols, sizeErr := term.GetSize(int(sb.Fd())); sizeErr == nil {
					sb.Resize(rows, cols)
				}
			}
		}()
		defer signal.Stop(winch)

		// Wire status bar to REPL if the active pane is a REPLPane.
		if pane, ok := s.panes[s.active].(*REPLPane); ok {
			pane.repl.SetStatusBar(sb)
		}
	}

	return s.panes[s.active].Run(ctx)
}
