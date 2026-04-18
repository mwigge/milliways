## ADDED Requirements

### Requirement: Explicit 9-state dispatch state machine
The TUI SHALL track dispatch state as an explicit enum with exactly 9 states: Idle, Routing, Routed, Streaming, Done, Failed, Cancelled, Awaiting, Confirming. State transitions SHALL be driven by Event stream events, not ad-hoc boolean flags. The process map panel SHALL update on every state transition.

#### Scenario: State transitions on routing event
- **WHEN** a routing decision event arrives
- **THEN** state SHALL transition from Routing to Routed and the process map SHALL immediately reflect the new kitchen name

#### Scenario: State transitions on first output line
- **WHEN** the first EventText or EventCodeBlock from the kitchen arrives
- **THEN** state SHALL transition to Streaming

### Requirement: Routing feedback in process map (Tier 1)
The process map SHALL display the kitchen name as a colored badge, the routing reason truncated to panel width, the routing tier (keyword, enriched, learned, forced, fallback), and the risk level when available. Elapsed time SHALL be shown and updated at least every 100 ms during active dispatch.

#### Scenario: Kitchen badge shown after routing
- **WHEN** routing completes and the Routed state is entered
- **THEN** the process map SHALL show the kitchen name as a colored badge before any kitchen output arrives

#### Scenario: Elapsed time updates at 100 ms cadence
- **WHEN** a dispatch is in an active state (Routing, Routed, Streaming, Awaiting, Confirming)
- **THEN** the elapsed-time counter in the process map SHALL refresh at least every 100 ms

### Requirement: Pipeline step list in process map (Tier 2)
When vertical space permits, the process map SHALL show a step list below routing info for the steps: sommelier.route, kitchen.exec, ledger.write, quota.update. Each step SHALL display an icon (✓ done, ● active, · pending), name, and duration when complete. Steps SHALL update in real time as the dispatch progresses.

#### Scenario: Active step shown with bullet icon
- **WHEN** a pipeline step begins
- **THEN** its icon SHALL change from · to ● and the step name SHALL be highlighted

#### Scenario: Completed step shows duration
- **WHEN** a pipeline step finishes
- **THEN** its icon SHALL change to ✓ and its wall-clock duration SHALL appear beside the name

### Requirement: Dialogue overlays for EventQuestion and EventConfirm
When EventQuestion arrives the TUI SHALL enter Awaiting state and show a yellow-bordered overlay input. When EventConfirm arrives the TUI SHALL enter Confirming state and show an inline `[y/N]` prompt. Overlay input SHALL call `adapter.Send()` on submit. If `adapter.Send()` returns ErrNotInteractive the TUI SHALL log a warning and auto-answer (empty string for questions, "n" for confirms).

#### Scenario: Question overlay shown and answer sent
- **WHEN** EventQuestion arrives during Streaming state
- **THEN** state SHALL transition to Awaiting, a yellow-bordered overlay input SHALL appear, and on submit the answer SHALL be passed to adapter.Send()

#### Scenario: Confirm inline prompt
- **WHEN** EventConfirm arrives
- **THEN** an inline `[y/N]` prompt SHALL appear in the output viewport and the keypress SHALL be forwarded to adapter.Send()

#### Scenario: ErrNotInteractive auto-answer
- **WHEN** adapter.Send() returns ErrNotInteractive
- **THEN** the TUI SHALL log a warning and supply an empty string for questions or "n" for confirms without blocking

### Requirement: Ctrl+I context injection overlay
During Streaming state, pressing Ctrl+I SHALL open the overlay input with placeholder `"+ context: "`. On submit the text SHALL be written to the adapter via Send().

#### Scenario: Context injection while streaming
- **WHEN** the user presses Ctrl+I during Streaming state
- **THEN** the overlay SHALL open with the `"+ context: "` placeholder and on submit SHALL call adapter.Send() with the entered text

### Requirement: Headless routing feedback and safe defaults
In non-TUI mode with `--verbose`, routing decisions SHALL be printed to stderr as `[routed] kitchen_name`. All dialogue states SHALL have safe headless defaults (auto-answer, no blocking).

#### Scenario: Verbose headless routing line
- **WHEN** a dispatch runs in non-TUI mode with --verbose and routing completes
- **THEN** a `[routed] <kitchen_name>` line SHALL be written to stderr

#### Scenario: Headless dialogue auto-answer
- **WHEN** a question or confirm event arrives in headless mode
- **THEN** the system SHALL auto-answer without blocking the dispatch
