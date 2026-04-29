## ADDED Requirements

### Requirement: Single canonical runner location

All provider runner implementations SHALL live under `internal/daemon/runners/`. No other package SHALL contain a competing runner implementation for the same provider.

#### Scenario: REPL package removed

- **WHEN** the change is applied
- **THEN** `internal/repl/` does not exist in the repository
- **AND** the build (`go build ./...`) succeeds
- **AND** `grep -r "internal/repl" --include="*.go" .` returns no matches outside `.claude/worktrees/`

#### Scenario: Every active provider has exactly one runner

- **WHEN** a developer searches for a runner implementation by provider name
- **THEN** they find exactly one `<provider>.go` file under `internal/daemon/runners/`
- **AND** no other Go file in the repository defines a `New<Provider>Runner` constructor for the same provider

### Requirement: Provider coverage

The daemon SHALL provide runners for every provider previously available via REPL: `claude`, `codex`, `gemini`, `copilot`, `local`, `minimax`, `opsx`, `pool`.

#### Scenario: All eight providers register at startup

- **WHEN** the daemon starts
- **THEN** the runner registry contains entries for `claude`, `codex`, `gemini`, `copilot`, `local`, `minimax`, `opsx`, and `pool`
- **AND** each runner's `Name()` returns the expected provider identifier

#### Scenario: Missing API key disables a runner cleanly

- **WHEN** a runner requires credentials that are not configured
- **THEN** the runner registers but reports `AuthStatus() = false`
- **AND** dispatch attempts return a structured error identifying the missing credential

### Requirement: Feature parity with prior REPL implementations

Each ported runner SHALL preserve the user-observable behaviours of its REPL predecessor: rate-limit detection, exhaustion detection, proxy-block detection (where applicable), reasoning-mode controls, attachment handling, session usage accounting, and quota reporting.

#### Scenario: Claude runner detects rate-limit reset time

- **WHEN** the claude API returns a `rate_limit_info` event with a reset timestamp
- **THEN** the runner emits a structured progress event containing the reset time
- **AND** subsequent dispatches return `ErrSessionLimit` until the reset time passes

#### Scenario: Codex runner detects Zscaler proxy block

- **WHEN** codex stderr contains a Zscaler block signature (HTML, 403 Forbidden, or "internet security by zscaler")
- **THEN** the runner returns `ErrCodexProxyBlocked`
- **AND** the user-facing error names "browser approval required"

#### Scenario: Minimax runner reports session usage

- **WHEN** a chat dispatch completes and the response includes a `usage` block
- **THEN** the runner accumulates `prompt_tokens` and `completion_tokens` into session totals
- **AND** `Quota()` returns a non-nil `SessionUsage` after at least one dispatch

### Requirement: Test parity

Every runner port SHALL preserve the test coverage of its REPL predecessor. Tests for known edge cases (rate-limit shapes, exhaustion text, JSON event parsing, stream integrity) SHALL be ported alongside the implementation.

#### Scenario: Existing REPL tests run green against ported runners

- **WHEN** the ported runner test suite runs (`go test ./internal/daemon/runners/...`)
- **THEN** every test that existed in `internal/repl/runner_<name>_test.go` (or its conceptual equivalent) passes against the new implementation
- **AND** no previously covered edge case is left untested

### Requirement: Build green at every commit

The decommission SHALL proceed runner-by-runner with the build green at every intermediate commit. The deletion of `internal/repl/` SHALL only occur after every consumer has been migrated.

#### Scenario: Intermediate commits compile and test cleanly

- **WHEN** any commit on the migration branch is checked out
- **THEN** `go build ./...` succeeds
- **AND** `go test ./...` passes (excluding tests inside `internal/repl/` that have already been ported and removed)

### Requirement: CLI surface cleanup

The `--repl` CLI flag and `MILLIWAYS_REPL` environment variable SHALL be removed in the same change that deletes `internal/repl/`.

#### Scenario: --repl flag is no longer recognised

- **WHEN** a user runs `milliways --repl`
- **THEN** the CLI returns a usage error indicating the flag is unknown (cobra default behaviour)
- **AND** no REPL UI is launched

#### Scenario: MILLIWAYS_REPL env var is ignored

- **WHEN** a user runs `MILLIWAYS_REPL=1 milliways`
- **THEN** the launcher behaves as if the variable were unset
- **AND** the launcher proceeds with the milliways-term/wezterm path

#### Scenario: Launcher fallback messages no longer reference --repl

- **WHEN** `milliwaysd` fails to start
- **THEN** the error message guides the user to troubleshoot `milliwaysd` (logs, lock files)
- **AND** the error message does NOT contain the string `--repl`
