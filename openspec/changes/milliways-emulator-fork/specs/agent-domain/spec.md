## ADDED Requirements

### Requirement: `AgentDomain` implements wezterm's `Domain` trait

Milliways SHALL provide a Rust type `AgentDomain` that implements wezterm's `Domain` trait. It SHALL appear alongside wezterm's built-in `LocalDomain`, `SshDomain`, and `MuxDomain` so that agent panes can be created from any wezterm surface that creates panes (new tab, split, etc.).

#### Scenario: AgentDomain registered at startup

- **WHEN** `milliways-term` initialises
- **THEN** `milliways::init` SHALL register an instance of `AgentDomain` with the global mux
- **AND** the domain SHALL be queryable via `wezterm.mux.get_domain('agents')`

### Requirement: `spawn_pane` opens an agent stream over RPC

When wezterm requests a new pane from `AgentDomain`, the domain SHALL:

1. Connect to `milliwaysd` over UDS (or reuse a pooled connection)
2. Call `agent.open({agent_id, session_id?})` and receive a `{handle, pty_size}`
3. Allocate a virtual PTY pair locally
4. Call `agent.stream({handle})`, receive `{stream_id}`, open the NDJSON sidecar
5. Run two background tasks: NDJSON → PTY master writes; PTY slave reads → `agent.send`
6. Return a wezterm `Pane` backed by the PTY

#### Scenario: Open Claude pane

- **WHEN** Lua calls `milliways.open_agent('claude')`
- **THEN** `AgentDomain::spawn_pane` SHALL execute the steps above
- **AND** within 500ms a new pane SHALL appear streaming claude's banner output

### Requirement: User input flows back via `agent.send`

Bytes written to the agent pane (user keystrokes, including Enter) SHALL be forwarded to the daemon via `agent.send({handle, bytes})`. Batching policy:

- The pane SHALL accumulate keystroke bytes for up to 16ms before flushing.
- The pane SHALL flush IMMEDIATELY (without waiting for the 16ms tick) on any of: a newline byte, a carriage-return byte, a control character (byte < 0x20 except TAB), or a buffer reaching 8KB.

This guarantees zero added latency on `Enter` while still batching held-key repeats.

#### Scenario: User presses Enter

- **WHEN** the user types a prompt and presses Enter in an agent pane
- **THEN** `AgentDomain` SHALL flush immediately on the newline byte
- **AND** the runner SHALL receive the bytes within 50ms (no 16ms tick wait)

#### Scenario: User holds a key

- **WHEN** the user holds a printable key for 500ms
- **THEN** the keystrokes SHALL be batched at most 16ms per send
- **AND** SHALL never exceed 8KB per send

### Requirement: Reconnect on daemon disconnect

If the UDS connection drops while a pane is alive, `AgentDomain` SHALL display a banner in the pane (via OSC + ANSI) and attempt to reconnect every 2s for up to 30s. On successful reconnect, the same `handle` SHALL be re-attached and the stream resumed.

#### Scenario: Daemon killed and restarted

- **WHEN** `milliwaysd` is killed while an agent pane is open
- **THEN** the pane SHALL display a red reconnect banner within 3s
- **AND** when the daemon comes back, the pane SHALL resume the stream within 5s without losing prior buffered output

#### Scenario: Reconnect deadline exceeded

- **WHEN** 30s pass without reconnect
- **THEN** the banner SHALL change to an error state with a "Press R to retry, X to close" prompt
- **AND** the pane SHALL await user input

### Requirement: Special agent ids are reserved

The agent id namespace SHALL include reserved ids prefixed with underscore. MVP reservations:

- `_observability` — opens the observability cockpit pane (see `observability-cockpit`)

User-facing agents (`claude`, `codex`, `minimax`, `copilot`) SHALL never start with underscore.

#### Scenario: Open observability pane via AgentDomain

- **WHEN** `Cmd+Shift+O` triggers `milliways.open_agent('_observability')`
- **THEN** `AgentDomain` SHALL open a pane backed by the daemon's observability stream
- **AND** the pane SHALL render the cockpit (see `observability-cockpit`)
