# milliways-warp-experience — From Dispatch Shell to Warp-Like AI Terminal

> The restaurant doesn't serve one guest at a time.
> Every table is active, every waiter knows their tables,
> and the kitchen board shows all orders at a glance.

## Why

Milliways today is sequential: one dispatch at a time, output piled into a single scrollback, queue that blocks until the current dispatch finishes. This worked for "route my prompt to the right CLI" but fails as a primary developer tool. Users coming from Warp expect:

1. **Block-based output** — each dispatch is a discrete, visually separated block with its own status, collapsible and scrollable independently. Not a single undifferentiated stream.

2. **Concurrent dispatch** — multiple kitchens working in parallel, each in its own block. Submit to Claude while OpenCode is still running. The queue shouldn't be FIFO-with-blocking; it should be a concurrent work tracker.

3. **Rich input** — command palette (`/` prefix), fuzzy history search (`Ctrl+R`), inline completions for kitchen names and common prompts.

4. **Live progress per block** — each block shows its own state: routing, streaming, tool use, cost, duration. Not a single process map for the whole TUI.

5. **Searchable, structured history** — blocks persist across sessions. Search by kitchen, by prompt text, by date.

The current TUI has the building blocks (session sections, process map, queue, adapters) but the UX compounds them into a single-threaded experience that can't compete with Warp's parallel, block-centric model.

## What Changes

### Block Model

Replace the flat `Session.Sections` list + single viewport with a **block-oriented viewport**:

- Each dispatch becomes a `Block` — a self-contained unit with header (prompt, kitchen badge, status), body (streaming output), and footer (cost, duration, rating).
- Blocks are visually separated by full-width borders. The active/streaming block gets a highlighted border.
- Blocks can be **collapsed** (show header only) or **expanded** (show full output). Default: last 3 expanded, rest collapsed.
- Blocks support **per-block scroll** — arrow keys scroll within the focused block; `Tab` moves focus between blocks.
- The viewport is a vertical stack of blocks, not a single text buffer.

```
┌─────────────────────────────────────────────────────┐
│ ▶ explain the auth middleware          claude  ✓ 23s│
│─────────────────────────────────────────────────────│
│ claude  The auth middleware in server/middleware.go  │
│         validates JWT tokens by...                  │
│         [collapsed — 42 more lines]                 │
└─────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────┐
│ ▶ @opencode fix the login bug     opencode  ⟳ 47s  │
│─────────────────────────────────────────────────────│
│ opencode ⚙ Read src/auth.go (done)                  │
│ opencode ⚙ Edit src/auth.go (started)               │
│ opencode   Fixing the token expiry check...         │
│                                            streaming│
└─────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────┐
│ ▶ search for OWASP top 10 patterns  gemini  ⟳ 12s  │
│─────────────────────────────────────────────────────│
│ gemini   Searching for security patterns...         │
│                                             routing │
└─────────────────────────────────────────────────────┘
```

### Concurrent Dispatch

Replace the sequential dispatch model with concurrent execution:

- Remove `isDispatching()` gate. Any prompt submitted starts immediately in a new block.
- Each block owns its own `context.Context`, `cancelFn`, adapter reference, and dispatch state.
- The process map panel becomes a **block list** showing all active blocks with one-line status.
- `Ctrl+C` cancels the **focused** block, not the entire TUI. `Ctrl+D` still exits.
- Maximum concurrent dispatches configurable in `carte.yaml` (default: 4).
- Queue remains for overflow beyond max concurrent — shows "queued (position N)".

```go
// Block represents a single dispatch lifecycle.
type Block struct {
    ID            string
    Prompt        string
    Kitchen       string
    Decision      sommelier.Decision
    State         DispatchState
    Lines         []OutputLine
    Collapsed     bool
    StartedAt     time.Time
    Duration      time.Duration
    Cost          *adapter.CostInfo
    ExitCode      int
    CancelFn      context.CancelFunc
    ActiveAdapter adapter.Adapter
}
```

### Block List Panel (replaces Process Map)

The right-side panel becomes a vertical list of all blocks with compact status:

```
┌────────────────────────┐
│ Blocks                 │
│ ✓ explain auth    23s  │
│ ⟳ fix login bug   47s  │
│ ⟳ search OWASP    12s  │
│ · queued: lint all  #4 │
│                        │
│ Active: 3/4            │
│ Queued: 1              │
└────────────────────────┘
```

