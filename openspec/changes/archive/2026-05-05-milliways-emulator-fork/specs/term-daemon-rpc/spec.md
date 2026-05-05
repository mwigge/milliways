## ADDED Requirements

### Requirement: JSON-RPC 2.0 over UDS, newline-delimited

The terminal-to-daemon protocol SHALL be JSON-RPC 2.0 over the Unix domain socket, **newline-delimited (NDJSON)**, never `Content-Length`-framed. One framing across the whole protocol — unary and streams alike. JSON values SHALL be transmitted in compact form (no embedded literal newlines). Single source of schema truth: `proto/milliways.json`.

#### Scenario: Method call returns typed result

- **WHEN** the terminal calls `status.get` with no params
- **THEN** the daemon SHALL respond with a JSON-RPC 2.0 result conforming to the `Status` schema in `proto/milliways.json`
- **AND** the response time SHALL be under 50ms in the steady-state cache hit path

#### Scenario: Schema drift detected

- **WHEN** `scripts/gen-rpc-types.sh` is run
- **THEN** generated `internal/rpc/types.go` and `crates/milliways-term/milliways/src/rpc/types.rs` SHALL match the JSON Schema
- **AND** CI SHALL fail any PR that introduces drift

### Requirement: Server-pushed streams via sidecar connection

JSON-RPC 2.0 has no first-class server-push. For long-lived streams (`agent.stream`, `context.subscribe`, `status.subscribe`, `observability.subscribe`), the protocol SHALL use a two-step convention: a unary RPC returns a `stream_id` and a `replay_token`; the client opens a second connection on the same UDS path with a one-line preamble `STREAM <stream_id> <last_seen_offset>\n`, after which the daemon writes NDJSON-framed events until either side closes.

#### Scenario: Open agent stream

- **WHEN** the terminal calls `agent.stream({handle})`
- **THEN** the daemon SHALL respond with `{stream_id, output_offset: 0}` immediately
- **AND** the terminal SHALL open a second UDS connection and write `STREAM <stream_id> 0\n` as the first line
- **AND** the daemon SHALL push one NDJSON object per output chunk: `{"t":"data","b64":"...","offset":<n>}` for bytes, `{"t":"end"}` for stream close, or `{"t":"err","code":...,"msg":...}` for errors

#### Scenario: Stream closes on terminal disconnect

- **WHEN** the terminal closes the stream connection
- **THEN** the daemon SHALL detect the disconnect within 1s
- **AND** it SHALL retain the underlying agent state and output ring so the same stream_id can be re-attached

### Requirement: Stream-id reservation timeout

Every `*.stream` or `*.subscribe` unary call SHALL allocate a `stream_id` reservation. If the sidecar connection is not established within 5 seconds, the reservation SHALL expire and the stream_id SHALL become invalid.

#### Scenario: Sidecar never opens

- **WHEN** a client calls `agent.stream` and never opens the sidecar within 5s
- **THEN** the daemon SHALL release the reservation
- **AND** any subsequent `STREAM <stream_id> ...\n` attempt SHALL receive a single NDJSON line `{"t":"err","code":-32003,"msg":"stream_attach_timeout"}` and be closed

### Requirement: Output ring + offset replay

For every active stream, the daemon SHALL maintain a per-handle output ring buffer (default size 256KB, configurable via daemon config). The buffer SHALL retain bytes the client has not yet acknowledged via reconnect. On sidecar attach, the daemon SHALL replay any buffered output from `last_seen_offset` forward before resuming live emission.

#### Scenario: Client reconnects after dropping mid-stream

- **WHEN** a sidecar connection drops while bytes are in flight
- **AND** the client opens a new sidecar with `STREAM <stream_id> <last_offset>\n` within 30 seconds
- **THEN** the daemon SHALL push every buffered byte from `last_offset` onward in order
- **AND** SHALL transition seamlessly to live output without duplication

#### Scenario: Reconnect window exceeded

