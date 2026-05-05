## ADDED Requirements

### Requirement: Observability pane opens with `Cmd+Shift+O`

A keybinding SHALL open the observability cockpit as a new wezterm pane (default: a new tab; users can split it manually). The pane SHALL be backed by `AgentDomain` with the reserved agent id `_observability`.

#### Scenario: Open observability pane

- **WHEN** the user presses `Cmd+Shift+O`
- **THEN** a new tab SHALL open within 500ms
- **AND** the pane SHALL render the observability cockpit layout

### Requirement: Live span tail (top half)

The pane SHALL render a continuously updating tail of recent OTel spans. Each row SHALL show: start timestamp (relative), span name, duration, status (OK/ERROR with semantic colour), and the most relevant attribute (e.g., `agent_id` for agent spans).

#### Scenario: New span appears

- **WHEN** a new span lands in the daemon's ring buffer
- **THEN** within 1s a row SHALL appear at the top of the tail
- **AND** rows SHALL be coloured `theme.err` when status is ERROR, `theme.ok` otherwise
- **AND** the tail SHALL scroll automatically when new rows arrive unless the user has scrolled away

#### Scenario: Span row click expands attributes

- **WHEN** the user clicks a span row
- **THEN** the row SHALL expand to show all attributes as a key/value list
- **AND** clicking again collapses it

### Requirement: Throughput sparkline (top right)

A sparkline SHALL render tokens-per-second across all agents over the last 60 seconds, refreshed at 1 Hz. It SHALL be rendered Rust-side by the shared chart renderer from data fetched via `metrics.rollup.get` and emitted as a kitty-graphics PNG.

#### Scenario: Throughput updates at 1 Hz

- **WHEN** the pane is open
- **THEN** the sparkline image SHALL be replaced once per second
- **AND** wezterm's image cache SHALL serve the previous image until the new bytes arrive

### Requirement: Latency percentiles bar (middle)

A bar group SHALL show p50/p95/p99 dispatch latency per active agent over the last 5 minutes. Bars SHALL use semantic colour: green if below the configured SLO, yellow if within 80–100% of SLO, red if exceeded.

#### Scenario: Latency exceeds SLO

- **WHEN** an agent's p99 latency exceeds the configured SLO threshold
- **THEN** that agent's p99 bar SHALL render in `theme.err` colour
- **AND** the bar SHALL NOT animate or distract beyond the colour change

### Requirement: Cost-per-hour line chart (bottom left)

A line chart SHALL show cost burn rate over the last 60 minutes, rendered Rust-side by the shared chart renderer and emitted as a kitty-graphics PNG. The x-axis SHALL be 60 minutes, the y-axis SHALL be USD-per-hour.

#### Scenario: Cost trend visible

- **WHEN** the pane is open after at least 5 minutes of activity
- **THEN** the line chart SHALL show at least 5 data points
- **AND** the y-axis SHALL auto-scale to the observed range

### Requirement: Error rate badge + sparkline (bottom right)

A numeric badge SHALL show the total error count over the last 5 minutes. A sparkline below SHALL show the error count per minute over the last 60 minutes.

#### Scenario: Error rate increases

- **WHEN** an error span is recorded
- **THEN** within 1s the badge counter SHALL increment
- **AND** the sparkline's most recent bar SHALL grow

### Requirement: Pane is Rust-rendered, not daemon-streamed

The observability cockpit SHALL be assembled Rust-side by a special pane implementation. It SHALL use the `_observability` reservation in `AgentDomain` for picker/keybinding integration, but `AgentDomain::spawn_pane("_observability")` SHALL return a pane whose contents are produced by an in-process Rust renderer rather than a virtual PTY fed by daemon bytes.

The renderer SHALL:

1. Subscribe to `observability.subscribe` for new spans, `status.subscribe` for live status, and call `metrics.rollup.get` for time-series data on each repaint.
2. Compose the pane layout (span tail, latency bars, sparklines, line chart, error badge) Rust-side.
3. Produce chart PNGs via the shared chart renderer (`crates/milliways-term/milliways/src/charts/`) — same code path as the `/context` overlay charts.
4. Write the resulting frame (ANSI text + kitty-graphics escapes) directly into the pane's display surface.

This is the consequence of moving chart rendering to Rust (Decision 12). The daemon owns data, the renderer owns presentation.

#### Scenario: Renderer composes a frame

- **WHEN** any tracked metric or span set changes
- **THEN** the Rust-side renderer SHALL pull updated data via the relevant subscription/RPC
- **AND** SHALL re-compose the affected regions only (regions whose data hash is unchanged are not re-emitted)
- **AND** SHALL write the frame's bytes into the pane

### Requirement: Frame budget and cadence

To keep the pane from competing with agent streams for redraw time, frame emission SHALL be bounded:

- The renderer SHALL emit at most 1 frame per second (1 Hz cadence) under steady-state conditions.
- A single frame SHALL emit at most 32KB of bytes into the pane, including any new PNG payloads.
- PNG payloads SHALL use stable kitty-graphics image ids so unchanged charts cost nothing on subsequent frames (only an image-id reference is emitted).
- If a frame would exceed 32KB, the renderer SHALL emit image-id-only references for any chart whose payload was sent in the last 60s.

#### Scenario: Frame fits within budget

- **WHEN** all five regions are rendered with their sparklines and charts
- **THEN** the total bytes emitted per frame SHALL be ≤ 32KB measured at the pane write
- **AND** the cadence SHALL be ≥ 1s between frames in steady state

#### Scenario: Burst rate-limited

- **WHEN** a metric storm causes many simultaneous changes
- **THEN** the renderer SHALL coalesce changes into one frame per second
- **AND** SHALL NOT emit faster than 1 Hz even if the underlying data updates faster