Click (or `1-9` shortcut) focuses a block. Focused block gets highlighted border and receives scroll input.

### Command Palette

`/` prefix in the input opens a fuzzy-filtered command palette:

| Command | Action |
|---------|--------|
| `/status` | Show kitchen availability |
| `/report` | Routing statistics |
| `/history` | Searchable dispatch history |
| `/cancel <N>` | Cancel block N |
| `/collapse all` | Collapse all blocks |
| `/expand <N>` | Expand block N |
| `/kitchen <name>` | Force next dispatch to kitchen |
| `/pipeline <prompt>` | Multi-kitchen pipeline |
| `/session save` | Save session to file |
| `/session load <file>` | Load a saved session |

### Fuzzy History Search

`Ctrl+R` opens a fuzzy search overlay over the input:

- Searches all prompts from current session + pantry ledger history
- Shows kitchen badge and outcome for each match
- Enter selects, Esc cancels
- Results ranked by recency + frequency

### Session Persistence

Sessions persist to `~/.config/milliways/sessions/`:

- Auto-save on exit (blocks + collapsed state + history)
- Resume last session on start (`milliways --tui --resume`)
- Named sessions (`milliways --tui --session auth-refactor`)
- Block output stored as structured data, re-renderable

## Capabilities

### New Capabilities

- `block-model`: self-contained dispatch blocks with header/body/footer, collapse/expand, per-block scroll, and visual separation
- `concurrent-dispatch`: parallel kitchen execution with per-block context, configurable concurrency limit, overflow queue
- `block-list-panel`: compact block status panel replacing the process map, with focus navigation
- `command-palette`: `/`-prefixed fuzzy command picker for TUI actions
- `fuzzy-history`: `Ctrl+R` prompt history search with kitchen and outcome metadata
- `session-persistence`: auto-save/resume sessions with named session support

### Modified Capabilities

- `tui-app`: `Model` struct gains `blocks []Block`, `focusedBlock int`, `maxConcurrent int`; replaces single `dispatchState` and `processMap` with per-block state
- `tui-view`: `View()` renders block stack instead of flat viewport; side panel becomes block list
- `tui-dispatch`: `startDispatch()` creates a new block and starts adapter in parallel; no longer gates on `isDispatching()`
- `tui-queue`: queue only used for overflow beyond `maxConcurrent`; dequeues into new block when a block completes
- `session-model`: `Session.Sections` replaced by `[]Block`; `RenderViewport` replaced by per-block rendering

## Impact

### New Files

- `internal/tui/block.go` — Block struct, rendering, collapse/expand logic
- `internal/tui/blocklist.go` — block list panel (replaces process map)
- `internal/tui/palette.go` — command palette overlay with fuzzy matching
- `internal/tui/search.go` — fuzzy history search overlay
- `internal/tui/persist.go` — session save/load to disk

### Modified Files

- `internal/tui/app.go` — Model refactored: `blocks []Block` replaces `session`, `dispatchState`, `processMap`; Update handles per-block messages; concurrent dispatch
- `internal/tui/view.go` — block-stack rendering replaces flat viewport
- `internal/tui/dispatch.go` — `startDispatch` creates Block, runs in parallel; no `isDispatching` gate
- `internal/tui/messages.go` — messages gain `blockID` field for routing to correct block
- `internal/tui/state.go` — DispatchState stays but moves to per-block scope
- `internal/tui/queue.go` — queue dequeues into new concurrent block, not into input field
- `internal/tui/styles.go` — block border styles (active, done, failed, focused)

### Removed Files

- `internal/tui/session.go` — replaced by block model (session aggregation moves to Block helpers)
- `internal/tui/jobs_panel.go` — absorbed into block list panel

### Dependencies

- `github.com/sahilm/fuzzy` — fuzzy matching for command palette and history search (already a bubbles dependency)

## Supersedes

This change supersedes:

- `milliways-repl-experience` — the block model + concurrent dispatch is the REPL experience
- `milliways-jobs-panel` — jobs panel is replaced by the block list panel which shows all dispatches (sync and async) in one view

## Inspiration

- [Warp terminal](https://github.com/warpdotdev/warp) — block-based output, AI agents in parallel, command palette
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — already the foundation; this change uses it more idiomatically with nested models per block
- [Harrison Cramer's Go TUI patterns](https://harrisoncramer.me/terminal-applications-in-go/) — parent/child model delegation, async commands
