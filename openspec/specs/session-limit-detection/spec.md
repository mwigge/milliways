# session-limit-detection Specification

## Purpose
TBD - created by archiving change runner-takeover. Update Purpose after archive.
## Requirements
### Requirement: Runners report SessionLimitReached signal

Each runner's `Execute` method SHALL return the typed `ErrSessionLimit` error when it detects that the session has been exhausted (context window, token quota, or rate limit). The terminal dispatch loop SHALL treat that typed error as the `SessionLimitReached` signal before surfacing any user-visible failure.

#### Scenario: Claude context window exhausted

- **WHEN** claude runner receives stderr containing `context window` or `session limit`
- **OR** claude runner exits with a signal consistent with context overflow
- **THEN** runner SHALL return `ErrSessionLimit`
- **AND** the terminal dispatch loop SHALL handle it before printing a user-visible failure

#### Scenario: Codex max turns reached

- **WHEN** codex runner receives a JSON event with type `max_turns` or `context_length_exceeded`
- **THEN** runner SHALL return `ErrSessionLimit`

#### Scenario: MiniMax quota exceeded

- **WHEN** MiniMax HTTP runner receives HTTP 429 with body containing `quota_exceeded`
- **THEN** runner SHALL return `ErrSessionLimit`

#### Scenario: Copilot rate limited

- **WHEN** copilot runner receives stderr matching `rate limit` pattern
- **THEN** runner SHALL return `ErrSessionLimit`

#### Scenario: Non-limit error not misclassified

- **WHEN** claude runner exits non-zero for a reason unrelated to session limits (e.g. auth failure)
- **THEN** runner SHALL NOT return `ErrSessionLimit`
- **AND** runner SHALL surface the original error to the terminal as normal

### Requirement: Terminal intercepts limit signal and auto-rotates

When `ErrSessionLimit` is received during dispatch and a rotation ring is configured, the terminal SHALL automatically trigger a takeover to the next ring member and re-dispatch the original prompt.

#### Scenario: Auto-rotate on limit with ring active

- **WHEN** claude returns `ErrSessionLimit`
- **AND** rotation ring `[claude, codex]` is configured
- **THEN** terminal SHALL generate a takeover briefing
- **AND** rotate ring to codex
- **AND** re-dispatch the original user prompt to codex with the briefing injected
- **AND** print `[auto-takeover] claude session limit — continuing on codex`

#### Scenario: Limit with no ring configured

- **WHEN** runner returns `ErrSessionLimit`
- **AND** no rotation ring is configured
- **THEN** REPL SHALL print `Session limit reached on <runner>. Use /takeover-ring to enable auto-rotation, or /takeover <runner> to continue manually.`
- **AND** SHALL NOT switch runners automatically

#### Scenario: Re-dispatch prompt preserved exactly

- **WHEN** auto-rotate triggers
- **THEN** the re-dispatched prompt SHALL be identical to the original user input
- **AND** the briefing SHALL be injected as a synthetic history turn, not prepended to the prompt text

### Requirement: Auto-rotate cap prevents infinite loops

The system SHALL track the number of consecutive auto-rotations within a single user turn. If rotations exceed the ring length, the system SHALL halt and surface an error.

#### Scenario: Rotation cap reached

- **WHEN** all ring runners have each emitted `session.limit_reached` for the same user turn
- **THEN** system SHALL stop rotating
- **AND** system SHALL print `[ring] all runners hit session limits on this turn — giving up`
- **AND** SHALL surface the last error to the user without further dispatch

