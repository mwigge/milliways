## Context

Milliways runs its TUI as a Bubble Tea application with stdin/stdout connected to the terminal's file descriptors. However, these are not connected to a real pseudo-terminal (PTY). This causes two classes of problems:

1. **Login flows break**: `LoginKitchen` spawns subprocesses (claude auth login, gemini auth login, etc.) and tries to give them a TTY by passing `os.Stdin` — but `os.Stdin` in a non-PTY context is a pipe from the textarea, not a terminal. The subprocess blocks forever waiting for terminal input that never arrives.

2. **Interactive subprocesses can't work**: shell sessions, interactive config tools (opencode providers), and anything that needs `isatty()` to return true cannot function.

The reference implementation is Toad (batrachian.ai/toad), which runs its entire TUI inside a PTY. When Toad needs to run `claude auth login`, it runs it as a child of the PTY — the auth flow sees a real TTY and works correctly.

## Goals / Non-Goals

**Goals:**
- Allocate a real PTY on TUI startup so stdin/stdout/stderr are terminal file descriptors
- Make PTY optional via `--no-pty` flag for CI/headless environments
- Fix login flows by running them as PTY children when a PTY is available, or using an inline overlay when not
- Add welcome block, selection visual feedback, and codex to the login switch
- Preserve existing TUI behavior for non-PTY environments

**Non-Goals:**
- Running the PTY outside milliways (e.g., a wrapper script) — milliways owns the PTY lifecycle
- Supporting Windows (golang.org/x/term has limited Windows PTY support; mark as unsupported)
- Making every subprocess use PTY — only interactive ones (login, shell) need it

## Decisions

**1. PTY allocation in main.go before tea.NewProgram**

```go
func main() {
    pty, err := shell.NewPTY()
    if err == nil {
        defer pty.Close()
        pty.InstallAsTerminal() // setsid + TIOCSCTTY
    }
    // TUI runs with PTY as controlling terminal
    p := tea.NewProgram(...)
    ...
}
```

Rationale: Installing the PTY before creating the Tea program means all child processes (including the Bubble Tea viewport's internal state) inherit the correct terminal context. The alternative — PTY per subprocess — would require wrapping each subprocess call, which is more complex.

**2. `shell.NewPTY()` with graceful fallback**

```go
type Pty struct {
    Master     *os.File
    SlaveName  string
    Slave      *os.File
    isPTY      bool
}

func NewPTY() (*Pty, error) {
    master, slave, err := term.NewPTY(256, 256)
    if err != nil {
        return &Pty{isPTY: false}, err // non-fatal
    }
    return &Pty{Master: master, Slave: slave, SlaveName: slave.Name(), isPTY: true}, nil
}
```

Rationale: `term.NewPTY` (from golang.org/x/term) abstracts the syscall-level `openpty`/`ioctl` calls across macOS and Linux. Returning `isPTY: false` with no error lets the TUI continue without a PTY; callers check `preexisting.IsPTY()` to decide between PTY-subprocess and overlay-based login.

**3. Login overlay as primary for TUI-mode logins**

`LoginKitchen` is never called in the TUI path. Instead, `/login` in the TUI renders a `loginOverlay` view using Bubble Tea's overlay system. The overlay shows:
- Kitchen list with auth status (from `Diagnose`)
- For API-key kitchens: inline masked text input → `UpdateKitchenAuth`
- For OAuth kitchens: "Open browser" button (calls `exec.Command("open", url)`) or shows URL + code entry field
- For PTY-available: "Open interactive login" button that spawns in PTY shell

Rationale: This is the cleanest fix without requiring a full PTY for every environment. The overlay approach is also more user-friendly — no subprocess flashes across the screen.

**4. PTY resize via SIGWINCH handling**

When the terminal resizes, Bubble Tea's viewport receives `tea.WindowSizeMsg`. We forward the new size to the PTY master via:

```go
func (p *Pty) Resize(cols, rows int) error {
    return term.SetSize(p.Master.Fd(), cols, rows)
}
```

Rationale: Bubble Tea already handles window size changes and updates the viewport dimensions. Forwarding to the PTY master ensures that PTY child processes (e.g., opencode providers during login) see the correct terminal size.

**5. Install PTY as controlling terminal**

```go
func (p *Pty) InstallAsTerminal() error {
    // Make the slave the controlling terminal of this process group
    // setsid creates a new session, slave becomes its controlling terminal
    syscall.Setsid()
    // TIOCSCTTY claims the terminal
    _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, p.Slave.Fd(), syscall.TIOCSCTTY, 0)
    if errno != 0 {
        return errno
    }
    // Wire stdio to slave
    syscall.Dup2(int(p.Slave.Fd()), 0)
    syscall.Dup2(int(p.Slave.Fd()), 1)
    syscall.Dup2(int(p.Slave.Fd()), 2)
    return nil
}
```

Rationale: `setsid` + `TIOCSCTTY` is the standard way to install a PTY as the controlling terminal. `Dup2` redirects stdin/stdout/stderr to the slave FD so the process and its children see a terminal.

## Risks / Trade-offs

- [Risk] PTY allocation can fail silently on some Linux setups (e.g., no `/dev/pts` access in containers)
  → Mitigation: `NewPTY` returns `isPTY: false` — milliways continues without PTY and falls back to overlay logins
- [Risk] Installing PTY as controlling terminal (`Setsid` + `TIOCSCTTY`) requires the process not to already have a controlling terminal
  → Mitigation: On macOS and in typical terminal emulators, this works. In tmux/screen, it may fail — detect and log a warning.
- [Risk] Not all Bubble Tea components handle PTY correctly — textarea and viewport use stdin directly
  → Mitigation: Bubble Tea's viewport already reads from stdin in a PTY-aware way. The main concern is mouse events and key handling, which work correctly in a PTY.
- [Risk] Adding `golang.org/x/term` as a dependency
  → Mitigation: Already transitively available via `charmbracelet/bubbletea` and its `teacup` dependency. Check `go.mod` before adding explicit import.

## Migration Plan

1. **Phase 1 — PTY infrastructure** (`internal/shell/pty.go`): Implement `NewPTY`, `Pty.Start`, `Pty.Resize`, `Pty.Close`, `IsPTY`. Add to `main.go` before `tea.NewProgram`. Add `--no-pty` flag.
2. **Phase 2 — Login overlay**: Refactor `LoginKitchen` to return an overlay-friendly result (`NeedsOverlay`, `NeedsPTY`, `NeedsEnvVar`). Implement `loginOverlay` view in `internal/tui/`.
3. **Phase 3 — codex, welcome block, selection**: Add codex to switch. Add welcome block in `Init()`. Add mouse selection visual feedback in viewport rendering.
4. **Phase 4 — CI verification**: Run `go build ./...`, `go test ./...`, `go vet ./...` in CI without PTY. Test that `--no-pty` flag works.

## Open Questions

1. How does the OAuth callback URL get handled? We need a temporary HTTP server on a high port to receive the auth code from the provider's redirect. Is this acceptable for a CLI tool?
2. For `opencode providers` — does it work as a PTY subprocess? We should test this before committing to the PTY approach for interactive TUI logins.
3. Should the PTY wrap the entire TUI (like Toad) or only the login/shell subprocesses? The current design wraps the whole TUI — simpler but more invasive.