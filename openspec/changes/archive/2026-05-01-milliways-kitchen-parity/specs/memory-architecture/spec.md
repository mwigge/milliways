## MODIFIED Requirements

### Requirement: Typed memory layers map onto MemPalace primitives
The existing working, episodic, semantic, and procedural memory layers SHALL map onto MemPalace primitives, with working memory stored as typed conversation state and episodic recall replayed from MemPalace runtime events.

#### Scenario: Working memory round-trip
- **WHEN** milliways writes working memory fields such as summary, open_questions, active_goals, and next_action
- **THEN** those fields SHALL round-trip through MemPalace without loss of type information

#### Scenario: Episodic replay from runtime events
- **WHEN** milliways reconstructs recent conversation history for recall or resume
- **THEN** episodic memory SHALL be derived from ordered runtime events and turns stored in MemPalace

#### Scenario: Semantic and procedural memory remain available
- **WHEN** milliways performs memory retrieval unrelated to the active conversation
- **THEN** existing MemPalace semantic and procedural memory capabilities SHALL continue to function unchanged
