## MODIFIED Requirements

### Requirement: Conversation continuity is substrate-backed
Provider continuity SHALL preserve the existing behaviour of same-block continuation and native-resume where supported, but the canonical conversation state SHALL be persisted and sourced from the MemPalace substrate rather than the legacy milliways-local SQLite store.

#### Scenario: Continuity after provider exhaustion
- **WHEN** the active provider becomes exhausted mid-conversation
- **THEN** milliways SHALL read the conversation state from MemPalace, construct the continuation payload from that state, and continue the conversation in the next kitchen without transcript loss

#### Scenario: Same-provider resume still works
- **WHEN** the same provider becomes available again and native resume is supported
- **THEN** milliways SHALL continue to use native resume semantics, with the canonical source of truth remaining the MemPalace substrate

#### Scenario: Restart preserves continuity
- **WHEN** milliways is restarted during an active or paused conversation
- **THEN** resuming the conversation SHALL recover the same transcript, working memory, and provider lineage from MemPalace
