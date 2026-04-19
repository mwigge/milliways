## MODIFIED Requirements

### Requirement: Segment records session context

Each segment SHALL record the repository context at segment start, in addition to existing fields (kitchen, timestamps, end_reason).

**New fields added to segment:**
- `repo_context`: struct containing repository state at segment start

#### Scenario: Segment with full repo context
- **WHEN** a new segment starts
- **AND** project resolution succeeded with palace
- **THEN** segment.repo_context SHALL contain:
  - repo_root: absolute path to repository
  - repo_name: directory name
  - branch: current git branch
  - commit: current commit SHA (short form)
  - codegraph_symbols: number of indexed symbols
  - palace_drawers: number of drawers in palace

#### Scenario: Segment without palace
- **WHEN** a new segment starts
- **AND** project resolution succeeded without palace
- **THEN** segment.repo_context SHALL contain:
  - repo_root, repo_name, branch, commit, codegraph_symbols: populated
  - palace_drawers: null

#### Scenario: Query repos worked on
- **WHEN** querying segments from the last 7 days
- **THEN** unique repo_name values SHALL be extractable
- **AND** last-touched timestamp per repo SHALL be derivable from segment.started_at