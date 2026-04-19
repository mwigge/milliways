## Why

When milliways dispatches a task to OpenHands, there is no visibility into the agent's
progress from within the TUI — you have to check a browser tab or tail a log file.
The task-queue SQLite DB (written by `openhands_bridge.py`) is the source of truth for
OpenHands job state; milliways should surface it.

Separately, milliways' own async dispatch writes to `mw_tickets` (via `pantry.TicketStore`).
A `jobsRefreshMsg` polling loop already exists and populates `m.jobTickets` on the TUI
model — but the data is **never rendered**. This proposal also covers completing that
rendering, so both job sources are visible.

## What Changes

- **OpenHands jobs**: new `internal/jobs/` package — read-only reader for
  `~/.agent_task_queue.db` (OpenHands task-queue SQLite). Separate DB, separate schema,
  separate from milliways' own `mw_tickets`.
- **milliways tickets rendering**: `renderJobsPanel()` — uses the already-populated
  `m.jobTickets` slice (from `pantry.TicketStore.ListRecent(8)` via `jobsRefreshMsg`).
  Data is flowing in but not displayed yet.
- Jobs panel (OpenHands) fits in the same sidebar as the existing ledger; toggled via
  `SidePanelJobs` mode in the panel system (`milliways-tui-panels` SPS-1).

## Two Data Sources

| Source | DB | Schema | Status |
|--------|----|--------|--------|
| OpenHands jobs | `~/.agent_task_queue.db` | `id, title, status, created_at, updated_at, wing...` | `internal/jobs/` NOT written yet |
| milliways tickets | milliways own `mw_tickets` (SQLite) | `id, kitchen, prompt, mode, pid, status...` | Polling infra exists; rendering missing |

These are completely separate DBs with different schemas. The proposal covers both.

## Capabilities

### New Capabilities

- `jobs-panel-openhands`: Read-only TUI panel listing OpenHands task-queue jobs
  (pending/claimed/in_progress/done/failed) with status icon, truncated title, and
  elapsed time. Polls on a `tea.Tick`. No interaction — display only.
- `jobs-panel-milliways`: Renders the already-populated `m.jobTickets` slice from
  `pantry.TicketStore`. No new polling needed — uses existing `jobsRefreshMsg`.

### Modified Capabilities

- `milliways-tui-presence` (TP): The panel system (`milliways-tui-panels` SPS-1)
  will integrate both job panels as swappable `SidePanelJobs` modes. This proposal
  covers the rendering only; the panel cycling infrastructure is SPS-1's scope.

## Impact

- **internal/jobs/**: new package (`reader.go`, `reader_test.go`) — reads OpenHands DB
- **internal/tui/app.go**: adds `openhandsJobsReader *jobs.Reader` field to `Model`,
  handles `jobsTickMsg`, wires OpenHands reader into the refresh loop
- **internal/tui/view.go** (or new `jobs_panel.go`): adds `renderJobsPanel()` for
  milliways tickets AND OpenHands panel rendering
- **go.mod**: adds `modernc.org/sqlite` (pure-Go SQLite driver, no CGO)
- **No API changes, no breaking changes, no new concurrency primitives beyond existing
  tea.Tick loop**
