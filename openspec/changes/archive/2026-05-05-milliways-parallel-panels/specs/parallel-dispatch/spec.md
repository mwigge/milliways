## ADDED Requirements

### Requirement: /parallel slash command dispatches prompt to N providers simultaneously

The system SHALL provide a `/parallel <prompt>` slash command in the chat REPL. When invoked, it SHALL call `parallel.dispatch` on the daemon with the given prompt and the active pool's provider list, then launch the parallel panel layout. The calling chat session SHALL remain open and receive the consensus summary when the group completes.

#### Scenario: /parallel with no --providers flag uses pool config

- **WHEN** the user runs `/parallel review internal/server/`
- **AND** the active carte.yaml defines `pool.members: [claude, codex, local]`
- **THEN** the system SHALL open one slot per pool member (3 slots total)
- **AND** SHALL dispatch the prompt `review internal/server/` to each slot concurrently
- **AND** SHALL print `[parallel] group <group-id> started — 3 slots` to the calling session

#### Scenario: /parallel with --providers flag overrides pool config

- **WHEN** the user runs `/parallel --providers claude,codex review internal/server/`
- **THEN** the system SHALL open exactly 2 slots (claude, codex)
- **AND** SHALL ignore the pool config for provider selection

#### Scenario: /parallel with unknown provider name

- **WHEN** the user runs `/parallel --providers claude,groq <prompt>`
- **AND** `groq` is not a registered kitchen
- **THEN** the system SHALL print `unknown provider: groq` and abort without opening any slots

#### Scenario: /parallel with empty prompt

- **WHEN** the user runs `/parallel` with no prompt text
- **THEN** the system SHALL print `usage: /parallel [--providers <list>] <prompt>` and abort

### Requirement: parallel.dispatch RPC opens N sessions and returns immediately

The `parallel.dispatch` RPC SHALL open N concurrent daemon sessions using `agent.open`, prime each with MemPalace baseline context for any file path detected in the prompt, create a ParallelGroup record in SQLite, and return `{group_id, slots: [{handle, provider}]}` immediately without waiting for sessions to complete.

#### Scenario: Successful dispatch with 3 providers

- **WHEN** `parallel.dispatch` is called with prompt `review internal/server/` and providers `[claude, codex, local]`
- **THEN** the RPC SHALL return within 2 seconds with a `group_id` (UUID) and 3 slot records
- **AND** each slot record SHALL contain `handle` (existing session handle format) and `provider`
- **AND** all 3 sessions SHALL be running concurrently in the daemon

#### Scenario: MemPalace baseline injection before session start

- **WHEN** the prompt contains a detectable file path (e.g., `internal/server/`)
- **AND** MemPalace contains prior findings for that path from a previous group
- **THEN** each session SHALL be primed with a synthetic context message containing those prior findings before the user prompt is sent
- **AND** the priming message SHALL be labelled `[prior findings from mempalace]` so the agent can distinguish it from user input

#### Scenario: MemPalace unavailable during dispatch

- **WHEN** the MemPalace MCP server is unreachable at dispatch time
- **THEN** `parallel.dispatch` SHALL proceed without baseline injection
- **AND** SHALL log a warning at slog WARN level
- **AND** SHALL NOT fail the dispatch

#### Scenario: Provider unavailable at dispatch time

- **WHEN** one of the requested providers is registered but has `auth_status != ok`
- **THEN** `parallel.dispatch` SHALL skip that provider, open slots for the remaining providers, and include a `skipped: [{provider, reason}]` array in the response

### Requirement: ParallelGroup is persisted to SQLite and survives daemon restart

The daemon SHALL store each ParallelGroup in `mw_parallel_groups` and each slot in `mw_parallel_slots`. On daemon restart, the group and slot records SHALL be readable via `group.status` and `group.list` RPCs. Running slots that were in-flight at restart SHALL be marked `interrupted`.

#### Scenario: group.status returns current slot states

- **WHEN** `group.status {group_id}` is called for a running group
- **THEN** the response SHALL contain `group_id`, `prompt`, `created_at`, `status` (running|done|interrupted), and a `slots` array each with `handle`, `provider`, `status` (running|done|error|interrupted), `started_at`, `completed_at` (if done), `tokens_in`, `tokens_out`

#### Scenario: group.list returns recent groups

- **WHEN** `group.list {}` is called
- **THEN** the response SHALL return up to 20 most-recent groups ordered by `created_at` descending
- **AND** each entry SHALL include `group_id`, `prompt` (truncated to 80 chars), `created_at`, `status`, and `slot_count`

#### Scenario: Interrupted slots on daemon restart

- **WHEN** the daemon restarts while a ParallelGroup has slots with status `running`
- **THEN** those slots SHALL be updated to status `interrupted` on daemon startup
- **AND** the group status SHALL be set to `interrupted`
