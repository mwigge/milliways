# Design — milliways-warp-experience

## D1: Block as the core unit

A `Block` replaces both `Section` (session.go) and the singleton `processState`/`DispatchState`. Each Block owns its full lifecycle.

```go
// Block represents one dispatch lifecycle in the TUI.
type Block struct {
    ID        string           // unique per session, e.g. "b1", "b2"
    Prompt    string
    Kitchen   string
    Decision  sommelier.Decision
    State     DispatchState    // per-block state machine
    Lines     []OutputLine     // streaming output
    Collapsed bool             // header-only view
    Focused   bool             // receives scroll input
    StartedAt time.Time
    Duration  time.Duration
    Cost      *adapter.CostInfo
    ExitCode  int
    Rated     *bool

    // Lifecycle — not serialized
    cancelFn      context.CancelFunc
    activeAdapter adapter.Adapter
}
```

**Why not embed the existing Section?** Section was designed for a flat list rendered into a single viewport string. Block needs its own View() method, focus handling, and lifecycle management. Clean break is simpler than wrapping.

## D2: Message routing by block ID

All dispatch messages gain a `BlockID` field so the Update loop routes them to the correct block:

```go
type blockEventMsg struct {
    BlockID string
    Event   adapter.Event
}

type blockRoutedMsg struct {
    BlockID  string
    Decision sommelier.Decision
    Adapt    adapter.Adapter
}

type blockDoneMsg struct {
    BlockID  string
    Result   kitchen.Result
    Decision sommelier.Decision
    Duration time.Duration
    Err      error
}
```

The `Update` loop finds the target block by ID and mutates it. This replaces the current pattern where all events implicitly target "the one active dispatch."

## D3: Concurrent dispatch with semaphore

```go
type Model struct {
    blocks        []Block
    focusedIdx    int
    maxConcurrent int           // from carte.yaml, default 4
    activeCount   int           // blocks in Routing/Routed/Streaming/Awaiting/Confirming
    queue         taskQueue     // overflow queue (unchanged FIFO)
    nextBlockID   int           // monotonic counter
    // ...existing fields minus dispatchState, processMap, session, activeAdapter
}
```

On submit:
1. If `activeCount < maxConcurrent`: create Block, start dispatch, increment `activeCount`.
2. If `activeCount >= maxConcurrent`: enqueue. Show "[queued] position N" in a system line.

On `blockDoneMsg`:
1. Mark block done, decrement `activeCount`.
2. If queue non-empty and `activeCount < maxConcurrent`: dequeue, create Block, start dispatch.

**Why semaphore, not goroutine pool?** Each dispatch already runs as a `tea.Cmd` goroutine. We just need to cap how many are active. A counter is simpler than a pool and integrates naturally with Bubble Tea's message model.

## D4: Block-stack viewport rendering

The main viewport is no longer a single `viewport.Model` with a flat string. Instead:

```go
func (m Model) renderBlocks() string {
    var buf strings.Builder
    for i, block := range m.blocks {
        border := blockBorder
        if i == m.focusedIdx {
            border = focusedBlockBorder
        }
        if block.Collapsed {
            buf.WriteString(border.Render(block.RenderHeader()))
        } else {
            content := block.RenderHeader() + "\n" +
                       block.RenderSeparator() + "\n" +
                       block.RenderBody(m.width - 30)
            buf.WriteString(border.Render(content))
        }
        buf.WriteString("\n")
    }
    return buf.String()
}
```

The outer viewport.Model wraps this entire block stack for scroll. Per-block scroll (when focused) adjusts an internal `scrollOffset` on the Block, showing a window into its Lines.

**Collapse/Expand:**
- `c` key toggles collapse on focused block
- Collapsed blocks show: `▶ {prompt truncated}  {kitchen badge}  {status icon} {duration}`
- Default: blocks auto-collapse when a new block starts (configurable)

## D5: Block list panel

Replaces both the process map and jobs panel with a unified view:

