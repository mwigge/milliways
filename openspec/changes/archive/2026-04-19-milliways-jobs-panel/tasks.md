# Tasks — milliways-jobs-panel

> **Pre-existing infrastructure (NOT this change's work):**
> - `pantry.TicketStore.ListRecent(n)` — already reads milliways `mw_tickets`
> - `jobsRefreshCmd` + `scheduleJobsRefresh` — fires `jobsRefreshMsg` every 5s
> - `m.jobTickets []pantry.Ticket` — stored on TUI model via `jobsRefreshMsg`
> - All of `JP-2` (old): pantry polling loop is already wired ✓
>
> **This change covers:**
> 1. `internal/jobs/` — new read-only package for OpenHands `~/.agent_task_queue.db`
> 2. OpenHands polling loop in TUI — separate from milliways tickets loop
> 3. `renderJobsPanel()` — renders `m.jobTickets` (milliways tickets) AND OpenHands jobs
> 4. SPS-1 (`milliways-tui-panels`) will integrate as `SidePanelJobs` once SPS-1 ships

## JP-1: internal/jobs package (OpenHands task-queue reader) [1 SP]

> Reads `~/.agent_task_queue.db` — separate DB, separate schema from milliways `mw_tickets`.

- [x] JP-1.1 Add `modernc.org/sqlite` dependency: `go get modernc.org/sqlite`
- [x] JP-1.2 Create `internal/jobs/reader.go`:
  ```go
  type Job struct {
      ID        string
      Title     string
      Status    string // pending/claimed/in_progress/done/failed
      CreatedAt string
      UpdatedAt string
      Wing      string
  }
  type Reader struct { db *sql.DB }
  ```
- [x] JP-1.3 `NewReader() (*Reader, error)`: reads `TASK_QUEUE_DB` env var
  (default `~/.agent_task_queue.db`), opens with `?_journal_mode=WAL&_timeout=100`,
  returns `nil, nil` if file does not exist (graceful degradation)
- [x] JP-1.4 `(r *Reader) List(n int) ([]Job, error)`:
  `SELECT id, title, status, created_at, updated_at, COALESCE(wing,'') FROM tasks
   ORDER BY updated_at DESC LIMIT ?`
  Returns empty slice (not nil) when table is empty
- [x] JP-1.5 `(r *Reader) Close() error`
- [x] JP-1.6 Table-driven tests in `internal/jobs/reader_test.go`:

## JP-2: OpenHands reader TUI integration [0.5 SP]

> milliways tickets polling (via pantry) already exists. This adds a SECOND, independent
> loop for OpenHands jobs. The two loops are separate — different DBs, different intervals.

- [x] JP-2.1 Add to `Model` in `internal/tui/app.go`:
  `openhandsJobsReader *jobs.Reader`, `openhandsJobs []jobs.Job`
- [x] JP-2.2 Add `openhandsJobsTickMsg` type (alias `time.Time`)
- [x] JP-2.3 `openhandsJobsTickCmd()`: `tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
  return openhandsJobsTickMsg(t) })`
- [x] JP-2.4 In `NewModel()`: call `jobs.NewReader()`, store on model
  (nil reader = degraded state, panel shows "no jobs db")
- [x] JP-2.5 In `Init()`: add `openhandsJobsTickCmd()` to the returned `tea.Batch`
  (keep existing `jobsRefreshCmd` for milliways tickets — do NOT remove it)
- [x] JP-2.6 Handle `openhandsJobsTickMsg` in `Update()`:
  if `m.openhandsJobsReader != nil`, call `m.openhandsJobsReader.List(6)`,
  store in `m.openhandsJobs`, reschedule via `openhandsJobsTickCmd()`.
  DB errors → silently ignore (stale data is fine).

## JP-3: renderJobsPanel() [0.5 SP]

> Two independent data sources rendered in one panel:
> - `m.jobTickets` — milliways tickets (already populated via `jobsRefreshMsg`)
> - `m.openhandsJobs` — OpenHands jobs (new, via `openhandsJobsTickMsg`)
>
> When `milliways-tui-panels` SPS-1 ships, this becomes `renderJobsPanel(width, height int)`
> and is called from `renderActiveSidePanel`. Until then, append to the sidebar View().

- [x] JP-3.1 `renderJobsPanel() string` in `internal/tui/view.go` (or `jobs_panel.go`):
  Returns `""` when `m.height < 20`.
  When `m.jobTickets` is non-empty OR `m.openhandsJobs` is non-empty, render a divider
  `"Jobs"` header then two sub-sections:
  - **milliways**: iterate `m.jobTickets` (up to 6), show `statusIcon(t.Status) + " " +
    truncate(t.Prompt, 20) + " " + t.Kitchen`
  - **OpenHands**: iterate `m.openhandsJobs` (up to 6), show
    `openhandsStatusIcon(j.Status) + " " + truncate(j.Title, 20) + " " + j.Wing`
  When both empty: `"No active jobs"` in muted style.
  When respective reader is nil: show section header only with `"no db"` muted note.
- [x] JP-3.2 `openhandsStatusIcon(status string) string`: `in_progress`/`claimed` → `⠿`,
  `done` → `✓` (green), `failed` → `✗` (red), `pending` → `○` (muted), default → `·`
- [x] JP-3.3 In `View()`: append `renderJobsPanel()` to right-side vertical join
  (below `renderLedger()`), only when non-empty. **Do NOT remove existing
  `renderLedger()` call — both panels coexist.**
- [x] JP-3.4 Render tests (table-driven string-match on `renderJobsPanel()` output):
  milliways done → checkmark; milliways failed → red cross; openhands done → checkmark;
  openhands failed → red cross; both empty → "No active jobs"; nil milliways reader →
  "no db" for milliways section; nil openhands reader → "no db" for openhands section;
  height < 20 → `""`

## JP-4: Integration + cleanup [0.5 SP]

- [x] JP-4.1 `go test ./...` passes — existing tests unaffected
- [x] JP-4.2 `go vet ./...` passes
- [x] JP-4.3 `go build ./...` passes with no CGO warnings
- [ ] JP-4.4 Manual smoke: `milliways --tui`, submit an OpenHands task from another
  terminal, confirm OpenHands jobs section updates within ~2s; submit a milliways async
  ticket, confirm milliways section updates within ~5s
- [ ] JP-4.5 Update `milliways-tui-panels` JP-extra task to reflect actual JP-3 work done
  here, to avoid double-implementing `renderJobsPanel()`

- [ ] 🍽️ **Service check** — The kitchen has a window. You can see what's cooking without
  leaving the table.
