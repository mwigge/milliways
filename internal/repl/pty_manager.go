package repl

import (
	"context"
	"io"
	"os/exec"
)

// PTYManager allocates and manages pseudo-terminal pairs.
//
// Phase 1: milliways uses package-level runPTY/runPTYWithContext for runner
// login/logout flows. PTYManager is not yet wired.
//
// Future (6beta+): Shell owns a PTYManager to multiplex child processes onto
// virtual panes, enabling milliways to become a true terminal emulator.
type PTYManager interface {
	// Spawn starts cmd in a new PTY and returns its read/write/close handle.
	Spawn(ctx context.Context, cmd *exec.Cmd) (io.ReadWriteCloser, error)
}
