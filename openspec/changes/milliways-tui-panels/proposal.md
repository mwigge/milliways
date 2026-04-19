# Proposal — milliways-tui-panels

## Why

The TUI right sidebar currently shows a static split: block list (top) + ledger (bottom). This is insufficient for power users who need different views at different times — a cost analyst wants spend tracking, a debugging session wants system resources, a code review wants a diff viewer. All the data needed for richer panels already exists in the milliways event stream; it's just not being rendered.

Additionally, the jobs panel (`milliways-jobs-panel`) was designed as a standalone addition. It should instead be one panel in an interchangeable panel system.

## What Changes

### Core: Interchangeable panel system

The right sidebar (24 columns) is restructured as:

```
┌─────────────────────┬──────────┐
│  Block viewport     │ Block list│
│  (main area)        ├──────────┤
│                     │ [Ledger]  │  ← cycles with ctrl+]/ctrl+[
│                     │ [Jobs]    │
│                     │ [Cost]    │
│                     │ [Routing] │
│                     │ [System]  │
│                     │ [OpenSpec]│  ← NEW
│                     │ [Snippets]│
│                     │ [Diff]    │
│                     │ [Compare] │
├─────────────────────┴──────────┤
│  Input bar                     │
└────────────────────────────────┘
```

- **Top-right block list**: always pinned (active block state)
- **Bottom-right panel**: swappable — one panel active at a time, cycles through 10 modes
- **`ctrl+]`** advances to next panel; **`ctrl+[`** goes to previous
- **Panel header** shows current panel name + `ctrl+[/ctrl+]` hint
- Panel state persists across sessions (stored with TUI session)

### New panels

| Panel | What it shows | Data source |
|-------|--------------|-------------|
| **Cost** | Per-kitchen cumulative USD, per-call breakdown, session total | `adapter.EventCost` |
| **Routing audit** | Why sommelier chose this kitchen — signal scores + tier | `sommelier.Decision` |
| **System resources** | CPU%, memory MB for each active subprocess | `os/exec` + `ps` |
| **Prompt library** | Saved snippets with variable placeholders, fuzzy search | `~/.config/milliways/snippets.toml` |
| **Diff / changeset** | Files modified in this session, inline diff for key files | git diff + filewatch |
| **Multi-model compare** | Side-by-side output from N kitchens on the same prompt | parallel dispatch |
| **OpenSpec** | Active change name, task progress bar, incomplete items by course | `openspec list --json` + `tasks.md` parsing |

### Modified capabilities

- `tui-process-map`: block list stays; ledger moves to swappable panel
- `jobs-panel`: becomes swappable panel mode (replaces standalone addition)
- `openspec-panel`: new swappable panel showing active change progress and task breakdown

## Capabilities

### New capabilities

- `panel-system`: interchangeable bottom-right panel with keyboard navigation
- `cost-panel`: per-kitchen spend tracking with session totals
- `routing-audit-panel`: sommelier decision reasoning display
- `system-resources-panel`: live subprocess CPU/memory per kitchen
- `prompt-library-panel`: snippet management and insertion
- `diff-panel`: session file changes with inline diff
- `compare-panel`: parallel multi-kitchen dispatch with side-by-side rendering

### Modified capabilities

- `tui-process-map`: ledger panel becomes swappable (Ledger, Jobs, Cost, Router, System, Snippets, Diff, Compare)
- `jobs-panel`: integrated into swappable panel system

## Impact

- `internal/tui/view.go` — View() restructured: block list pinned top-right, swappable panel bottom-right
- `internal/tui/state.go` — new `SidePanelMode` enum (8 states), `sidePanelIdx int`, `sidePanelState` struct
- `internal/tui/app.go` — `ctrl+[/]` keybindings, panel state persistence
- `internal/tui/styles.go` — panel header style (tab name + nav hint)
- `internal/tui/cost.go` — new: cost accumulator, renderCostPanel()
- `internal/tui/routing_audit.go` — new: routing history, renderRoutingPanel()
- `internal/tui/system_resources.go` — new: subprocess monitor, renderSystemPanel()
- `internal/tui/snippets.go` — new: snippet loader, renderSnippetsPanel()
- `internal/tui/diff.go` — new: git diff parser, renderDiffPanel()
- `internal/tui/compare.go` — new: parallel dispatch tracker, renderComparePanel()
- `internal/tui/jobs_panel.go` — extracted from jobs_panel tasks
- `internal/tui/ledger_panel.go` — extracted from existing renderLedger()
- `internal/tui/openspec_panel.go` — new: OpenSpec change status, task progress
- `cmd/milliways/main.go` — snippet file path resolution
- `~/.config/milliways/snippets.toml` — user-managed snippet storage (created on demand)
- No changes to orchestrator, sommelier, kitchen, ledger (read existing data only)