- **WHEN** a sidecar drop is not followed by a reconnect within 30 seconds
- **THEN** the daemon SHALL discard the output ring
- **AND** SHALL keep the underlying agent handle alive (so a fresh `agent.stream({handle})` call still works) but SHALL respond `{"t":"err","code":-32007,"msg":"replay_window_expired"}` if the old `stream_id` is reused

#### Scenario: Buffer overflow during disconnect

- **WHEN** the runner produces more than 256KB while no sidecar is attached
- **THEN** the daemon SHALL discard the *oldest* bytes in the ring
- **AND** on reattach SHALL push a `{"t":"warn","code":-32008,"msg":"replay_truncated","dropped_bytes":<n>}` line before resuming live output

### Requirement: Method catalogue (MVP)

The daemon SHALL implement at minimum the following methods:

- Lifecycle: `ping`, `shutdown`, `reload`
- Status: `status.get`, `status.subscribe`
- Agents: `agent.list`, `agent.open`, `agent.send`, `agent.stream`, `agent.close`
- Context: `context.get`, `context.get_all`, `context.subscribe`, `context.chart_data`
- Observability: `observability.spans`, `observability.metrics`, `observability.subscribe`
- Quota: `quota.get`
- Routing: `routing.peek`

#### Scenario: Unknown method returns standard error

- **WHEN** the terminal calls a method not in the catalogue
- **THEN** the daemon SHALL respond with JSON-RPC error code `-32601` (Method not found)
- **AND** SHALL log the unknown method at debug level

### Requirement: Versioning, with handshake-first ordering

The protocol SHALL include a major.minor version exposed via `ping`. Terminal and daemon SHALL refuse to communicate if their major versions disagree. The terminal SHALL call `ping` as its FIRST RPC after connecting, before any agent or stream calls.

#### Scenario: Version mismatch on connect

- **WHEN** the terminal's `proto.major` differs from the daemon's
- **THEN** the terminal SHALL render an error banner instructing the user to reinstall
- **AND** SHALL NOT call any further methods

#### Scenario: Stream call before handshake

- **WHEN** the terminal calls any non-`ping` method before completing the version handshake on a fresh connection
- **THEN** the daemon SHALL respond with error `-32002 version_handshake_required`
- **AND** the connection SHALL remain open (the terminal can complete the handshake and retry)

### Requirement: Standard error code table

The daemon SHALL use the following error codes consistently:

| Code   | Name                          | When                                                  |
|--------|-------------------------------|-------------------------------------------------------|
| -32601 | method_not_found              | unknown method                                        |
| -32602 | invalid_params                | malformed parameters                                  |
| -32001 | stream_not_found              | unknown `stream_id` on sidecar attach                 |
| -32002 | version_handshake_required    | non-ping called before handshake                      |
| -32003 | stream_attach_timeout         | sidecar did not attach within 5s                      |
| -32004 | method_disabled               | method exists but disabled by config                  |
| -32005 | quota_exceeded                | pantry quota exhausted for the requested agent        |
| -32006 | agent_not_implemented         | reserved or unimplemented `agent_id`                  |
| -32007 | replay_window_expired         | stream_id reused after replay buffer was discarded    |
| -32008 | replay_truncated              | non-fatal warning: bytes dropped during disconnect    |

#### Scenario: Caller can branch on error code

- **WHEN** the terminal receives any error response
- **THEN** the `code` field SHALL be one of the codes above (or a JSON-RPC standard code)
- **AND** the `message` field SHALL be a human-readable string suitable for the reconnect banner

### Requirement: Authentication via UDS file mode

There SHALL be no token, no TLS, no password. Auth is the file mode (0600) of the socket. The daemon SHALL refuse to start if the socket parent directory is world-writable.

#### Scenario: Insecure parent directory

- **WHEN** `${state}/` has mode other than 0700 (or the immediate parent is world-writable)
- **THEN** the daemon SHALL exit non-zero before binding the socket
- **AND** SHALL print a remediation message
