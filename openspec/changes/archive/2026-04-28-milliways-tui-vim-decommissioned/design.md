# Design — milliways-tui-vim

## 1. Vi Mode

### Motivation

The TUI currently has `overlayActive bool` which is `true` for every overlay (palette, search, feedback, panel mode). This conflates "a modal overlay is open" with "navigation keys should not type into input". The `OverlayPanel` state was bolted on to work around this — `Ctrl+O` sets `overlayActive=true, overlayMode=OverlayPanel` and blurs the input so `h`/`l` don't type. But this is fragile: `Esc` closes *everything* including panel mode, and `h`/`l` only work after explicitly pressing `Ctrl+O`.

Vi mode replaces this with a clean separation.

### VimMode Type

```go
// internal/tui/state.go

// VimMode distinguishes insert (typing) vs normal (navigation) mode.
type VimMode int

const (
    VimInsert VimMode = iota  // default: typing enabled
    VimNormal                 // navigation enabled, input blurred
)
```

### Model Field Addition

```go
// internal/tui/app.go — Model struct
VimMode vimMode VimMode  // insert or normal (vi-style)
```

### Mode Transition Rules

```
┌──────────────────────────────────────────────────────────────────┐
│                     MODE STATE DIAGRAM                            │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  INSERT MODE (vimMode=VimInsert, overlayActive=false)              │
│  ─────────────────────────────────────────────────               │
│  • All printable keys → input                                      │
│  • Esc       → enter NORMAL MODE (overlayActive=true, blur input) │
│  • Ctrl+O    → enter NORMAL MODE (same)                          │
│  • /         → open palette (overlayActive=true, VimInsert kept)   │
│  • Ctrl+S   → open summary (overlayActive=true, VimInsert kept)    │
│  • Ctrl+F   → open feedback (overlayActive=true, VimInsert kept)   │
│  • ↑/↓      → history navigation                                  │
│                                                                  │
│  NORMAL MODE (vimMode=VimNormal, overlayActive=true)               │
│  ─────────────────────────────────────────────────               │
│  • Input is BLURRED (m.input.Blur()) — skipInputUpdate=true       │
│  • h / l    → rewindSidePanel / advanceSidePanel                  │
│  • j / k    → same as h/l (vim convention)                       │
│  • ↑ / ↓    → navigate within active panel                         │
│  • i        → enter INSERT MODE (overlayActive=false, focus input) │
│  • Esc      → enter INSERT MODE (same)                            │
│  • Enter    → if on OpenSpec course → select item                  │
│                                                                  │
│  OVERLAYS (any vimMode, overlayActive=true)                        │
│  ─────────────────────────────────────────────────               │
│  • Palette, Search, Feedback, Summary overlays take precedence      │
│  • Their key handlers run first, VimMode unchanged                 │
│  • On overlay dismiss → restore previous VimMode state              │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

### Key Handling Changes

In `handleKey(msg tea.KeyMsg) []tea.Cmd`:

```go
case tea.KeyEsc:
    if m.vimMode == VimInsert {
        m.vimMode = VimNormal
        m.overlayActive = true   // blur happens in Update()
        return nil
    }
    // In normal mode, Esc also returns to insert (existing overlay-only behavior)
    m.vimMode = VimInsert
    m.overlayActive = false
    m.input.Focus()
    return nil

case 'i':
    // Only in normal mode: enter insert mode
    if m.vimMode == VimNormal {
        m.vimMode = VimInsert
        m.overlayActive = false
        m.input.Focus()
        return nil
    }

case 'h', 'j', 'k', 'l':
    if m.vimMode == VimNormal {
        switch msg.Runes[0] {
        case 'h', 'k': m.rewindSidePanel()
        case 'l', 'j': m.advanceSidePanel()
        }
        return nil
    }
    // In insert mode: fall through to input update
