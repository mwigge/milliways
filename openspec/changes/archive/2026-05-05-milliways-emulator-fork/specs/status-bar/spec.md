## ADDED Requirements

### Requirement: Wezterm Lua status bar driven by `status.get`

The wezterm status bar SHALL render live milliways state via the `update-right-status` Lua hook. The hook SHALL call `milliwaysctl status --json` (one-shot) and format the result.

#### Scenario: Status bar renders on each tick

- **WHEN** wezterm fires `update-right-status` (default 1 Hz)
- **THEN** the Lua hook SHALL invoke `milliwaysctl status --json`
- **AND** SHALL render a string of the form `{agent} | turn:{n} | {in}↑/{out}↓ tok | ${cost} | quota: {pct}% | err:{n}`
- **AND** SHALL apply semantic colour from `milliways.theme`

### Requirement: Sub-second updates during a dispatch

During an active dispatch, the status bar SHALL update faster than the default 1 Hz so users see token counts ticking. This SHALL use a watch sidecar.

#### Scenario: Watch sidecar writes latest line to a file

- **WHEN** `milliwaysctl status --watch` is invoked
- **THEN** it SHALL subscribe to `status.subscribe`
- **AND** for each event, SHALL write to `${state}/status.cur.tmp`, fsync, then atomically rename to `${state}/status.cur` (POSIX `rename(2)` is atomic at the directory-entry level on APFS and ext4)
- **AND** SHALL debounce writes to no more than 4 Hz so the file system is not thrashed during high-frequency dispatches
- **AND** the Lua hook SHALL `os.time() - mtime < 2` BEFORE opening the file (skip the open if stale or absent), then `open + read + close` on each tick

#### Scenario: Watch sidecar started lazily and tied to parent lifetime

- **WHEN** the first wezterm window opens
- **THEN** `milliways.init` (Lua side) SHALL spawn `milliwaysctl status --watch &` once
- **AND** the spawned process SHALL exit when its parent (`milliways-term`) exits — on Linux via `prctl(PR_SET_PDEATHSIG, SIGTERM)` set inside `milliwaysctl`, on macOS via a kqueue watcher on `NOTE_EXIT` of the parent pid
- **AND** the Lua side SHALL ALSO send SIGTERM on the `gui-detached` event as a belt-and-braces fallback

### Requirement: Status bar is non-blocking

The Lua hook SHALL never block the wezterm UI. If `milliwaysctl status --json` takes longer than 200ms, the hook SHALL render the previous status and log a warning.

#### Scenario: Daemon slow

- **WHEN** the daemon takes 1s to respond to `status.get`
- **THEN** the status bar SHALL render the last-known string
- **AND** SHALL append a small `…` glyph indicating staleness
- **AND** SHALL NOT freeze the UI

### Requirement: Status bar fields

At minimum, the status bar SHALL display:

- Active agent id (or `–` if none)
- Session turn count
- Tokens in (with up arrow), tokens out (with down arrow)
- Cost in USD with two decimals
- Quota remaining percentage of the active agent's pantry quota
- Error count over the last 5 minutes

#### Scenario: Field overflow

- **WHEN** the rendered string would exceed the available status-bar width
- **THEN** lower-priority fields (error count, quota) SHALL be elided in that order
- **AND** the agent and tokens fields SHALL never be elided

### Requirement: Configurable via `milliways.lua`

Users SHALL be able to customise the status bar format and field selection by editing `milliways.lua`.

#### Scenario: Custom format

- **WHEN** the user sets `milliways.status_format = function(s) return s.agent .. ' / ' .. s.cost end`
- **THEN** the status bar SHALL use that function instead of the default
- **AND** SHALL fall back to the default if the function errors
