## ADDED Requirements

### Requirement: Post-session hook writes structured findings to MemPalace

When a parallel slot's session transitions to Done, the daemon SHALL parse the agent's final assistant message for structured findings and write each finding to MemPalace via `kg_add`. Each triple SHALL be tagged with `source=<provider>` and `group_id=<group-id>` properties. This enables cross-run memory: subsequent `/parallel` runs targeting the same file path receive prior findings as baseline context.

#### Scenario: Finding extracted from file-path-anchored output

- **WHEN** a slot completes with a final assistant message containing lines of the form:
  ```
  internal/server/auth.go: missing input validation on token parameter
  ```
- **THEN** the post-session hook SHALL call `kg_add` with:
  - subject: `file:internal/server/auth.go`
  - predicate: `has_finding`
  - object: `missing input validation on token parameter`
  - properties: `{source: "claude", group_id: "<group-id>", ts: "<iso8601>"}`

#### Scenario: Multiple findings from one session

- **WHEN** the final message contains 3 file-anchored findings
- **THEN** the hook SHALL make 3 separate `kg_add` calls, one per finding
- **AND** all 3 SHALL carry the same `source` and `group_id` properties

#### Scenario: No file-anchored findings in response

- **WHEN** the final assistant message contains no lines matching the `<path>: <description>` pattern
- **THEN** the hook SHALL make no `kg_add` calls
- **AND** SHALL log at slog DEBUG level: `[memwrite] no findings extracted from <provider> response`
- **AND** SHALL NOT fail the slot completion

#### Scenario: MemPalace write failure

- **WHEN** a `kg_add` call to MemPalace returns an error
- **THEN** the hook SHALL log at slog WARN level with the error
- **AND** SHALL continue processing remaining findings from the same session
- **AND** SHALL NOT fail the slot completion or affect the consensus aggregator

#### Scenario: Prior findings injected as baseline context

- **WHEN** `/parallel` is invoked with a prompt containing `internal/server/`
- **AND** MemPalace contains `has_finding` triples with `subject_prefix=file:internal/server/` from a previous run
- **THEN** the dispatch coordinator SHALL query MemPalace for those triples before sending the prompt
- **AND** SHALL prepend a synthetic context message to each slot's conversation:
  ```
  [prior findings from mempalace]
  internal/server/auth.go: missing input validation on token parameter (source: claude, 2026-05-01)
  ```
- **AND** the user prompt SHALL follow this context message unchanged

#### Scenario: Prior findings capped to avoid context bloat

- **WHEN** MemPalace returns more than 20 prior findings for the target path
- **THEN** the baseline injection SHALL use only the 20 most-recent findings (ordered by `ts` descending)
- **AND** SHALL append a note: `[truncated — showing 20 of N prior findings]`
