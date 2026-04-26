## Why

The current milliways TUI is broken beyond repair. It runs on a complex Bubble Tea architecture with panels, vim mode, mouse selection, and PTY handling — all of which has proven slow, hard to follow, and difficult to maintain. The fire-and-forget dispatch model buffers output and dumps it all at once instead of streaming in real-time. The tiering auto-routing model was abandoned. The TUI features (vim mode, file browser, nvim plugin) have been in-progress for months with nothing working. We need a clean break.

## What Changes

- **Replace TUI with REPL**: Lightweight read-eval-print loop using `liner` (readline replacement) + raw ANSI escape sequences. No panels, no vim mode, no mouse. Fast and simple.
- **Sequential runners**: One runner (claude/codex/minimax) at a time. Switch explicitly with `/switch`. No auto-routing. No concurrent blocks.
- **Unified kitchen model**: claude and codex execute as CLI subprocesses. minimax uses HTTP API (MiniMax-M2.7 model) with SSE streaming.
- **Mempalace-backed session persistence**: Conversations survive runner switches and milliways restarts. Memory is shared across all runners.
- **Real-time streaming**: Output appears line-by-line as it arrives from the subprocess. No buffering.
- **Green phosphor aesthetic**: Adobe "Monochromatic CPU Terminal Green" palette (#4FB522, #2E6914, #2E6914, #466D35 on #000000).
- **REPL commands**: `/switch`, `/stick`, `/back`, `/session`, `/history`, `/summary`, `/cost`, `/limit`, `/openspec`, `/repo`, `/login`, `/logout`, `/auth`, `/help`. `!<cmd>` for bash.
- **Drop**: Bubble Tea, all TUI panels, vim mode, mouse selection, concurrent blocks, file browser, nvim plugin.

## Capabilities

### New Capabilities

- `repl-interface`: Lightweight REPL with liner input + raw ANSI output. Single shared viewport. Sequential execution.
- `sequential-runners`: One runner at a time. Explicit `/switch <runner>`. Runner manages its own subagents internally.
- `kitchen-runners`: claude and codex run as CLI subprocesses with piped stdout streaming. minimax uses HTTP API with SSE streaming. Auth via PTY for CLI kitchens.
- `session-persistence-mempalace`: Mempalace fork persists conversation across runner switches and restarts. Shared memory layer.
- `repl-commands`: All REPL commands for routing, session management, quotas, context, and auth.
- `phosphor-aesthetic`: Green monochrome terminal aesthetic with the Adobe palette.
- `quota-tracking`: Per-runner day/week/month quotas with reset timestamps shown via `/limit`.

### Modified Capabilities

<!-- No existing spec-level behavior changes. The jobs-panel spec remains unchanged. -->

## Impact

- `internal/tui/` → `internal/repl/` — complete rewrite
- Bubble Tea → liner + raw ANSI — no TUI framework
- `internal/kitchen/adapter/http.go` — keep for minimax HTTP API (MiniMax-M2.7 model)
- `internal/orchestrator/` — simplified for sequential runner model, no auto-routing
- `internal/session/` — mempalace-backed persistence replaces internal session
- `openspec/specs/` — new spec files for above capabilities
