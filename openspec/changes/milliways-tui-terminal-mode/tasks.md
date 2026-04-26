# Tasks — milliways-tui-terminal-mode

## Sign-off criteria

- `go build ./...` passes
- `go test ./...` passes
- `go vet ./...` passes
- Manual:
  - `milliways --tui` shows welcome block on startup
  - `/login` shows login overlay with all kitchens
  - `/login minimax` prompts for API key inline in overlay (no subprocess)
  - `/login codex` works (shows OAuth flow or PTY subprocess)
  - Mouse selection in viewport shows visual highlight
  - `milliways --tui --no-pty` starts without PTY, login falls back to overlay
  - PTY resize works when terminal window is resized

---

## PTY-1: PTY package — NewPTY, Pty struct, Start, Resize, Close

**Files**: `internal/shell/pty.go`, `internal/shell/pty_test.go`

```go
package shell

import (
    "os"
    "golang.org/x/term"
)

// Pty wraps a master/slave PTY pair.
type Pty struct {
    Master    *os.File
    SlaveName string
    Slave     *os.File
    isPTY     bool
}

// NewPTY allocates a new PTY pair. Returns Pty with isPTY=true on success,
// or Pty with isPTY=false on platforms where PTY is not available
// (Windows, some containers, CI environments).
func NewPTY() (*Pty, error)

// IsPTY returns whether the PTY was successfully allocated.
func (p *Pty) IsPTY() bool

// Start runs cmd with stdin/stdout/stderr connected to the slave PTY.
func (p *Pty) Start(cmd *exec.Cmd) error

// Resize sets the terminal dimensions on the master.
func (p *Pty) Resize(cols, rows int) error

// Close releases both file descriptors.
func (p *Pty) Close() error
```

Implementation notes:
- Use `term.NewPTY(width, height)` from `golang.org/x/term` for cross-platform support
- `Pty.Start`: set `cmd.Stdout/cmd.Stderr/cmd.Stdin = slave`, `cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}` (to create new session), then `cmd.Start()`
- `Pty.Resize`: use `term.SetSize(master.Fd(), cols, rows)`
- Add unit test: NewPTY returns isPTY=true on Linux/macOS; test Resize error handling

**Verify**: `go build ./internal/shell/...`

---

## PTY-2: Install PTY as controlling terminal

**Files**: `internal/shell/pty.go` (extend), `internal/shell/pty_test.go`

```go
// InstallAsTerminal installs the slave PTY as the controlling terminal
// of the current process. This must be called before creating the Bubble Tea program
// so that child processes see a real TTY.
func (p *Pty) InstallAsTerminal() error
```

Implementation notes:
- Call `syscall.Setsid()` to create a new session — the slave becomes its controlling terminal
- Call `syscall.Syscall(syscall.SYS_IOCTL, p.Slave.Fd(), syscall.TIOCSCTTY, 0)` to claim the terminal
- Use `syscall.Dup2(int(p.Slave.Fd()), 0)`, `Dup2(..., 1)`, `Dup2(..., 2)` to wire stdio
- In tests, skip if already running in a PTY (detect with `term.IsTerminal(0)`)

**Verify**: `go test ./internal/shell/...`

---

## PTY-3: PTY in main.go with --no-pty flag

**Files**: `cmd/milliways/main.go`

```go
// Add before tea.NewProgram:
noPty := flags.Lookup("no-pty") != nil
var pty *shell.Pty
if !noPty {
    pty, _ = shell.NewPTY()
    if pty != nil && pty.IsPTY() {
        defer pty.Close()
        if err := pty.InstallAsTerminal(); err != nil {
            fmt.Fprintf(os.Stderr, "warning: could not install PTY: %v\n", err)
        }
    }
}
```

Add `--no-pty` bool flag to the tui command:
```go
tuiCmd.Flags().Bool("no-pty", false, "Run TUI without PTY (for CI/headless)")
```

**Verify**: `go build ./cmd/milliways/...`

---

## LOGIN-1: loginOverlay view in TUI

**Files**: `internal/tui/login_overlay.go`, integrate into `internal/tui/app.go`

```go
// loginOverlay shows auth status for all kitchens with action buttons.
type loginOverlay struct {
    model *Model
    selectedKitchen string
    apiKeyInput    textinput.Model
    showingAPIKey   bool
}

func (m *Model) handleLoginOverlay(msg tea.Msg) (tea.Model, tea.Cmd)
```

Implementation notes:
- Add `loginOverlayActive bool` to Model
- In `Update`, handle `loginOverlayActive` case:
  - Render list of kitchens from `maitre.Diagnose` with status badges
  - Keyboard: Enter on API-key kitchen → show masked input field
  - Keyboard: Enter on OAuth kitchen → open browser or show URL
  - Escape → close overlay
