## Why

Milliways' TUI runs inside a Bubble Tea session — stdin is a textarea, not a terminal. This breaks every login flow that relies on TTY access (claude auth login, gemini auth login, codex auth, MiniMax API key prompt) and prevents interactive subprocesses like shell sessions. Additionally, codex is missing from the login switch, mouse selection has no visual feedback, and there is no welcome block on startup.

The fix is not to patch individual login calls — it is to run the TUI inside a real pseudo-terminal (PTY), the same way Toad (batrachian.ai/toad) works. Once the TUI has a real TTY, all subprocesses and interactive flows work as expected.

## What Changes

- Run the milliways TUI inside a PTY (pseudo-terminal) so stdin/stdout/stderr are real terminal file descriptors
- Add an inline login overlay to the TUI for providers that need interactive auth (claude, gemini, codex, opencode)
- Add MiniMax API key login via inline TUI input (not `term.ReadPassword`)
- Add `codex` to the `LoginKitchen` switch case
- Add visual selection feedback for mouse-driven text selection
- Add a welcome block on TUI startup showing available kitchens, mode, and keyboard hints
- Preserve the `--no-pty` flag for headless/CI environments where PTY allocation fails

## Capabilities

### New Capabilities

- `tui-pty-mode`: Run the Bubble Tea TUI inside a real PTY. The PTY becomes the controlling terminal for the process, giving child processes (login flows, shell sessions) a genuine TTY. On platforms without PTY support or in CI, fall back to a non-PTY mode.
- `login-overlay`: Replace `LoginKitchen` subprocess spawning with a TUI-native overlay. Shows auth status per kitchen, inline input for API keys, and OAuth activation buttons that open system browser. No subprocess with redirected stdin.
- `codex-auth`: Add codex to the login switch. Uses `loginCLIOAuth("codex", "codex", "auth")` when PTY is available, or shows OAuth URL + code entry when in TTY mode.
- `selection-feedback`: Visual highlight of selected text in the TUI viewport. Uses lipgloss reverse attribute on the selected range so mouse selection is clearly visible before yank.
- `welcome-block`: First block in the TUI on startup. Shows: milliways version + mode, available kitchens with status icons, keyboard shortcuts hint, palace/codegraph status if available. Auto-collapses after first user prompt.

### Modified Capabilities

- `jobs-panel` (existing): The jobs panel already has auth status display. The login overlay should reuse the same `Diagnose` output format for consistency.

## Impact

- `cmd/milliways/main.go` — PTY allocation before `tea.NewProgram`, `--no-pty` flag
- `internal/tui/app.go` — `Init()` adds welcome block, selection rendering in viewport, login overlay handler
- `internal/maitre/onboard.go` — Replace subprocess-based login with TUI overlay + API key input; add codex case
- `internal/shell/` (new) — PTY wrapper: `NewPTY()` → `* Pty { master, slave, name }`, `Pty.Start(cmd)`, `Pty.Resize(w, h)`, `Pty.Close()`
- `internal/acp/` — May need PTY integration if ACP shell sessions are to be embedded in the TUI
- `go.mod` — Add `golang.org/x/term` for PTY + terminal size (already present via bubble/teacup)