## ADDED Requirements

### Requirement: Cross-palace citation resolution

The system SHALL resolve citation handles to their source palace, even when that palace is not the active project palace.

#### Scenario: Citation to non-active palace
- **WHEN** active project is "acme-saas"
- **AND** turn 5 has a citation to palace "work-docs", drawer "xyz123"
- **AND** user asks "what was that thing about Jeli?"
- **THEN** system SHALL query "work-docs" palace for drawer "xyz123"
- **AND** result SHALL be returned with palace attribution

#### Scenario: Citation to unreachable palace
- **WHEN** citation references palace_path "/old/deleted/palace"
- **AND** that path does not exist
- **THEN** system SHALL return error "Citation source unavailable: palace not found"
- **AND** citation SHALL be marked stale in turn metadata

### Requirement: Read-only access to non-active palaces

The system SHALL enforce read-only access when resolving citations to non-active palaces.

#### Scenario: Read operation allowed
- **WHEN** following a citation to non-active palace
- **THEN** mempalace_search and mempalace_kg_query SHALL be allowed

#### Scenario: Write operation blocked
- **WHEN** attempting mempalace_add_drawer to non-active palace
- **THEN** system SHALL reject with error "Write access denied: not the active project palace"

### Requirement: Conversation-scoped palace discovery

The system SHALL track all palaces cited in the current conversation for efficient cross-reference.

#### Scenario: Build palace set from conversation
- **WHEN** loading a conversation with turns citing 3 different palaces
- **THEN** system SHALL build a set of known palace paths: [active, cited1, cited2]
- **AND** these palaces SHALL be available for search via `/repos`

#### Scenario: New citation adds to set
- **WHEN** a turn injects a citation to a previously unseen palace
- **THEN** that palace SHALL be added to the conversation's palace set

### Requirement: Just-in-time citation verification

The system SHALL verify citation validity when resolving, not at write time.

#### Scenario: Valid citation
- **WHEN** resolving citation to drawer "xyz123" in palace "work-docs"
- **AND** that drawer exists
- **THEN** resolution SHALL succeed
- **AND** drawer content SHALL be returned

#### Scenario: Stale citation - drawer deleted
- **WHEN** resolving citation to drawer "xyz123" in palace "work-docs"
- **AND** that drawer no longer exists
- **THEN** resolution SHALL fail with "Citation stale: drawer not found"
- **AND** citation.verified_at SHALL be updated with failure status

#### Scenario: Stale citation - content changed
- **WHEN** resolving citation with fact_summary "uses Jeli API"
- **AND** drawer now contains different content
- **THEN** resolution SHALL succeed with warning "Citation content may have changed"
- **AND** current content SHALL be returned

### Requirement: Access rules from registry

The system SHALL honor access rules from `~/.milliways/projects.yaml` when resolving cross-palace citations.

#### Scenario: Read restricted by registry
- **WHEN** active project matches registry entry with `read: "project"`
- **AND** citation points to a different palace
- **THEN** citation resolution SHALL fail with "Cross-palace read denied by access rules"

#### Scenario: No registry entry - default allow
- **WHEN** active project has no registry entry
- **AND** citation points to a different palace
- **THEN** citation resolution SHALL succeed (default: `read: "all"`)