```

### Update() Changes

```go
case tea.KeyMsg:
    // Compute vim-normal before handleKey so skipInputUpdate uses it
    inVimNormal := m.vimMode == VimNormal
    skipInputUpdate = inVimNormal && isSidePanelKey(m.sidePanelIdx, msg, inVimNormal)
    cmds = append(cmds, m.handleKey(msg)...)

// In the input update section:
if skipInputUpdate {
    inputCmd = nil
} else if m.overlayActive {
    // overlay (palette, search, etc.) — always update overlayInput
    m.overlayInput, inputCmd = m.overlayInput.Update(msg)
} else {
    // Normal case: update main input
    m.input, inputCmd = m.input.Update(msg)
}
```

### Overlay Dismiss → Restore VimMode

When an overlay is dismissed (palette selection, search selection, Esc from feedback), restore vim mode:

```go
case "enter":  // palette selection
    // ... execute command ...
    m.overlayActive = false
    m.overlayMode = OverlayNone
    m.palette.Active = false
    m.vimMode = VimInsert   // ← restore
    m.input.Focus()
    return nil
```

### View Changes

Normal mode indicator in the bottom input area:

```go
// internal/tui/view.go — renderInputBar()
case m.vimMode == VimNormal:
    inputBar = panelBorder.Width(m.width - 2).Render(
        lipgloss.NewStyle().
            Foreground(lipgloss.Color("#10B981")).
            Render("[N]  h/l switch panels  ↑↓ navigate  i or Esc to type"),
    )
case m.overlayActive && m.overlayMode == OverlayPanel:
    // DEPRECATED: OverlayPanel no longer used for panel mode
    // Left as-is for backward compat during transition
```

### Backward Compatibility During Transition

`OverlayPanel` mode is kept in the enum and handled by `Ctrl+O` during the transition period, but the view indicator is changed to suggest using `Esc` instead. After one release, `OverlayPanel` can be removed.

---

## 2. Unix Line-Editing Keys

All handlers go in `handleKey()`. These only apply in insert mode with `overlayActive=false`.

| Key | Action | Implementation |
|-----|--------|---------------|
| `Ctrl+U` | Kill line (clear input) | `m.input.SetValue(""); return nil` |
| `Ctrl+A` | Beginning of line | `m.input.SetCursor(0); return nil` |
| `Ctrl+E` | End of line | `m.input.SetCursor(len(m.input.Value())); return nil` |

```go
case "ctrl+u":
    if !m.overlayActive {
        m.input.SetValue("")
        return nil
    }

case "ctrl+a":
    if !m.overlayActive {
        m.input.SetCursor(0)
        return nil
    }

case "ctrl+e":
    if !m.overlayActive {
        m.input.SetCursor(len(m.input.Value()))
        return nil
    }
```

---

## 3. Mouse Select-to-Copy

### Enabling Mouse Events

```go
// cmd/milliways/main.go
p := tea.NewProgram(
    m,
    tea.WithAltScreen(),
    tea.WithMouseAllMotion(),   // ← add this
)
```

### Dependencies

```go
// internal/tui/mouse.go
import (
    tea "github.com/charmbracelet/bubbletea"
    "github.com/atotto/clipboard"
)
```

### Selection State

```go
// internal/tui/mouse.go

type mouseState struct {
    selecting     bool
    selStartRow  int
    selStartCol  int
    selEndRow    int
    selEndCol    int
    lastMouseRow int
    lastMouseCol int
}

