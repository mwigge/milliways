## ADDED Requirements

### Requirement: PTY allocation on startup

When `milliways --tui` starts, it SHALL attempt to allocate a pseudo-terminal pair before creating the Bubble Tea program. If PTY allocation fails (non-Linux platform, CI environment, or `--no-pty` flag), milliways SHALL fall back to operating without a PTY — all functionality works except interactive subprocesses that require a TTY.

#### Scenario: PTY allocation on Linux/macOS

- **WHEN** `milliways --tui` starts on a platform with PTY support
- **THEN** milliways SHALL allocate a master/slave PTY pair using `golang.org/x/term` / `syscall/ioctl`
- **AND** set the slave end as the controlling terminal for the process via `setsid()` + `ioctl TIOCSCTTY`
- **AND** wire stdin/stdout/stderr to the slave FD

#### Scenario: Fallback on CI or --no-pty

- **WHEN** `milliways --tui --no-pty` is invoked
- **OR** when PTY allocation returns an error
- **THEN** milliways SHALL run without a PTY (standard file descriptors)
- **AND** the TUI SHALL render correctly using the existing viewport/textarea approach
- **AND** login flows SHALL use the overlay approach instead of subprocess spawning

#### Scenario: Resize handling

- **WHEN** the terminal window is resized while the TUI is running
- **THEN** milliways SHALL detect the window size change via `SIGWINCH` or `TIOCGWINSZ`
- **AND** forward the new dimensions to the PTY master via `ioctl TIOCSWINSZ`

### Requirement: PTY wrapper API

The PTY package in `internal/shell/pty.go` SHALL expose:

```go
type Pty struct {
    Master *os.File   // master FD
    SlaveName string  // path to pts device e.g., /dev/pts/3
    Slave *os.File    // slave FD
}

// NewPTY() (*Pty, error) — allocates master/slave pair
// (p *Pty) Start(cmd *exec.Cmd) error — runs cmd with slave as stdin/stdout/stderr
// (p *Pty) Resize(cols, rows int) error — TIOCSWINSZ on master
// (p *Pty) Close() error — closes both FDs
// IsPTY() bool — true if PTY was successfully allocated
```

#### Scenario: PTY Start runs command with PTY

- **WHEN** `pty.Start(cmd)` is called with a prepared exec.Cmd
- **THEN** the command SHALL have its stdio set to the slave PTY FD
- **AND** the command SHALL be started as a subprocess
- **AND** output from the command SHALL flow through the master FD for consumption by the TUI

#### Scenario: PTY close releases resources

- **WHEN** `pty.Close()` is called
- **THEN** both master and slave file descriptors SHALL be closed
- **AND** any lingering subprocesses SHALL be signaled to terminate