```go
func (m Model) renderBlockList(width, height int) string {
    lines := []string{mutedStyle.Render("Blocks")}

    for i, b := range m.blocks {
        icon := stateIcon(b.State)
        prompt := truncate(b.Prompt, width-16)
        dur := ""
        if b.State != StateIdle {
            dur = fmt.Sprintf("%.0fs", b.elapsed().Seconds())
        }
        prefix := " "
        if i == m.focusedIdx {
            prefix = ">"
        }
        lines = append(lines, fmt.Sprintf("%s%s %-*s %s",
            prefix, icon, width-12, prompt, dur))
    }

    // Queue indicator
    if m.queue.Len() > 0 {
        lines = append(lines, "")
        for j, qt := range m.queue.items {
            lines = append(lines, fmt.Sprintf(" · queued: %s #%d",
                truncate(qt.Prompt, width-18), j+1))
        }
    }

    lines = append(lines, "")
    lines = append(lines, mutedStyle.Render(
        fmt.Sprintf("Active: %d/%d", m.activeCount, m.maxConcurrent)))
    if m.queue.Len() > 0 {
        lines = append(lines, mutedStyle.Render(
            fmt.Sprintf("Queued: %d", m.queue.Len())))
    }

    return panelBorder.Width(width).Height(height).
        Render(strings.Join(lines, "\n"))
}
```

Number keys `1`-`9` focus block N. `Tab` cycles focus. Focused block gets highlighted in both the block list and the main viewport.

## D6: Command palette

`/` at the start of input opens a filtered overlay:

```go
type PaletteItem struct {
    Command     string   // e.g. "status"
    Description string   // e.g. "Show kitchen availability"
    Action      func(m *Model) tea.Cmd
}

var paletteItems = []PaletteItem{
    {"status", "Show kitchen availability", actionStatus},
    {"report", "Routing statistics", actionReport},
    {"cancel", "Cancel focused block", actionCancel},
    {"collapse", "Collapse all blocks", actionCollapseAll},
    {"expand", "Expand focused block", actionExpand},
    {"history", "Search dispatch history", actionHistory},
    {"session save", "Save session to file", actionSessionSave},
    {"session load", "Load a saved session", actionSessionLoad},
}
```

Fuzzy match on the text after `/`. Up/down to navigate, Enter to execute, Esc to cancel. The palette renders as a popup anchored above the input bar.

## D7: Fuzzy history search

`Ctrl+R` opens a history overlay:

- Source: current session blocks + pantry ledger (last 200 entries)
- Each entry: `{kitchen badge} {prompt} {status icon} {date}`
- Fuzzy match on prompt text
- Enter populates the input with the selected prompt
- Does NOT re-dispatch — just fills the input

## D8: Session persistence

Sessions serialize to `~/.config/milliways/sessions/{name}.json`:

```go
type PersistedSession struct {
    Name      string            `json:"name"`
    CreatedAt time.Time         `json:"created_at"`
    UpdatedAt time.Time         `json:"updated_at"`
    Blocks    []PersistedBlock  `json:"blocks"`
}

type PersistedBlock struct {
    ID        string            `json:"id"`
    Prompt    string            `json:"prompt"`
    Kitchen   string            `json:"kitchen"`
    State     string            `json:"state"`    // "done", "failed", "cancelled"
    Lines     []OutputLine      `json:"lines"`
    Collapsed bool              `json:"collapsed"`
    ExitCode  int               `json:"exit_code"`
    Duration  float64           `json:"duration_s"`
    Cost      *adapter.CostInfo `json:"cost,omitempty"`
    StartedAt time.Time         `json:"started_at"`
}
```

Only completed blocks are persisted (no in-progress state). Auto-save on clean exit. `--resume` reloads the last session. `--session <name>` loads/creates a named session.

## D9: Migration path from current TUI

The refactor is a clean replacement of the TUI internals. The adapter layer, sommelier, pantry, and kitchen packages are untouched — only `internal/tui/` changes.

Migration order:
1. `block.go` — Block struct + rendering (no external deps)
2. `blocklist.go` — panel rendering (replaces process map + jobs panel)
3. Refactor `app.go` — Model with blocks, concurrent dispatch, message routing
4. `view.go` — block-stack rendering
5. `dispatch.go` — per-block dispatch with blockID in messages
6. `palette.go` — command palette
7. `search.go` — fuzzy history
8. `persist.go` — session save/load
9. Delete `session.go`, `jobs_panel.go`

Each step is independently testable. Steps 1-5 are the core refactor; 6-8 are additive features.
