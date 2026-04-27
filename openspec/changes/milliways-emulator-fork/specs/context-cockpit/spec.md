## ADDED Requirements

### Requirement: `/context` overlay opens with `Cmd+Shift+C` (per-agent) and `Cmd+Shift+G` (aggregated)

The `/context` cockpit SHALL be invokable as a wezterm overlay over the active tab. `Cmd+Shift+C` opens the overlay for the agent in the focused pane (or a no-op if the focused pane is not an agent pane). `Cmd+Shift+G` opens the aggregated overlay regardless of focus.

#### Scenario: Open per-agent overlay

- **WHEN** the focused pane is a claude agent pane and the user presses `Cmd+Shift+C`
- **THEN** an overlay SHALL render in the active tab within 100ms of keypress
- **AND** the overlay SHALL display the per-agent layout populated from `context.get({agent_id: 'claude'})`

#### Scenario: Open aggregated overlay

- **WHEN** the user presses `Cmd+Shift+G`
- **THEN** the overlay SHALL render the aggregated layout populated from `context.get_all()`
- **AND** SHALL show one mini-card per active agent plus a totals header

#### Scenario: Press Esc closes overlay

- **WHEN** the overlay is visible and the user presses `Esc`
- **THEN** the overlay SHALL close and focus returns to the previously focused pane

### Requirement: Per-agent layout displays the full agent context

The per-agent overlay SHALL display, at minimum:

- Header row: agent id, model name, session id (truncated), uptime
- Token-budget visualisation: donut chart (kitty-graphics PNG) showing in/out/cached split, with absolute counts
- Conversation timeline sparkline: tokens over the last N turns (kitty-graphics PNG)
- Tools list: each tool name with last-used timestamp
- MCP servers list: each server with connection status and tool count
- Files in context: tree-rendered list with byte-size annotation
- Cost meter: numeric + horizontal bar against the configured budget cap
- Error badge: count of recent errors with the most recent error message

#### Scenario: Required fields all visible

- **WHEN** the per-agent overlay is open
- **THEN** every field listed above SHALL be visible without scrolling on a 1920x1080 display at the milliways baseline font (JetBrains Mono 14pt, configurable via `milliways.lua`)
- **AND** any field SHALL gracefully render `—` when its data is unavailable

### Requirement: Aggregated layout displays a totals header plus per-agent cards

The aggregated overlay SHALL display:

- Totals header: total tokens (in/out/cached) across all agents, total cost, active agent count, total error count
- Routing strip: bar chart of the last 50 routing decisions (kitty-graphics PNG), one stripe per agent, hover/click to focus
- Quota strip: pantry quota meters, one per agent, with remaining percentage
- Per-agent grid: one mini-card per agent with model, tokens, cost, error count, last-active timestamp

#### Scenario: Aggregated overlay shows all agents

- **WHEN** the aggregated overlay is open and at least one agent is active
- **THEN** the layout SHALL include every active agent in the per-agent grid
- **AND** the totals header SHALL exactly equal the sum of per-agent fields

### Requirement: Live updates during dispatch

The overlay SHALL subscribe to `context.subscribe` while open. Field updates SHALL render within 200ms of the daemon emitting a delta. Charts SHALL only re-render Rust-side when the `data_hash` returned by `context.chart_data` changes.

#### Scenario: Tokens update during dispatch

- **WHEN** the overlay is open over claude's pane and a prompt is dispatched
- **THEN** the tokens-in counter SHALL increment within 200ms of each daemon update
- **AND** the timeline sparkline SHALL extend to include the new turn

#### Scenario: Chart cache hit

- **WHEN** the `data_hash` for a chart is unchanged across two consecutive updates
- **THEN** the overlay SHALL NOT re-render the PNG and SHALL NOT re-emit the kitty-graphics escape
- **AND** wezterm's image cache SHALL serve the existing image keyed by the stable image-id

### Requirement: Visual quality bar — mechanically falsifiable criteria

The overlay SHALL meet explicit, mechanically checkable visual criteria. "Comparable visual fidelity to Claude Code's `/context`" is the directional intent; the rules below are the gate:

- **Graphics, not glyphs.** The per-agent overlay SHALL render at minimum 2 distinct kitty-graphics images (e.g., token-budget donut + timeline sparkline). The aggregated overlay SHALL render at minimum 1 graphics image (the routing-decisions strip). Image PNG bytes SHALL be assembled Rust-side via the chart renderer in `crates/milliways-term/milliways/src/charts/` from structured data fetched via `context.chart_data` — the daemon SHALL NOT render images. Counts asserted by an integration test that captures the overlay output and counts `\x1b_G` escape openings.
- **Typography hierarchy.** At least 3 distinct font weights SHALL be in use simultaneously: bold (e.g., model name), regular (attribute values), and dim/light (metadata such as session id, timestamps).
- **Colour discipline.** Source files under `crates/milliways-term/milliways/src/context_overlay/` SHALL contain ZERO raw hex literals (regex `#[0-9a-fA-F]{3,8}`). All colours SHALL come from `theme.*` references resolved from `milliways.lua`. Enforced by a `cargo test` that greps the source tree.
- **Information density.** The per-agent overlay SHALL display every required field above the fold on a 1920x1080 display at the baseline font (JetBrains Mono 14pt). A reviewer counts visible labelled data points; minimum 18 (matched against the field list in the previous requirement: header 4, donut absolutes 3, timeline points ≥3, tools list ≥1, MCP list ≥1, files list ≥1, cost meter 2, error badge 2, plus session metadata 1).
- **No primary-region box-drawing borders.** Source files in the same path SHALL contain ZERO Unicode box-drawing characters (regex `[─-╿]`) outside of `// allow-box` annotated lines. Whitespace and weight do the structuring.

#### Scenario: Visual checks pass automatically

- **WHEN** the test suite runs `cargo test -p milliways context_overlay`
- **THEN** the hex-literal grep SHALL find zero matches outside `theme.rs`
- **AND** the box-drawing grep SHALL find zero unannotated matches
- **AND** the integration overlay-snapshot test SHALL count ≥2 kitty-graphics image starts in per-agent output

#### Scenario: Visual review against Claude Code

- **WHEN** a maintainer compares a screenshot of `Cmd+Shift+C` to a screenshot of Claude Code's `/context`
- **THEN** the milliways overlay SHALL be of comparable visual fidelity
- **AND** any deviation SHALL be justified in the review notes
- **AND** a deviation SHALL NOT be grounds for sign-off rejection if all four mechanical criteria above pass — only the mechanical gate is blocking
