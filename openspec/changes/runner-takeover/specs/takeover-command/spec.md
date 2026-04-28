## ADDED Requirements

### Requirement: Explicit takeover command

The system SHALL provide a `/takeover [runner]` command that generates a structured briefing from the current session state, injects it as context for the next runner, and switches the active runner.

#### Scenario: Takeover with explicit target runner

- **WHEN** user runs `/takeover codex` while active runner is claude
- **THEN** system SHALL generate a structured briefing from current session turns
- **AND** system SHALL inject the briefing as a synthetic user turn in the new runner's history
- **AND** system SHALL switch the active runner to codex
- **AND** system SHALL print a confirmation: `[takeover] claude → codex — briefing injected`

#### Scenario: Takeover without target uses ring-next or sommelier

- **WHEN** user runs `/takeover` with no argument
- **AND** a rotation ring is active
- **THEN** system SHALL rotate to the next runner in the ring
- **WHEN** user runs `/takeover` with no argument
- **AND** no rotation ring is active
- **THEN** system SHALL route to the sommelier's best available runner

#### Scenario: Takeover to unavailable runner

- **WHEN** user runs `/takeover groq`
- **AND** "groq" is not a registered kitchen
- **THEN** system SHALL print `Unknown runner: groq` and abort without switching

#### Scenario: Takeover to same runner

- **WHEN** user runs `/takeover claude`
- **AND** active runner is already claude
- **THEN** system SHALL print `Already on claude — use a different runner` and abort

### Requirement: Structured briefing content

The system SHALL generate a briefing containing: the triggering task, a summary of the last three assistant turns, any git files changed since session start, extracted key decisions, and the recommended next step. The briefing SHALL be capped at 500 tokens. If the briefing would exceed 500 tokens, the decisions and progress sections SHALL be truncated first, preserving the task and next step.

#### Scenario: Briefing sections populated from session turns

- **WHEN** takeover is triggered
- **AND** session has at least one completed exchange
- **THEN** briefing SHALL include `## Current task` from the last user prompt that initiated work
- **AND** `## Progress` summarising assistant turns as bullet points (max 3 bullets)
- **AND** `## Next step` from the final sentence of the last assistant response

#### Scenario: Briefing when session has no prior turns

- **WHEN** takeover is triggered on a session with zero completed turns
- **THEN** system SHALL generate a minimal briefing: `[TAKEOVER] No prior context — starting fresh.`
- **AND** switch SHALL still proceed

#### Scenario: Files changed section populated via git

- **WHEN** takeover is triggered
- **AND** current working directory is a git repository
- **THEN** briefing SHALL include `## Files changed` listing output of `git diff --name-only HEAD` (max 20 files)
- **WHEN** current working directory is not a git repository
- **THEN** `## Files changed` section SHALL be omitted

### Requirement: MemPalace snapshot on takeover

When MemPalace MCP is configured, the system SHALL asynchronously write the takeover briefing as a drawer to the active palace before switching runners.

#### Scenario: MemPalace snapshot succeeds

- **WHEN** `MILLIWAYS_MEMPALACE_MCP_CMD` is set
- **AND** takeover is triggered
- **THEN** system SHALL call `mempalace_add_drawer` with key `handoff/<iso8601-timestamp>` and content equal to the briefing markdown
- **AND** the snapshot call SHALL be non-blocking — runner switch SHALL not wait for it

#### Scenario: MemPalace snapshot fails

- **WHEN** MemPalace MCP call returns an error
- **THEN** system SHALL log the error at debug level
- **AND** the runner switch SHALL proceed regardless
