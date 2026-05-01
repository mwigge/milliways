## ADDED Requirements

### Requirement: Forked MemPalace conversation schema
The `mempalace-milliways` fork SHALL add `conversation`, `segment`, `turn`, `runtime_event`, and `checkpoint` tables to MemPalace's storage backend. All additions SHALL be add-only; existing drawers, rooms, KG, and search tools SHALL continue to work unchanged.

#### Scenario: Existing MemPalace tests pass after fork additions
- **WHEN** the forked MemPalace is built and its full test suite is run
- **THEN** all pre-existing tests SHALL pass with no regressions

#### Scenario: Conversation CRUD round-trip
- **WHEN** a conversation is created, modified, and fetched
- **THEN** all fields (id, prompt, status, working_memory, context_bundle, active_segment_id) SHALL survive the round-trip intact

#### Scenario: Turn ordering by ordinal
- **WHEN** multiple turns are appended to a conversation
- **THEN** fetching turns SHALL return them ordered by `(conversation_id, ordinal)` ascending

#### Scenario: Event query by kind and time window
- **WHEN** runtime events of mixed kinds are appended and `mempalace_conversation_events_query` is called with a kind filter and time window
- **THEN** only matching events within the window SHALL be returned

### Requirement: MCP tool surface for conversations
The fork SHALL expose the following MCP tools: `mempalace_conversation_start`, `_end`, `_get`, `_list`, `_append_turn`, `_start_segment`, `_end_segment`, `_working_memory_get`, `_working_memory_set`, `_context_bundle_get`, `_context_bundle_set`, `_events_append`, `_events_query`, `_checkpoint`, `_resume`, `_lineage`. All tools SHALL be add-only and namespaced under `mempalace_conversation_*`.

#### Scenario: Start and end a conversation
- **WHEN** `mempalace_conversation_start` is called with a prompt
- **THEN** a conversation id SHALL be returned and the conversation SHALL be queryable via `mempalace_conversation_get`
- **WHEN** `mempalace_conversation_end` is subsequently called
- **THEN** the conversation status SHALL be updated to done

#### Scenario: Segment lineage
- **WHEN** three segments are started and ended sequentially on the same conversation
- **THEN** `mempalace_conversation_lineage` SHALL return all three in creation order

#### Scenario: Checkpoint and resume
- **WHEN** `mempalace_conversation_checkpoint` is called on an active conversation
- **THEN** the full state snapshot SHALL be stored
- **WHEN** `mempalace_conversation_resume` is called with that checkpoint id
- **THEN** the conversation state SHALL be restored to the snapshotted values

### Requirement: Fork governance and rebase cadence
The fork SHALL be maintained with a documented monthly rebase cadence from upstream MemPalace, and SHALL ship a `FORK.md` explaining delta scope and merge rules.

#### Scenario: FORK.md present
- **WHEN** the fork repository is inspected
- **THEN** a `FORK.md` SHALL exist at the root documenting the added conversation primitive scope and rebase rules

#### Scenario: Upstream test suite passes post-rebase
- **WHEN** a monthly rebase from upstream is applied
- **THEN** the upstream MemPalace test suite SHALL pass unmodified on the rebased fork
