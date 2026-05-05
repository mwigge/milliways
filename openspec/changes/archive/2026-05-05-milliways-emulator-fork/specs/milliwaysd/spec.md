## ADDED Requirements

### Requirement: `milliwaysd` is a long-running Go daemon

`milliwaysd` SHALL be a single long-running process that owns shared state across all agent panes: runners, sessions, MCP, MemPalace, sommelier, pantry, and the OTel SDK. The daemon SHALL expose its surface only via the JSON-RPC 2.0 protocol on a Unix domain socket.

#### Scenario: Daemon start

- **WHEN** `milliwaysd` is invoked with no flags
- **THEN** it SHALL acquire an exclusive `flock` on `${state}/pid`
- **AND** it SHALL listen on a Unix domain socket at `${XDG_RUNTIME_DIR:-$HOME/.local/state/milliways}/sock` with mode 0600
- **AND** it SHALL initialise the OTel SDK with the configured exporter
- **AND** it SHALL log structured JSON to stderr at the configured log level

#### Scenario: Daemon already running

- **WHEN** `milliwaysd` is invoked while another instance holds the `flock`
- **THEN** it SHALL exit non-zero with a clear error message naming the existing PID
- **AND** it SHALL NOT modify the socket or pid file

#### Scenario: Stale lock takeover

- **WHEN** the pid file exists but `kill -0 <pid>` fails (process gone)
- **THEN** the daemon SHALL detect the stale lock, remove the pid file, acquire its own lock, and proceed
- **AND** it SHALL log a warning about the stale lock takeover

### Requirement: Single-instance, single-user

The daemon SHALL serve exactly one user; the socket file mode SHALL be 0600 owned by the invoking user. There SHALL be no network surface.

#### Scenario: Wrong user attempts to connect

- **WHEN** another user on the same host attempts to `connect()` to the socket
- **THEN** the OS SHALL refuse the connection because of file permissions
- **AND** the daemon SHALL NOT need additional auth

### Requirement: Hosts existing milliways subsystems

The daemon SHALL host (without semantic change) the runner adapters, session store, MCP server registry, MemPalace integration, sommelier routing logic, and pantry quota enforcement that exist today in `internal/repl/runner_*.go`, `internal/session/`, `internal/mcp/`, `internal/mempalace/`, `internal/sommelier/`, `internal/pantry/`.

#### Scenario: Runner state shared across panes

- **WHEN** two panes are open against the same `agent_id` (e.g., two claude panes)
- **THEN** they SHALL share session and quota state through the daemon
- **AND** a quota deduction in one pane SHALL be visible in the other

### Requirement: Lifecycle commands

The daemon SHALL expose lifecycle and health endpoints over RPC:

- `ping()` returns `{pong: true, version, uptime_s}`
- `shutdown({deadline_s?})` initiates graceful shutdown
- `reload()` re-reads config without restart

#### Scenario: Graceful shutdown drains pane streams

- **WHEN** `shutdown` is called
- **THEN** the daemon SHALL stop accepting new connections immediately
- **AND** it SHALL allow active streams to finish for up to `deadline_s` seconds (default 5)
- **AND** it SHALL release the lock and exit zero

### Requirement: OTel self-instrumentation

Every RPC method invocation SHALL emit an OTel span with method name, request id, latency, and outcome. Daemon-internal subsystems (runners, MCP, MemPalace) SHALL also emit spans. All spans SHALL be available via `observability.spans` (see `observability-cockpit`).

#### Scenario: Span captured for `agent.send`

- **WHEN** a pane calls `agent.send`
- **THEN** the daemon SHALL emit a span named `agent.send` with attributes `agent_id`, `bytes`, `outcome`
- **AND** the span SHALL appear in the next `observability.spans` poll
