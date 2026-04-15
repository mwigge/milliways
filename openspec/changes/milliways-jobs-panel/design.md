## Context

`openhands_bridge.py` maintains a SQLite task-queue at `~/.agent_task_queue.db`.
Every OpenHands job dispatched from milliways (or from the CLI) is a row in this DB
with columns: `id`, `title`, `status` (pending/claimed/in_progress/done/failed),
`created_at`, `updated_at`, `assigned_to`, `wing`, `result`, `metadata`.

The milliways TUI currently has three vertical regions on the right-hand side:
Process Map (top, height 6) and Ledger (below, flexible height). Both fit in a
24-column sidebar. The Jobs panel slots in below the Ledger, within the same sidebar.

The TUI is the only consumer of this data — no write path needed.

## Goals / Non-Goals

**Goals:**
- Show the last N task-queue jobs (pending/in_progress/done/failed) in the TUI sidebar
- Refresh automatically every 2 seconds with a `tea.Tick`
- Zero new goroutines — the tick fires a `jobsTickMsg`; `Update()` reads the DB synchronously
  (sub-millisecond for a small table) and updates a `[]jobRow` slice on the model
- Pure-Go SQLite driver — no CGO, no build tag headaches
- Graceful degradation: if the DB path doesn't exist or is unreadable, panel shows
  "no jobs db" in muted style — never crashes the TUI

**Non-Goals:**
- Interacting with jobs (cancel, retry) — read-only for now
- Streaming live agent output — that is Option B (a future change)
- Showing jobs from remote instances — local DB only
- Filtering or searching jobs

## Decisions

### D1: Synchronous DB read in Update() — no goroutine

**Decision**: Read the SQLite DB directly inside `Update()` on `jobsTickMsg`, not in a
background goroutine.

**Rationale**: The task-queue table is tiny (tens to low hundreds of rows) and SQLite
reads on a local file are sub-millisecond. A goroutine + channel would add complexity
(ownership, leak risk) with no measurable benefit. If profiling ever shows this blocks
the render loop, we can convert to a goroutine at that point.

**Alternative considered**: background goroutine sending `jobsRefreshMsg` — rejected
for complexity; premature optimisation.

### D2: Pure-Go SQLite driver (`modernc.org/sqlite`)

**Decision**: Use `modernc.org/sqlite` (transpiled C → Go, no CGO).

**Rationale**: `mattn/go-sqlite3` requires CGO which complicates cross-compilation
and CI. `modernc.org/sqlite` is a drop-in `database/sql` driver with no build
constraints. Milliways has no existing SQLite dependency so we pick the better option now.

**Alternative considered**: `mattn/go-sqlite3` — rejected due to CGO requirement.

### D3: `internal/jobs` package — thin reader, no interface abstraction

**Decision**: A simple `jobs.Reader` struct with a `List(n int) ([]Job, error)` method.
No interface, no mock — the TUI test stubs the model's `[]jobRow` slice directly
(table-driven tests control input data, not the reader).

**Rationale**: The reader has one caller (the TUI). Introducing an interface for
testability adds indirection with no benefit when test data can be fed directly to the
model. Per golang-structs-interfaces: don't define interfaces on the producer side.

### D4: DB path from environment variable with default

**Decision**: `jobs.NewReader()` reads `TASK_QUEUE_DB` env var, falling back to
`~/.agent_task_queue.db` — identical to the bridge's config so they always agree.

### D5: Show last 6 jobs, most-recent first

**Decision**: `SELECT ... ORDER BY updated_at DESC LIMIT 6`. Fits comfortably in the
24-column sidebar without scrolling. Enough to show a running job + recent history.

## Risks / Trade-offs

- [Risk] SQLite "database is locked" if bridge writes at the exact same moment →
  Mitigation: open with `?_timeout=100` (100ms busy-wait) and `?_journal_mode=WAL`
  for read-only access; WAL mode allows concurrent readers + one writer without locking.

- [Risk] `modernc.org/sqlite` increases binary size (~3 MB) →
  Acceptable for a developer tool; no size budget enforced.

- [Risk] Layout crowding on narrow terminals → Mitigation: `renderJobs()` returns an
  empty string (panel hidden) when `m.height < 20`; no layout breakage.

- [Trade-off] Synchronous DB read couples render latency to disk I/O → Accepted;
  profiling baseline is <1ms on a warm filesystem cache. Revisit if it becomes noticeable.

## Migration Plan

No migration needed — additive change only. The DB path defaults to the same value
the bridge already uses; no configuration changes required for existing users.

New dependency (`modernc.org/sqlite`) added to `go.mod` / `go.sum` by `go get`.

## Open Questions

None — scope is well-defined and self-contained.
