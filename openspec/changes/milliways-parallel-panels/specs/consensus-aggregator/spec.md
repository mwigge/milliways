## ADDED Requirements

### Requirement: Consensus aggregator produces confidence-weighted summary from MemPalace findings

After all slots in a ParallelGroup reach Done status, the consensus aggregator SHALL query MemPalace for all `has_finding` triples tagged with the group's `group_id`, group findings by file+symbol, deduplicate near-identical findings using token-overlap similarity (Jaccard ≥ 0.65 threshold), and assign a confidence tier based on agreement count. The summary SHALL be rendered in the right content pane of the parallel layout and printed to the calling chat session on exit.

#### Scenario: Auto-trigger on group completion

- **WHEN** the last slot in a ParallelGroup transitions to Done
- **THEN** the consensus aggregator SHALL run automatically within 2 seconds
- **AND** the navigator pane status bar SHALL update to `all done — consensus ready (press c)`
- **AND** pressing `c` SHALL display the summary in the right pane

#### Scenario: Manual trigger before all slots complete

- **WHEN** the user presses `c` while some slots are still Running
- **THEN** the aggregator SHALL run on the findings available so far
- **AND** SHALL prepend a warning: `[partial — N slot(s) still running]`

#### Scenario: HIGH confidence finding (3+ agents agree)

- **WHEN** 3 or more slots write findings to MemPalace for the same file with Jaccard similarity ≥ 0.65
- **THEN** the consensus summary SHALL show those findings under `[HIGH]` tier for that file
- **AND** SHALL list the agreeing providers in parentheses: e.g., `[HIGH] (claude, codex, local)`

#### Scenario: MEDIUM confidence finding (exactly 2 agents agree)

- **WHEN** exactly 2 slots write similar findings for the same file
- **THEN** the consensus summary SHALL show those findings under `[MEDIUM]` tier
- **AND** SHALL list the 2 agreeing providers

#### Scenario: LOW confidence finding (only 1 agent)

- **WHEN** only 1 slot writes a finding for a given file+symbol combination
- **THEN** the consensus summary SHALL show the finding under `[LOW]` tier with the source provider

#### Scenario: Deduplication of near-identical findings

- **WHEN** two findings for the same file have Jaccard token-overlap ≥ 0.65
- **THEN** the aggregator SHALL treat them as the same finding and merge them
- **AND** SHALL use the longer of the two finding texts as the canonical description

#### Scenario: No structured findings in MemPalace

- **WHEN** the aggregator queries MemPalace and finds no `has_finding` triples for the group
- **THEN** the summary SHALL render: `no structured findings — check individual panes for narrative output`
- **AND** SHALL NOT error

#### Scenario: Consensus summary output format

- **WHEN** the aggregator renders the summary
- **THEN** the output SHALL follow this structure:
  ```
  ── consensus: group <group-id-short> ──────────────────
  internal/server/auth.go
    [HIGH]   (claude, codex, local) missing input validation on token parameter
    [MEDIUM] (claude, codex) error not wrapped — loses stack trace at L142
  internal/server/handler.go
    [LOW]    (local) goroutine leak on context cancellation
  ────────────────────────────────────────────────────────
  3 HIGH · 1 MEDIUM · 1 LOW
  ```

### Requirement: Consensus summary available via milliwaysctl

The `milliwaysctl parallel consensus <group-id>` command SHALL output the consensus summary for a completed or interrupted group to stdout, suitable for piping or scripting.

#### Scenario: CLI consensus for completed group

- **WHEN** `milliwaysctl parallel consensus <group-id>` is run for a group in Done status
- **THEN** it SHALL print the same structured summary as the TUI renders
- **AND** SHALL exit with status 0

#### Scenario: CLI consensus for in-progress group

- **WHEN** `milliwaysctl parallel consensus <group-id>` is run for a group still Running
- **THEN** it SHALL print a partial summary with the `[partial]` prefix
- **AND** SHALL exit with status 0

#### Scenario: CLI consensus for unknown group

- **WHEN** `milliwaysctl parallel consensus <group-id>` is run for an unknown group-id
- **THEN** it SHALL print `group not found: <group-id>` to stderr and exit with status 1
