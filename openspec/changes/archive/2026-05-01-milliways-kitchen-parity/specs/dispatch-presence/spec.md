## MODIFIED Requirements

### Requirement: TUI presence consumes MemPalace-backed runtime events
Dispatch presence in the TUI SHALL continue to render runtime activity such as routing decisions, provider output, tool use, jobs, and memory events, but the event source SHALL be the MemPalace-backed runtime-event stream.

#### Scenario: Presence stream from substrate
- **WHEN** runtime events are appended during a dispatch
- **THEN** the TUI SHALL render them from the MemPalace event stream in the same chronological order they were written

#### Scenario: Visible switch and failover lines preserved
- **WHEN** a user-initiated switch or provider failover occurs
- **THEN** the TUI SHALL continue to show the corresponding visible presence lines sourced from MemPalace runtime events

#### Scenario: Legacy presence behaviour preserved
- **WHEN** the same sequence of route, provider_output, and tool_use events occurs before and after the substrate migration
- **THEN** the user-visible TUI presence ordering and semantics SHALL remain unchanged
