## Why

milliways routes across 7 providers sequentially today — one turn, one provider. For tasks like repo review, running the same prompt against claude, codex, and local simultaneously and merging their findings yields higher signal than any single agent alone, and exposes disagreements that reveal genuine ambiguity. The infrastructure (concurrent daemon sessions, MemPalace knowledge graph, WezTerm pane splits) is already present; this change wires them into a first-class parallel-run workflow with a supervision UI matching the agent-deck dashboard aesthetic.

## What Changes

- New `/parallel <prompt>` slash command fans a prompt out to N providers simultaneously and takes over the terminal with a split-pane supervision layout.
- New `parallel.dispatch` RPC opens N daemon sessions concurrently, primes each with MemPalace baseline context for the target path, and returns a group ID immediately.
- New `ParallelGroup` tracker persists group state (slots, statuses, token counts) to SQLite via pantry so groups survive daemon restarts.
- New `milliways attach <handle>` sub-command tails a running session's output stream; used by WezTerm panes to display each slot live.
- New split-pane supervisor layout: narrow left navigator (slot list with provider, status, last-activity, token count, bright-border selection) + wide right content pane (live streaming from selected slot) + full-width status bar. Pure terminal aesthetic matching the agent-deck reference design.
- New consensus aggregator: after all slots complete, queries MemPalace for findings tagged by group and path, groups by file+symbol, weights by agreement count (HIGH ≥ 3 agents, MEDIUM = 2, LOW = 1), deduplicates near-identical findings.
- New post-session MemPalace write hook: parses each agent's completed response for structured findings and calls `kg_add` with source and group tags, enabling cross-run memory accumulation.

## Capabilities

### New Capabilities

- `parallel-dispatch`: `/parallel` slash command + `parallel.dispatch` RPC + `ParallelGroup` SQLite tracker. Fan-out to N providers, prime with MemPalace context, return group ID immediately.
- `parallel-panel-layout`: Split-pane supervision TUI — left slot navigator, right live stream, bottom status bar. `milliways attach <handle>` sub-command for pane-to-session binding. Keyboard: 1–N select slot, Tab cycle, c consensus, q exit.
- `consensus-aggregator`: Post-group MemPalace query, file+symbol grouping, confidence weighting, deduplication, structured summary render.
- `mempalace-session-write`: Post-session hook that extracts structured findings from agent responses and writes them to MemPalace with source and group tags.

### Modified Capabilities

- `takeover-command`: Stretch-goal `/inject <handle>` extension (pull another session's last-N messages into current context) deferred — not in this change.

## Impact

- **New package**: `internal/parallel/` — dispatch, group, layout, consensus, memwrite
- **New command**: `cmd/milliways/attach.go`
- **Daemon RPC**: two new methods (`parallel.dispatch`, `group.status`, `group.list`) registered in `cmd/milliwaysd/`
- **Pantry / SQLite**: two new tables (`parallel_groups`, `parallel_slots`)
- **Dependencies**: bubbletea + lipgloss already used; no new external deps required
- **MemPalace**: read (baseline injection) and write (post-session findings) — existing MCP client used
- **WezTerm integration**: `wezterm cli split-pane` used for pane spawning; graceful fallback to inline lipgloss columns when not in WezTerm
