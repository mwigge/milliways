## ADDED Requirements

### Requirement: Project status in TUI header

The system SHALL display the active project name and status in the TUI header or status bar.

#### Scenario: Full project with palace
- **WHEN** project resolution succeeds with CodeGraph and palace
- **THEN** TUI header SHALL show:
  - Project name (repo directory name)
  - CodeGraph status with symbol count
  - Palace status with drawer count

#### Scenario: Project without palace
- **WHEN** project resolution succeeds with CodeGraph but no palace
- **THEN** TUI header SHALL show:
  - Project name
  - CodeGraph status with symbol count
  - "palace: (none)"

#### Scenario: CodeGraph indexing in progress
- **WHEN** CodeGraph is being initialized
- **THEN** TUI status SHALL show "codegraph: indexing..."
- **AND** status SHALL update when indexing completes

### Requirement: Compact status bar format

The system SHALL support a compact status bar format for smaller terminals.

#### Scenario: Compact display
- **WHEN** terminal width is less than 100 columns
- **THEN** status bar SHALL show: `project | codegraph ✓ | palace ✓ | kitchen | repos: N`

#### Scenario: Full display
- **WHEN** terminal width is 100 columns or more
- **THEN** header SHALL show expanded format with counts and timestamps

### Requirement: Recent repos display

The system SHALL track and display repositories accessed in the current session.

#### Scenario: Single repo session
- **WHEN** only the active project has been accessed
- **THEN** repos display SHALL show 1 repo with "(active)" marker

#### Scenario: Multi-repo session via citations
- **WHEN** citations have been followed to 2 other palaces
- **THEN** repos display SHALL show 3 repos:
  - Active repo with "(active)" marker and timestamp
  - Cited repos with "(cited)" marker and timestamps

#### Scenario: Repos sorted by recency
- **WHEN** displaying repos list
- **THEN** repos SHALL be sorted by last access time, most recent first

### Requirement: Kitchen status integration

The system SHALL continue to show kitchen status alongside project status.

#### Scenario: Kitchen in status
- **WHEN** displaying project status
- **THEN** current kitchen name SHALL be shown
- **AND** sticky mode indicator SHALL be shown if enabled