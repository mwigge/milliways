# Tasks — milliways-warp-experience

## Phase 1: Block Model Core (replaces session model)

### T1: Block struct and rendering
- [x] Create `internal/tui/block.go` with `Block` struct per D1
- [x] `Block.RenderHeader()` — prompt + kitchen badge + state icon + duration
- [x] `Block.RenderBody(width)` — output lines with kitchen prefix (port from session.go)
- [x] `Block.RenderSeparator()` — full-width line between header and body
- [x] `Block.AppendEvent(event)` — port from `Session.AppendEvent`
- [x] `Block.Complete(exitCode, cost)` — port from `Session.CompleteSection`
- [x] Collapse/expand: `Block.Collapsed` toggles between header-only and full render
- [x] Tests: render header, render body, append events, collapse toggle

### T2: Block list panel
- [x] Create `internal/tui/blocklist.go` with `RenderBlockList(blocks, focused, queue, width, height)`
- [x] Show state icon, truncated prompt, duration per block
- [x] Highlight focused block with `>` prefix
- [x] Show queue entries below active blocks
- [x] Show `Active: N/M` and `Queued: N` footer
- [x] Tests: render with 0/1/N blocks, with queue items, focus indicator

## Phase 2: Concurrent Dispatch

### T3: Refactor Model for multi-block
- [x] Replace `session Session` with `blocks []Block` in Model
- [x] Replace `dispatchState` / `processMap` with per-block state (on Block)
- [x] Add `focusedIdx`, `maxConcurrent`, `activeCount`, `nextBlockID` to Model
- [x] Remove `activeAdapter` from Model (moved to Block)
- [x] Remove `cancelFn` from Model (moved to Block)
- [x] Update `isDispatching()` → check `activeCount > 0`

### T4: Per-block message routing
- [x] Add `BlockID` to `eventMsg`, `routedMsg`, `dispatchDoneMsg` per D2
- [x] Update `Update()` to route messages by BlockID to the correct block
- [x] `tickMsg` updates elapsed on ALL active blocks, not just one
- [x] `Ctrl+C` cancels focused block only; `Ctrl+D` exits TUI

### T5: Concurrent dispatch logic
- [x] `startDispatch()` creates a new Block, increments `activeCount`
- [x] Dispatch starts immediately if `activeCount < maxConcurrent`
- [x] Overflow goes to queue with `[queued] position N` system line
- [x] `blockDoneMsg` decrements `activeCount`, dequeues next if available
- [x] Read `max_concurrent` from carte.yaml (default 4)
- [x] Tests: concurrent dispatch up to limit, overflow queues, dequeue on completion

## Phase 3: Block-Stack Viewport

### T6: Block-stack rendering
- [x] `View()` renders block stack with borders per D4
- [x] Focused block gets highlighted border
- [x] Non-focused completed blocks auto-collapse
- [x] `c` key toggles collapse on focused block
- [x] Outer viewport wraps block stack for global scroll
- [x] Per-block scroll: up/down scrolls within focused block when it overflows
- [x] `Tab` cycles focus between blocks; `1`-`9` jumps to block N

### T7: Updated view layout
- [x] Left: block-stack viewport (replaces flat output viewport)
- [x] Right top: block list panel (replaces process map)
- [x] Right bottom: ledger panel (keep as-is)
- [x] Bottom: input bar (keep as-is, with overlay support)
- [x] Title bar: keep kitchen status + "Milliways" title

## Phase 4: Rich Input

### T8: Command palette
- [x] Create `internal/tui/palette.go` with `PaletteItem` and fuzzy matching per D6
- [x] `/` at start of empty input opens palette overlay
- [x] Fuzzy filter as user types after `/`
- [x] Up/down navigation, Enter executes, Esc cancels
- [x] Implement actions: status, report, cancel, collapse, expand, history
- [x] Palette renders as popup above input bar
- [x] Tests: fuzzy matching, action dispatch

### T9: Fuzzy history search
- [x] Create `internal/tui/search.go` with history search overlay per D7
- [x] `Ctrl+R` opens overlay
- [x] Source: current session blocks + pantry ledger last 200
- [x] Fuzzy match on prompt text
- [x] Show kitchen badge + status icon per entry
- [x] Enter fills input, Esc cancels
- [x] Tests: fuzzy search, result ranking

## Phase 5: Session Persistence

### T10: Session save/load
- [x] Create `internal/tui/persist.go` with `PersistedSession` per D8
- [x] Auto-save on clean exit to `~/.config/milliways/sessions/last.json`
- [x] `--resume` flag loads last session
- [x] `--session <name>` loads/creates named session
- [x] Only persist completed blocks (not in-progress)
- [x] `/session save` and `/session load` palette commands
- [x] Tests: round-trip serialize/deserialize, auto-save on exit

## Phase 6: Cleanup

### T11: Remove superseded code
- [x] Delete `internal/tui/session.go` (replaced by block.go)
- [x] Delete `internal/tui/jobs_panel.go` (replaced by blocklist.go)
- [x] Update `internal/tui/session_test.go` → block_test.go
- [x] Update all imports
- [x] Full test suite green