var ms mouseState
```

### Mouse Event Handler

```go
// internal/tui/app.go — new method on Model

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
    switch msg.Type {
    case tea.MouseLeft:
        if msg.Down {
            ms.selecting = true
            ms.selStartRow = msg.Y
            ms.selStartCol = msg.X
            ms.selEndRow = msg.Y
            ms.selEndCol = msg.X
        } else if msg.Up && ms.selecting {
            // Mouse up: copy selection to clipboard
            ms.selecting = false
            text := m.extractTextSelection(ms.selStartRow, ms.selStartCol, ms.selEndRow, ms.selEndCol)
            if text != "" {
                _ = clipboard.WriteAll(text)
            }
        }

    case tea.MouseMotion:
        if ms.selecting {
            ms.lastMouseRow = msg.Y
            ms.lastMouseCol = msg.X
            ms.selEndRow = msg.Y
            ms.selEndCol = msg.X
        }
    }

    // Never consume mouse events — viewport still needs them for scrolling
    return nil
}
```

### extractTextSelection

The output viewport renders markdown → ANSI-escaped text. To extract plain text for a selection:

1. Maintain a flat `[]string` `renderedLines` on `Model` updated in `Update()` after each render
2. Each entry is the **plain text** (pre-glamour) version of the output line
3. `extractTextSelection(row1, col1, row2, col2)` slices `renderedLines[row1:row2+1]`, takes cols from each, joins with `\n`

```go
// On Model — add field:
renderedLines []string  // plain text output lines, updated after each render

func (m *Model) extractTextSelection(r1, c1, r2, c2 int) string {
    if r1 < 0 || r2 >= len(m.renderedLines) {
        return ""
    }
    if r1 == r2 {
        line := m.renderedLines[r1]
        if c2 > len(line) {
            c2 = len(line)
        }
        return line[c1:c2]
    }
    var lines []string
    for r := r1; r <= r2; r++ {
        line := m.renderedLines[r]
        if r == r1 {
            line = line[c1:]
        } else if r == r2 {
            if c2 < len(line) {
                line = line[:c2]
            }
        }
        lines = append(lines, line)
    }
    return strings.Join(lines, "\n")
}
```

### RenderedLines Update

After `Update()` processes a `lineMsg` or `blockEventMsg` that changes output, update `renderedLines`:

```go
// In Update(), after processing block events:
m.renderedLines = buildRenderedLines(m.blocks, m.outputLines)
```

`buildRenderedLines` extracts the raw text (no ANSI, no glamour) from blocks and output lines.

---

## 4. Test Plan

### Panel Cycling Tests (panels_test.go)

All existing tests pass with updated expectations:
- Normal mode `h`/`l` → panel cycling (no input update)
- `Ctrl+J`/`Ctrl+K` → panel cycling (existing, unchanged)
- `Esc` in insert mode → normal mode
- `i` in normal mode → insert mode

### View Tests (view_test.go)

- Insert mode: input bar shows `m.input.View()`
- Normal mode: input bar shows `[N]` indicator with normal mode hint

### Line-Editing Tests (app_test.go or new `lineedit_test.go`)

- `Ctrl+U` with text → input is empty
- `Ctrl+U` with empty input → no change
- `Ctrl+A` → cursor at 0
- `Ctrl+E` → cursor at end
- All guards: no effect during overlay or normal mode

### Mouse Tests (mouse_test.go — new file)

- Mouse down → selecting=true, selStart set
- Mouse drag → selEnd updated
- Mouse up → clipboard written, selecting=false
- Empty selection → clipboard not written

---

## 5. File Manifest

| File | Changes |
|------|---------|
| `internal/tui/state.go` | Add `VimMode` type and constants |
| `internal/tui/app.go` | Add `vimMode` field; update `handleKey`; add `handleMouse`; update `Update()`; add line-editing handlers |
| `internal/tui/view.go` | Normal mode indicator in `renderInputBar`; deprecate `OverlayPanel` |
| `internal/tui/mouse.go` | New file: `mouseState`, `handleMouse`, `extractTextSelection` |
| `internal/tui/model.go` | Add `renderedLines []string` field |
| `cmd/milliways/main.go` | Add `tea.WithMouseAllMotion()` |
| `internal/tui/panels_test.go` | Add vi mode panel cycling tests |
| `internal/tui/view_test.go` | Add normal mode indicator test |
| `internal/tui/lineedit_test.go` | New file: Ctrl+U/A/E tests |
| `internal/tui/mouse_test.go` | New file: mouse selection tests |
