# Tasks — milliways-jobs-panel

## JP-1: jobs.Reader package [1 SP]

- [ ] JP-1.1 Add `modernc.org/sqlite` dependency: `go get modernc.org/sqlite` in `pprojects/milliways`
- [ ] JP-1.2 Create `internal/jobs/reader.go`: `Job` struct (`ID`, `Title`, `Status`, `CreatedAt`, `UpdatedAt`, `Wing string`) and `Reader` struct with `db *sql.DB`
- [ ] JP-1.3 Implement `NewReader() (*Reader, error)`: reads `TASK_QUEUE_DB` env var (default `~/.agent_task_queue.db`), opens with `?_journal_mode=WAL&_timeout=100`, returns `nil, nil` if the file does not exist (graceful degradation)
- [ ] JP-1.4 Implement `(r *Reader) List(n int) ([]Job, error)`: `SELECT id, title, status, created_at, updated_at, COALESCE(wing,'') FROM tasks ORDER BY updated_at DESC LIMIT ?`; returns empty slice (not nil) when table is empty
- [ ] JP-1.5 Implement `(r *Reader) Close() error`
- [ ] JP-1.6 Table-driven unit tests in `internal/jobs/reader_test.go`: (a) NewReader with missing file returns nil reader, no error; (b) List returns rows ordered by updated_at desc; (c) List with n=0 returns empty slice; (d) concurrent List calls do not error (WAL mode)

## JP-2: TUI model integration [1 SP]

- [ ] JP-2.1 Add to `Model` in `internal/tui/app.go`: `jobsReader *jobs.Reader`, `jobRows []jobs.Job`
- [ ] JP-2.2 Add `jobsTickMsg` type (alias `time.Time`)
- [ ] JP-2.3 Add `jobsTickCmd()` — `tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return jobsTickMsg(t) })`
- [ ] JP-2.4 In `NewModel()`: call `jobs.NewReader()`, store result on model (nil reader is valid — panel shows degraded state)
- [ ] JP-2.5 In `Init()`: return `tea.Batch(textinput.Blink, jobsTickCmd())` to start the polling loop
- [ ] JP-2.6 Handle `jobsTickMsg` in `Update()`: if `m.jobsReader != nil`, call `m.jobsReader.List(6)` and store result in `m.jobRows`; always reschedule next tick via `jobsTickCmd()`; ignore DB errors silently (stale data is fine)

## JP-3: Jobs panel renderer [0.5 SP]

- [ ] JP-3.1 Add `renderJobs()` to `internal/tui/app.go`:
  - Returns `""` (empty) when `m.height < 20`
  - Returns panel with "no jobs db" in muted style when `m.jobsReader == nil`
  - Returns panel with "no jobs yet" in muted style when `m.jobRows` is empty
  - For each job row: `jobStatusIcon(status) + " " + truncate(title, 16)` — one line per job
- [ ] JP-3.2 Add `jobStatusIcon(status string) string` pure function: `in_progress`/`claimed` → spinner `⠿`, `done` → `✓` (green), `failed` → `✗` (red), `pending` → `○` (muted), default → `·` (muted)
- [ ] JP-3.3 Update `View()`: append `renderJobs()` result to the right-side vertical join (below `renderLedger()`), only when non-empty
- [ ] JP-3.4 Render tests (string-match on `renderJobs()` output): in_progress shows spinner; done shows checkmark; failed shows red cross; empty slice shows "no jobs yet"; nil reader shows "no jobs db"; height < 20 returns ""

## JP-4: Integration + cleanup [0.5 SP]

- [ ] JP-4.1 `go test ./...` passes — all existing tests unaffected
- [ ] JP-4.2 `go vet ./...` passes
- [ ] JP-4.3 `go build ./...` passes with no CGO warnings (verify `modernc.org/sqlite` pure-Go)
- [ ] JP-4.4 Manual smoke test: run `milliways --tui`, submit a task via bridge in another terminal, confirm Jobs panel updates within ~2s

- [ ] 🍽️ **Service check** — The kitchen has a window. You can see what's cooking without leaving the table.
