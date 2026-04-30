## MODIFIED Requirements

### Requirement: Turn records repositories accessed

Each turn SHALL record which repositories were accessed during that turn, in addition to existing fields (role, content, metadata).

**New fields added to turn:**
- `repos_accessed`: array of RepoAccess structs

#### Scenario: Turn accessing only active project
- **WHEN** a turn queries only the active project palace
- **THEN** turn.repos_accessed SHALL contain one entry:
  - repo: active repo name
  - access: "active"
  - operations: ["palace_search"] (or whichever operations were performed)

#### Scenario: Turn following citations
- **WHEN** a turn follows citations to 2 other palaces
- **THEN** turn.repos_accessed SHALL contain 3 entries:
  - Active repo with access: "active"
  - Each cited repo with access: "cited"
  - Each cited entry SHALL have citation_source: turn ID that introduced the citation

#### Scenario: Turn with no palace access
- **WHEN** a turn does not query any palace
- **THEN** turn.repos_accessed SHALL be empty or contain only CodeGraph operations

### Requirement: Turn stores project citations

Each turn SHALL store palace-qualified citation handles in metadata, in addition to existing metadata fields.

**New field added to turn.metadata:**
- `project_refs`: array of ProjectRef structs

#### Scenario: Citations from context injection
- **WHEN** context injection adds 3 palace results
- **THEN** turn.metadata.project_refs SHALL contain 3 ProjectRef entries
- **AND** each entry SHALL be independently resolvable

#### Scenario: Citations survive serialization
- **WHEN** turn is persisted and later loaded
- **THEN** project_refs SHALL be fully restored
- **AND** citation resolution SHALL work on loaded turn

### Requirement: RepoAccess structure

The RepoAccess struct SHALL capture sufficient detail for audit and replay.

#### Scenario: RepoAccess fields
- **WHEN** recording a repo access
- **THEN** RepoAccess SHALL contain:
  - repo: repository name
  - access: "active" or "cited"
  - operations: array of operation names performed
  - citation_source: turn ID that introduced this repo (null for active)

### Requirement: ProjectRef structure

The ProjectRef struct SHALL capture sufficient detail for cross-palace resolution.

#### Scenario: ProjectRef fields
- **WHEN** storing a project citation
- **THEN** ProjectRef SHALL contain:
  - palace_id: short identifier
  - palace_path: absolute path for resolution
  - drawer_id: unique drawer identifier
  - wing: drawer's wing
  - room: drawer's room
  - fact_summary: ≤100 char summary
  - captured_at: timestamp