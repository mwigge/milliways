## ADDED Requirements

### Requirement: Context injection at turn boundary

The system SHALL query the active project palace after each user turn and inject relevant results into the context bundle.

#### Scenario: Palace available, results found
- **WHEN** user submits a turn containing "resilience-learning"
- **AND** project_palace is set
- **AND** palace search returns 2 matching drawers
- **THEN** context_bundle.project_hits SHALL contain those 2 results
- **AND** turn.metadata.project_refs SHALL contain citation handles for those results

#### Scenario: Palace available, no results
- **WHEN** user submits a turn containing "xyz123random"
- **AND** project_palace is set
- **AND** palace search returns 0 matching drawers
- **THEN** context_bundle.project_hits SHALL be empty
- **AND** turn.metadata.project_refs SHALL be empty

#### Scenario: No palace configured
- **WHEN** user submits a turn
- **AND** project_palace is nil
- **THEN** context injection SHALL be skipped
- **AND** no error SHALL be raised

### Requirement: Topic extraction from user turn

The system SHALL extract entities and topics from the user's message to use as search queries.

#### Scenario: Named entity extraction
- **WHEN** user message contains "let's work on the resilience-learning change"
- **THEN** search query SHALL include "resilience-learning"

#### Scenario: Multiple topics
- **WHEN** user message contains "what did we decide about Jeli sync and the metrics API?"
- **THEN** search queries SHALL include "Jeli sync" and "metrics API"

### Requirement: Result limit for context injection

The system SHALL limit injected results to prevent context bloat.

#### Scenario: Default limit
- **WHEN** palace search returns 10 matching drawers
- **AND** no custom limit is configured
- **THEN** context_bundle.project_hits SHALL contain at most 3 results
- **AND** results SHALL be ordered by relevance score

#### Scenario: Configured limit
- **WHEN** carte.yaml contains `project_context_limit: 5`
- **AND** palace search returns 10 matching drawers
- **THEN** context_bundle.project_hits SHALL contain at most 5 results

### Requirement: Citation handle creation

The system SHALL create palace-qualified citation handles for every injected result.

#### Scenario: Citation handle structure
- **WHEN** a drawer is injected into context
- **THEN** citation handle SHALL contain:
  - palace_id: short identifier for the palace
  - palace_path: absolute path to palace
  - drawer_id: unique drawer identifier
  - wing: drawer's wing
  - room: drawer's room
  - fact_summary: brief summary of content (≤100 chars)
  - captured_at: timestamp of injection

### Requirement: Citation storage in turn metadata

The system SHALL store all citation handles in the turn's metadata.

#### Scenario: Multiple citations
- **WHEN** 3 drawers are injected into context
- **THEN** turn.metadata.project_refs SHALL contain 3 citation handles
- **AND** each citation handle SHALL be independently resolvable

#### Scenario: Citations persist
- **WHEN** turn is saved to conversation palace
- **THEN** project_refs SHALL be persisted with the turn
- **AND** project_refs SHALL survive conversation reload