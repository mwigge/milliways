## ADDED Requirements

### Requirement: Explicit takeover command

The system SHALL provide a `/takeover [runner]` command that generates a structured briefing from the current session state, injects it as context for the next runner, and switches the active runner.

#### Scenario: Takeover with explicit target runner

- **WHEN** user runs `/takeover codex` while active runner is claude
- **THEN** system SHALL generate a structured briefing from current session turns
- **AND** system SHALL inject the briefing as a synthetic user turn in the new runner's history
- **AND** system SHALL switch the active runner to codex
- **AND** system SHALL print a confirmation: `[takeover] claude → codex — briefing injected`

#### Scenario: Takeover without target uses ring-next

- **WHEN** user runs `/takeover` with no argument
- **AND** a rotation ring is active
- **THEN** system SHALL rotate to the next runner in the ring

#### Scenario: Takeover without target and without ring requires explicit target

- **WHEN** user runs `/takeover` with no argument
- **AND** no rotation ring is active
- **THEN** system SHALL print `No target runner — use /takeover <runner> or configure a ring with /takeover-ring`
- **AND** system SHALL abort without switching

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

### Requirement: TTY transcript sidecar

The system SHALL write an ANSI-stripped plain-text transcript of all terminal output to a stable per-working-directory `.log` file in the session store. The transcript SHALL capture every token written to the terminal — runner responses, tool-use progress lines, and user prompts — appended continuously as the session runs.

#### Scenario: Transcript file created in session store

- **WHEN** a new session is initialised
- **THEN** system SHALL open `current-<cwd-hash>.log` in the session store
- **AND** all terminal output SHALL be written to that file with ANSI escape sequences stripped

#### Scenario: ANSI codes removed from transcript

- **WHEN** a runner emits output containing ANSI colour codes or cursor-control sequences
- **THEN** the `.log` file SHALL contain only the plain-text content
- **AND** control sequences (ESC codes, CSI sequences, OSC sequences) SHALL be omitted

#### Scenario: Transcript used as briefing source on takeover

- **WHEN** takeover briefing is generated
- **AND** the session `.log` file exists and is readable
- **THEN** `GenerateBriefing` SHALL use the transcript as its source
- **AND** briefing SHALL reflect the full session history, not just the last 20 turns

#### Scenario: Briefing falls back to turn ring when transcript absent

- **WHEN** takeover briefing is generated
- **AND** no `.log` sidecar file exists (e.g. session pre-dates this feature)
- **THEN** `GenerateBriefing` SHALL fall back to the `ConversationTurn` ring buffer

#### Scenario: Transcript pruned with session

- **WHEN** auto-session pruning runs (keep-5 rule)
- **THEN** the `.log` file SHALL be deleted alongside its corresponding `.json` file
- **WHEN** a `.log` file is older than 7 days
- **THEN** it SHALL be deleted on milliways startup

### Requirement: MemPalace snapshot on takeover

When MemPalace MCP is configured, the system SHALL asynchronously write the takeover briefing as a drawer to the active palace after generating the briefing. The snapshot is best-effort and SHALL NOT block switching runners.

#### Scenario: MemPalace snapshot succeeds

- **WHEN** `MILLIWAYS_MEMPALACE_MCP_CMD` is set
- **AND** takeover is triggered
- **THEN** system SHALL call `mempalace_add_drawer` with wing `milliways`, room `handoff`, source file `handoff/<iso8601-timestamp>`, and content equal to the briefing markdown
- **AND** the snapshot call SHALL be non-blocking — runner switch SHALL not wait for it

#### Scenario: MemPalace snapshot fails

- **WHEN** MemPalace MCP call returns an error
- **THEN** system SHALL log the error at debug level
- **AND** the runner switch SHALL proceed regardless
