## ADDED Requirements

### Requirement: Conversation persists across runner switches
The conversation SHALL persist to mempalace when switching runners and be available when switching back.

#### Scenario: Switch claude to codex and back
- **WHEN** user switches from claude to codex, then back to claude
- **THEN** the claude conversation continues with full transcript intact

### Requirement: Conversation survives restarts
The conversation SHALL persist to mempalace and survive milliways process restart.

#### Scenario: Restart milliways
- **WHEN** user exits milliways and restarts it with `/session <name>`
- **THEN** the conversation resumes from where it left off

### Requirement: Shared memory across runners
Memory created in one runner SHALL be accessible to subsequent runners via mempalace.

#### Scenario: Memory shared across runners
- **WHEN** claude creates memory during a session
- **THEN** codex can access that memory when it becomes the active runner

### Requirement: Conversation primitive in mempalace fork
The mempalace fork SHALL provide conversation primitives: start/end conversation, append turn, checkpoint, resume.

#### Scenario: Mempalace conversation primitives
- **WHEN** milliways needs to persist a conversation
- **THEN** mempalace provides: `mempalace_conversation_start`, `mempalace_conversation_append_turn`, `mempalace_conversation_checkpoint`, `mempalace_conversation_resume`

### Requirement: Session naming
The user SHALL be able to name a session with `/session <name>` for easy resume.

#### Scenario: Name session
- **WHEN** user types `/session auth-refactor`
- **THEN** the current session is named "auth-refactor" in mempalace
