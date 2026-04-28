# Proposal — milliways-tui-vim

## Problem Statement

The milliways TUI suffers from two interacting usability problems:

**1. h/l panel cycling is broken in insert mode.**  
Pressing `h` or `l` in normal TUI usage (insert mode, `overlayActive=false`) both cycles the panel AND types the character into the input. The `Ctrl+O` → panel mode workaround blurs input, but `Esc` is overloaded — it dismisses *all* overlays, making the flow confusing and inconsistent with vim muscle memory.

**2. No standard Unix line-editing keys.**  
Modern terminal emulators (macOS Terminal, iTerm2, Alacritty) send standard ANSI sequences for `Ctrl+U` (kill line), `Ctrl+A` (bol), `Ctrl+E` (eol). These are absent from milliways, forcing users to mash Backspace to correct a typed prompt.

**3. Text selection must be manually copied.**  
In macOS Terminal, selecting text auto-copies it. milliways currently has no mouse awareness — the output viewport does not respond to mouse events at all.

## Solution

Three focused improvements that share a common theme: making milliways feel like a natural terminal:

1. **Vi mode** — replace `OverlayPanel` hack with a proper `VimMode` field. `Esc` → normal mode (blur input, enable `h`/`l` panel cycling). `i` → insert mode (focus input). Familiar, zero new keybindings for vim users.

2. **Unix line-editing keys** — `Ctrl+U` kill line, `Ctrl+A` bol, `Ctrl+E` eol. Zero learning curve for anyone who uses a terminal.

3. **Mouse select-to-copy** — enable mouse events, track selection, copy to clipboard on mouse-up. Selection auto-copied on release, matching macOS Terminal behavior.

## Scope

**In scope:**
- `internal/tui/app.go` — vi mode key handling, line-editing keys, mouse event handler
- `internal/tui/state.go` — `VimMode` type and constants
- `internal/tui/view.go` — normal mode indicator in status area
- `internal/tui/mouse.go` — new file: mouse selection state and clipboard write
- `internal/tui/model.go` — `VimMode` field added to `Model`
- `cmd/milliways/main.go` — `tea.WithMouseAllMotion()` added to program options
- `internal/tui/panels_test.go` — updated for vi mode behavior
- `internal/tui/view_test.go` — updated for normal mode indicator
- Unit tests for all new behavior

**Out of scope:**
- Vim visual mode (character/line/block selection with `v`/`V`/`Ctrl+V`)
- Vim text objects (`iw`, `aw`, `i"`, etc.)
- Mouse-based panel switching (clicking panel tabs)
- `Ctrl+W` word-kill (already partially works via textinput)

## Success Criteria

- [ ] `h`/`l` cycles panels in normal mode without typing into input
- [ ] `Esc` in insert mode enters normal mode; `i` or `Esc` in normal mode returns to insert mode
- [ ] `Ctrl+U` discards the entire input line
- [ ] `Ctrl+A` moves cursor to beginning of line; `Ctrl+E` moves to end
- [ ] Selecting text with mouse and releasing copies it to system clipboard
- [ ] All existing panel cycling tests pass (Ctrl+J/K, Alt+/[, etc.)
- [ ] No regression in non-vi-mode usage (palette, search, feedback overlays)