- Add `case "login"` to `executePaletteCommand` that sets `loginOverlayActive = true`

**Verify**: `go build ./internal/tui/...`

---

## LOGIN-2: Inline API key input in overlay

**Files**: `internal/tui/login_overlay.go` (extend)

- Add `textinput.Model` for API key input (masked with `PasswordChar = '*'`)
- On submit: call `maitre.UpdateKitchenAuth(kitchen, apiKey)` and close overlay
- On success: show feedback in command feedback area ("API key saved for minimax")
- On failure: show error inline in overlay

**Verify**: `go test ./internal/tui/...`

---

## LOGIN-3: Add codex to LoginKitchen switch

**Files**: `internal/maitre/onboard.go`

```go
case "codex":
    return loginCLIOAuth("codex", "codex", "auth")
```

Add to the switch in `LoginKitchen`.

**Verify**: `go build ./internal/maitre/...`

---

## LOGIN-4: Non-TTY fallback in overlay

**Files**: `internal/tui/login_overlay.go` (extend)

When `!pty.IsPTY()` and user tries OAuth login:
- Show message: "Interactive login requires a PTY. Set environment variable and restart."
- Show copyable env-var command for each kitchen

**Verify**: Manual test with `milliways --tui --no-pty`

---

## WELCOME-1: Welcome block on Init

**Files**: `internal/tui/app.go` (Init change), `internal/tui/welcome.go`

```go
func (m Model) Init() tea.Cmd {
    cmds := []tea.Cmd{jobsRefreshCmd(m.ticketStore), m.startSystemMonitor(), initialOpenSpecRefreshCmd(), scheduleOpenSpecRefresh()}
    // Add welcome block
    welcomeBlock := NewWelcomeBlock(m.version, m.mode, m.kitchens, m.palaceStatus, m.codegraphStatus)
    m.blocks = append([]Block{welcomeBlock}, m.blocks...)
    m.focusedIdx = 0
    return tea.Batch(cmds...)
}
```

`NewWelcomeBlock` creates a Block with:
- Title: "Milliways v{m.version} — mode: {m.mode}"
- Content: kitchen status grid, palace/codegraph summary, keyboard hints
- Auto-collapses after first user prompt (track `welcomeShown bool` in Model)

Implementation notes:
- Add `welcomeShown bool` to Model
- In `handleKey` for Enter with non-empty input: if `!m.welcomeShown`, set `welcomeShown = true` and collapse welcome block
- On `--resume` or `--session`: skip welcome block creation

**Verify**: `go build ./internal/tui/...`

---

## SELECT-1: Mouse selection visual feedback

**Files**: `internal/tui/viewport.go` or `internal/tui/view.go`

The Bubble Tea viewport doesn't natively support selection highlighting. Approach:
- Track `selectionStart` and `selectionEnd` line indices in the viewport model
- In the viewport's `Render` method, apply a highlight style (lipgloss.Reverse() or background color) to lines within the selection range
- Mouse events in Bubble Tea: `tea.MouseMsg` with `Type == tea.MouseMotion` while `Button == tea.MouseButtonLeft`
- On mouse down: record `selectionStart`
- On mouse drag: update `selectionEnd`
- On mouse up: copy selection to clipboard via `exec.Command("pbcopy")` or `xclip`

Implementation notes:
- The viewport in `internal/tui/viewport.go` is a Bubble Tea component. We may need to wrap it or add a layer above it for selection rendering.
- Lipgloss `Reverse(true)` on a text style gives visual selection feedback.
- Simple approach: render a highlight overlay on top of the viewport lines for the selected range.

**Verify**: Manual test — drag-select text in TUI, observe highlight before yank

---

## INTEG-1: Integration test for PTY path

**Files**: `internal/shell/pty_integration_test.go` (or in `pty_test.go`)

```go
func TestPTYStart(t *testing.T) {
    pty, err := NewPTY()
    if err != nil {
        t.Skip("PTY not available")
    }
    defer pty.Close()

    cmd := exec.Command("echo", "hello from PTY")
    if err := pty.Start(cmd); err != nil {
        t.Fatalf("Start: %v", err)
    }

    out, _ := io.ReadAll(pty.Master)
    got := string(out)
    if !strings.Contains(got, "hello from PTY") {
        t.Fatalf("got %q, want containing 'hello from PTY'", got)
    }
}
```

---

## INTEG-2: CI-friendly --no-pty test

**File**: `.github/workflows/test.yml` (or existing CI)

Add a test run with `MILLIWAYS_NO_PTY=1 go test ./...` to verify non-PTY path is exercised in CI.

---

## BUILD: Final verification

After all tasks:
1. `go build ./...` — must pass
2. `go test ./...` — must pass
3. `go vet ./...` — must pass
4. Manual: `milliways --tui` shows welcome block, `/login` works, selection is visible