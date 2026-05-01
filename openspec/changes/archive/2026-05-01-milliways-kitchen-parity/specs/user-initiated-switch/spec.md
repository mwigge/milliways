## ADDED Requirements

### Requirement: /switch TUI command
The TUI SHALL support a `/switch <kitchen>` command that ends the current segment with reason `user_switch`, starts a new segment with the named kitchen, emits a `runtime_event` of kind `switch`, injects a continuation payload, and emits a visible system line explaining the switch.

#### Scenario: Successful user-initiated switch
- **WHEN** the user types `/switch codex` in the TUI with an active conversation
- **THEN** the current segment SHALL be ended, a new codex segment SHALL begin, and a system line SHALL appear: `[milliways] switched claude → codex — user requested. /back to reverse, /stick to disable.`

#### Scenario: Switch to unknown kitchen rejected
- **WHEN** the user types `/switch nonexistent-kitchen`
- **THEN** the TUI SHALL display an error line and no switch SHALL occur

#### Scenario: Switch event recorded in substrate
- **WHEN** a `/switch` completes
- **THEN** a `runtime_event` with kind=`switch` and the reason text SHALL be written to the MemPalace substrate

### Requirement: /stick TUI command
The TUI SHALL support a `/stick` command that toggles sticky mode on the active conversation. When sticky, the sommelier's continuous routing evaluator SHALL be a no-op for that conversation.

#### Scenario: /stick enables sticky mode
- **WHEN** the user types `/stick` with sticky mode inactive
- **THEN** the TUI SHALL display `[milliways] stuck to <kitchen> — auto-switching disabled. /stick again to release.` and the sommelier SHALL not auto-switch

#### Scenario: /stick releases sticky mode
- **WHEN** the user types `/stick` with sticky mode active
- **THEN** sticky mode SHALL be released and auto-routing SHALL resume

### Requirement: /back TUI command
The TUI SHALL support a `/back` command that reverses the most recent switch by starting a new segment with the previous kitchen. If no prior switch exists in the current conversation, it SHALL display a notice and take no action.

#### Scenario: /back reverses last switch
- **WHEN** the user types `/back` after a prior `/switch`
- **THEN** a new segment SHALL start with the previous kitchen and a system line SHALL confirm the reversal

#### Scenario: /back with no prior switch
- **WHEN** the user types `/back` on a conversation that has never been switched
- **THEN** the TUI SHALL display `[milliways] no prior switch to reverse` and no state change SHALL occur

### Requirement: --switch-to headless flag
The `milliways` CLI SHALL accept a `--switch-to <kitchen>` flag that, when combined with `--session <name>`, resolves the named paused or active session, performs the kitchen switch via the same code path as `/switch`, and continues with the supplied prompt.

#### Scenario: Headless switch continues conversation
- **WHEN** milliways is invoked with `--session mysession --switch-to codex "continue"`
- **THEN** the session SHALL switch to codex and the prompt SHALL be dispatched as the next turn

#### Scenario: Headless switch on non-existent session
- **WHEN** `--session unknown --switch-to codex` is used
- **THEN** milliways SHALL exit with a non-zero code and a descriptive error message
