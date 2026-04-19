## MODIFIED Requirements

### Requirement: Exhaustion events are persisted to the substrate
Exhaustion detection logic SHALL remain unchanged in how it recognises structured and textual provider exhaustion, but on detection it SHALL emit a `RuntimeEvent` into the MemPalace event log rather than the legacy milliways-local sink.

#### Scenario: Structured exhaustion event logged
- **WHEN** a provider emits a structured rate-limit or exhaustion event
- **THEN** milliways SHALL append a `runtime_event` to MemPalace describing the provider, kind, and reset time if available

#### Scenario: Text exhaustion event logged
- **WHEN** a provider emits a textual exhaustion message matching the existing detection rules
- **THEN** milliways SHALL append a corresponding `runtime_event` to MemPalace with the parsed reset time when present

#### Scenario: Detection semantics unchanged
- **WHEN** the same provider output that matched exhaustion before the substrate migration is received after the migration
- **THEN** milliways SHALL classify it the same way and only the sink destination SHALL differ
