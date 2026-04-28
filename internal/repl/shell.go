package repl

import (
	"context"
	"fmt"
	"io"
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
	return s.panes[s.active].Run(ctx)
}
