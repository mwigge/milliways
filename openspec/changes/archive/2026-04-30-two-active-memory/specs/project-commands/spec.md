## ADDED Requirements

### Requirement: /project command

The system SHALL provide a `/project` command that displays detailed project information.

#### Scenario: Full project info
- **WHEN** user types `/project`
- **THEN** system SHALL display:
  - Repository path
  - Git remote URL
  - Current branch
  - CodeGraph symbol count and last indexed time
  - Palace drawer count, wing count, room count (if palace exists)
  - Access rules (read/write permissions)

#### Scenario: No palace
- **WHEN** user types `/project`
- **AND** no palace is configured
- **THEN** palace section SHALL show "(none) | run `mempalace init` to enable"

### Requirement: /repos command

The system SHALL provide a `/repos` command that lists all repositories accessed in the conversation.

#### Scenario: List repos
- **WHEN** user types `/repos`
- **THEN** system SHALL display a table:
  - Repo name
  - Access type (active/cited)
  - Last accessed time
  - Drawer count (if palace)
  - Symbol count

#### Scenario: Active repo marker
- **WHEN** displaying repos list
- **THEN** active repo SHALL be marked with `●`
- **AND** cited repos SHALL be marked with `○`

### Requirement: /palace command

The system SHALL provide a `/palace` command for project palace operations.

#### Scenario: /palace with no args
- **WHEN** user types `/palace`
- **THEN** system SHALL display palace status (same as `/project` palace section)

#### Scenario: /palace init
- **WHEN** user types `/palace init`
- **AND** no palace exists
- **THEN** system SHALL initialize `.mempalace/` in repo root
- **AND** display confirmation "Palace initialized at /path/.mempalace"

#### Scenario: /palace init when exists
- **WHEN** user types `/palace init`
- **AND** palace already exists
- **THEN** system SHALL display "Palace already exists at /path/.mempalace"

#### Scenario: /palace search
- **WHEN** user types `/palace search resilience`
- **THEN** system SHALL search active palace for "resilience"
- **AND** display results inline with drawer IDs, wings, rooms

#### Scenario: /palace search no palace
- **WHEN** user types `/palace search X`
- **AND** no palace is configured
- **THEN** system SHALL display "No project palace configured. Run `/palace init` to create one."

### Requirement: /codegraph command

The system SHALL provide a `/codegraph` command for CodeGraph operations.

#### Scenario: /codegraph with no args
- **WHEN** user types `/codegraph`
- **THEN** system SHALL display:
  - Index path
  - Symbol count by language
  - Last indexed timestamp
  - Index status (ready/indexing/stale)

#### Scenario: /codegraph reindex
- **WHEN** user types `/codegraph reindex`
- **THEN** system SHALL trigger background reindex
- **AND** display "Reindexing started..."

#### Scenario: /codegraph search
- **WHEN** user types `/codegraph search UserService`
- **THEN** system SHALL search for symbols matching "UserService"
- **AND** display results with file paths and line numbers

### Requirement: Command error handling

The system SHALL provide helpful error messages for invalid command usage.

#### Scenario: Unknown subcommand
- **WHEN** user types `/palace xyz`
- **THEN** system SHALL display "Unknown subcommand 'xyz'. Usage: /palace [init|search <query>]"

#### Scenario: Missing required argument
- **WHEN** user types `/codegraph search`
- **THEN** system SHALL display "Missing search query. Usage: /codegraph search <query>"