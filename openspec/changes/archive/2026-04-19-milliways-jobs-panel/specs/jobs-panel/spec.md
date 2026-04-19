## ADDED Requirements

### Requirement: Jobs panel displays task-queue jobs in the TUI sidebar
The TUI SHALL render a Jobs panel in the right-side sidebar showing the most recent
task-queue jobs. The panel SHALL display at most 6 jobs ordered by `updated_at`
descending (most recent first).

#### Scenario: Jobs panel renders running job
- **WHEN** the task-queue DB contains a job with status `in_progress`
- **THEN** the Jobs panel SHALL show a spinner icon, the truncated job title (max 16 chars), and elapsed time since `updated_at`

#### Scenario: Jobs panel renders done job
- **WHEN** the task-queue DB contains a job with status `done`
- **THEN** the Jobs panel SHALL show a green checkmark icon and the truncated job title

#### Scenario: Jobs panel renders failed job
- **WHEN** the task-queue DB contains a job with status `failed`
- **THEN** the Jobs panel SHALL show a red cross icon and the truncated job title

#### Scenario: Jobs panel renders pending job
- **WHEN** the task-queue DB contains a job with status `pending` or `claimed`
- **THEN** the Jobs panel SHALL show a muted clock icon and the truncated job title

#### Scenario: Jobs panel empty state
- **WHEN** the task-queue DB exists but contains no jobs
- **THEN** the Jobs panel SHALL render the text "no jobs yet" in muted style

#### Scenario: Jobs panel DB absent
- **WHEN** the configured DB path does not exist or cannot be opened
- **THEN** the Jobs panel SHALL render "no jobs db" in muted style and SHALL NOT crash or return an error to the caller

#### Scenario: Jobs panel hidden on narrow terminal
- **WHEN** the terminal height is less than 20 rows
- **THEN** the Jobs panel SHALL not be rendered (returns empty string) to avoid layout breakage

### Requirement: Jobs panel refreshes automatically every 2 seconds
The TUI SHALL poll the task-queue DB every 2 seconds using a `tea.Tick`-based command.
No background goroutines SHALL be used for the refresh.

#### Scenario: Tick triggers refresh
- **WHEN** a `jobsTickMsg` is received in `Update()`
- **THEN** the TUI SHALL read the DB synchronously, update the `jobRows` slice on the model, and schedule the next tick

#### Scenario: Tick does not fire when TUI is not ready
- **WHEN** `m.ready` is false (window size not yet received)
- **THEN** no `jobsTickMsg` SHALL be scheduled and the jobs panel SHALL not be rendered

### Requirement: Jobs reader opens SQLite DB read-only with WAL and busy timeout
The `jobs.Reader` SHALL open the SQLite DB with `?_journal_mode=WAL&_timeout=100`
query parameters to allow concurrent access without locking.

#### Scenario: Concurrent write does not block the reader
- **WHEN** the SQLite DB is being written by the bridge at the same instant as a read
- **THEN** the reader SHALL wait up to 100ms and return results without error

#### Scenario: DB path resolved from environment
- **WHEN** `TASK_QUEUE_DB` environment variable is set
- **THEN** `jobs.NewReader()` SHALL use that path as the DB file location

#### Scenario: DB path uses default when env var absent
- **WHEN** `TASK_QUEUE_DB` environment variable is not set
- **THEN** `jobs.NewReader()` SHALL use `~/.agent_task_queue.db` as the DB file location
