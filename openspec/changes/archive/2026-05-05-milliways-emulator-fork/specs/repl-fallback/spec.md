## ADDED Requirements

### Requirement: `milliways --repl` preserves the legacy in-host REPL

The existing in-host REPL SHALL remain available behind the `--repl` flag for at least one release after the cockpit ships. Behaviour SHALL be identical to today's `milliways` (no flags) prior to this change.

#### Scenario: Legacy invocation

- **WHEN** the user runs `milliways --repl`
- **THEN** the existing REPL SHALL launch with the same flags, config, and session-store paths as before
- **AND** none of the daemon, terminal fork, or cockpit components SHALL be required to be installed for `--repl` to work

#### Scenario: Legacy flags continue to work

- **WHEN** the user runs `milliways --repl --resume <session>` or any other previously documented flag
- **THEN** the flag SHALL behave identically to its pre-change behaviour

### Requirement: Default `milliways` invocation launches the cockpit

With no flag, `milliways` SHALL launch the new cockpit experience: ensure the daemon is running (start it detached if not) and exec `milliways-term`. The legacy REPL is no longer the default.

#### Scenario: Fresh shell, daemon not running

- **WHEN** the user runs `milliways` (no flags) and `milliwaysd` is not running
- **THEN** the launcher SHALL start `milliwaysd` detached
- **AND** SHALL wait up to 5s for the socket to become reachable
- **AND** SHALL exec `milliways-term`
- **AND** SHALL exit zero only after `milliways-term` exits

#### Scenario: Daemon fails to start within 5s

- **WHEN** the launcher started `milliwaysd` but the socket is not reachable after 5s
- **THEN** the launcher SHALL print a clear error including the daemon's stderr output (collected via tee to `${state}/milliwaysd.log`)
- **AND** SHALL suggest `milliways --repl` as an immediate fallback
- **AND** SHALL exit non-zero
- **AND** SHALL NOT exec `milliways-term`

#### Scenario: Daemon already running

- **WHEN** the user runs `milliways` and `milliwaysd` is already running
- **THEN** the launcher SHALL skip the daemon-start step
- **AND** SHALL exec `milliways-term` directly

#### Scenario: Cockpit components not installed

- **WHEN** the user runs `milliways` (no flags) but `milliways-term` is not on PATH
- **THEN** the launcher SHALL print a clear error pointing to the install instructions
- **AND** SHALL suggest `milliways --repl` as a fallback

### Requirement: Deprecation timeline documented

The `--repl` flag SHALL print a deprecation notice on every invocation starting from the release that ships this change. The notice SHALL include the planned removal release.

#### Scenario: Deprecation notice on stderr

- **WHEN** the user runs `milliways --repl`
- **THEN** the first line on stderr SHALL be a one-line deprecation notice with the planned removal version
- **AND** the REPL SHALL continue to start normally
