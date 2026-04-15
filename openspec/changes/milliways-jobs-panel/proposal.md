## Why

When milliways dispatches a task to OpenHands, there is no visibility into the agent's
progress from within the TUI — you have to check a browser tab or tail a log file.
The task-queue SQLite DB (written by `openhands_bridge.py`) is already the source of
truth for job state; milliways should surface it.

## What Changes

- New **Jobs panel** in the TUI right-side column (below the Process Map), showing
  pending, running, and recently completed OpenHands task-queue jobs
- New `internal/jobs/` package — lightweight read-only reader for the task-queue DB
- Periodic poll (every 2s) via a `tea.Tick` command; no new goroutines or channels needed
- Jobs panel respects the existing layout: fits in the 24-column right sidebar

## Capabilities

### New Capabilities

- `jobs-panel`: Read-only TUI panel that lists task-queue jobs (pending/running/done/failed)
  with status icon, truncated title, and elapsed/duration. Polls the local SQLite DB on a
  2s tick. No interaction — display only.

### Modified Capabilities

<!-- none — existing TUI layout and dispatch flow are unchanged -->

## Impact

- **internal/jobs/**: new package (`reader.go`, `reader_test.go`)
- **internal/tui/app.go**: adds `jobsPanel` field to `Model`, handles `jobsTickMsg`,
  calls `renderJobs()` in `View()`
- **internal/tui/styles.go**: no changes required (reuses existing status icons and muted style)
- **go.mod**: adds `modernc.org/sqlite` (pure-Go SQLite driver, no CGO) or reuses
  `database/sql` + `mattn/go-sqlite3` if already present
- **No API changes, no breaking changes, no new concurrency primitives**
