# Tasks — milliways-emulator-fork

## Delivery checklist

- [x] Stand up `milliwaysd` and `milliwaysctl` as the daemon/control-plane foundation.
- [x] Import the wezterm fork under `crates/milliways-term/` and add the milliways Rust subtree.
- [x] Make `milliways` with no arguments launch the daemon-backed `milliways-term` cockpit.
- [x] Preserve the built-in terminal as deprecated `milliways --repl` fallback with an explicit deprecation notice.
- [ ] Complete an end-to-end `milliways-term` agent-pane smoke across claude, codex, minimax, and copilot.
- [ ] Complete `/context` cockpit visual acceptance with screenshots against `specs/context-cockpit/spec.md`.
- [ ] Complete observability cockpit pane visual/functional acceptance.
- [ ] Complete daemon crash/reconnect manual smoke for an in-flight agent pane.
- [ ] Replace or archive stale active REPL/TUI OpenSpec changes once parity is signed off.

## Sign-off criteria

- `make all` builds both `milliways-term` (Rust) and `milliwaysd` + `milliwaysctl` (Go).
- `make repl` builds the legacy `milliways --repl` binary unchanged.
- `go test ./...` passes; `cargo test --workspace` passes.
- CI green on macOS + Linux for all three artefacts.
- Manual:
  - `milliways` (no flags) launches a window, status bar shows daemon status.
  - `Cmd+Shift+A` opens the agent picker; selecting `claude` opens an agent pane that streams responses.
  - Typing a prompt in the agent pane and pressing Enter streams text via `agent.stream`; status bar tokens/cost update during the dispatch.
  - `Cmd+Shift+C` opens the per-agent `/context` overlay; charts render via kitty graphics; numbers update live.
  - `Cmd+Shift+G` opens the aggregated `/context` overlay across all agents.
  - `Cmd+Shift+O` opens the observability cockpit pane; recent spans visible, sparkline updates at 1 Hz.
  - `Cmd+T` opens a regular shell tab (wezterm default), unchanged.
  - `milliways --repl` launches the legacy REPL identically to today.
  - Killing the daemon while a pane is open shows a reconnect banner; restarting the daemon resumes the pane.
- `openspec validate milliways-emulator-fork --strict` passes.
- Visual acceptance: `/context` overlay reviewed against `specs/context-cockpit/spec.md` visual criteria. Sign-off requires side-by-side screenshot vs Claude Code `/context`.

---

## Phase 0 — Repo restructure

### TASK-0.1: Add `crates/` directory and Cargo workspace

**Files**: `Cargo.toml` (new, workspace root), `crates/.gitkeep`, `Makefile` (extend)

- Create root `Cargo.toml` declaring an empty workspace `members = ["crates/*"]`.
- Add `Makefile` targets: `term`, `daemon`, `ctl`, `repl`, `all`.
- `make repl` builds today's `cmd/milliways/` to `~/.local/bin/milliways`.

**Verify**: `make repl` still works; `cargo metadata` reports the workspace.

---

### TASK-0.2: `proto/milliways.json` JSON Schema skeleton

**Files**: `proto/milliways.json`, `proto/README.md`, `scripts/gen-rpc-types.sh`

- Write the JSON Schema scaffold for top-level RPC types: `Status`, `AgentInfo`, `ContextSnapshot`, `Span`, `Metric`, `QuotaSnapshot`, `RoutingDecision`. Empty inner shapes; flesh out per phase.
- `scripts/gen-rpc-types.sh` runs `quicktype` (or equivalent) to emit `internal/rpc/types.go` (Go) and `crates/milliways-term/milliways/src/rpc/types.rs` (Rust).
- CI step that runs the generator and fails if `git diff --exit-code` reports drift.

**Verify**: `scripts/gen-rpc-types.sh` is idempotent; running it twice leaves no diff.

---

### TASK-0.3: Spike — kitty graphics protocol inside wezterm's overlay surface

**Files**: `docs/spikes/SPIKE-wezterm-overlay-kitty-graphics.md`

**Status**: BLOCKING. Phase 5 (`/context` cockpit) cannot start until this spike returns.

The question: does **wezterm's overlay rendering surface** (the surface used by `CommandPalette`, `LaunchMenu`, etc.) consume **kitty graphics protocol** escape sequences (`ESC _ G ... ESC \`) the same way regular panes do? Wezterm renders kitty graphics in panes; whether overlays share that render path is undocumented.

- Build a minimal wezterm fork patch (or external wezterm config + Lua hook) that opens an overlay containing a kitty-graphics escape sequence with a known PNG payload.
- Run on macOS and Linux. Capture screenshots.
- Three possible outcomes, each with a follow-on:
  1. **PASS** — wezterm overlays render kitty graphics. Decision 4 holds. Phase 5 proceeds as designed.
  2. **PARTIAL** — wezterm overlays render images but with caveats (no caching, redraw flicker). Document caveats; Phase 5 implementation accommodates.
  3. **FAIL** — wezterm overlays do not render kitty graphics at all. Decision 4 falls back: `/context` becomes a real pane (tab) under reserved id `_context`, opened via `Cmd+Shift+C` as a new tab. Visual contract preserved; surface changes from overlay to pane.

**Verify**: `docs/spikes/SPIKE-wezterm-overlay-kitty-graphics.md` exists with: outcome (PASS/PARTIAL/FAIL), screenshots, recommended Phase 5 surface (overlay vs pane), follow-on tasks if FAIL.

---

### TASK-0.4: Spike — `AgentDomain` over `cat` with virtual PTY

**Files**: `docs/spikes/SPIKE-agent-domain-virtual-pty.md`, throwaway code in `crates/milliways-term/milliways/src/spikes/agent_domain_cat.rs`

**Status**: BLOCKING. Phase 3 (`AgentDomain` MVP) cannot start until this spike returns.

- Implement a no-op `AgentDomain` whose "agent" is `cat` reading from a tokio task-fed virtual PTY (no daemon involvement).
- Open a pane backed by it. Exercise the full wezterm pane surface:
  - Resize (terminal window grow/shrink)
  - Copy mode (`Cmd+Shift+X` or wezterm default)
  - Search (`Cmd+F`)
  - Scrollback walk
  - Splits and tab moves
  - Focus events
  - Mouse selection
- Document any behaviours that diverge from a real-PTY shell pane.

**Verify**: `docs/spikes/SPIKE-agent-domain-virtual-pty.md` exists with: full feature matrix (works/partial/broken per feature), `wezterm_compat` shim sketch if any feature requires one, patch-budget impact estimate.

---

## Phase 1 — `milliwaysd` daemon

### TASK-1.1: Daemon main + UDS listener

**Files**: `cmd/milliwaysd/main.go`, `internal/daemon/server.go`, `internal/daemon/lock.go`

- `cmd/milliwaysd/main.go`: parse flags (`--socket`, `--state-dir`, `--log-level`), acquire `flock` on `${state}/pid`, start RPC server on UDS at `${state}/sock` with mode 0600.
- `internal/daemon/server.go`: JSON-RPC 2.0 server using `sourcegraph/jsonrpc2`. Method dispatcher, request logging.
- `internal/daemon/lock.go`: stale-lock takeover via `kill -0`.

**Verify**: `milliwaysd & milliwaysctl ping` returns `{"pong": true}`.

---

### TASK-1.2: `milliwaysctl` CLI

**Files**: `cmd/milliwaysctl/main.go`, `internal/rpc/client.go`

- `milliwaysctl ping`, `milliwaysctl status --json`, `milliwaysctl shutdown`, `milliwaysctl status --watch` (subscribes via NDJSON).
- One-shot calls reuse `internal/rpc/client.go`.

**Verify**: All four subcommands return the expected JSON or exit non-zero.

---

### TASK-1.3: Lift runner adapters into the daemon

**Files**: `internal/daemon/runners/{claude,codex,minimax,copilot}.go`, copied from `internal/repl/runner_*.go`

- Copy (don't move yet) — the legacy REPL keeps its own copies until it is removed in a follow-up change.
- Wire daemon `agent.list` → returns the four runner ids with their auth status.
- Smoke test: `milliwaysctl agents` lists `claude`, `codex`, `minimax`, `copilot`.

**Verify**: `go test ./internal/daemon/runners/...` passes.

---

### TASK-1.4: Lift sessions, MCP, MemPalace, sommelier, pantry

**Files**: `internal/daemon/sessions/`, `internal/daemon/mcp/`, `internal/daemon/mempalace/`, `internal/daemon/sommelier/`, `internal/daemon/pantry/`

- Copy from existing `internal/session/`, `internal/mcp/`, etc. into daemon-private packages.
- Daemon owns the singletons; methods exposed as RPC where needed (`status.get` aggregates).

**Verify**: `go test ./internal/daemon/...` passes.

---

### TASK-1.5: OTel SDK self-instrumentation + ring buffer

**Files**: `internal/daemon/observability/sdk.go`, `internal/daemon/observability/ring.go`

- Daemon installs OTel SDK with stdout exporter (default) and an in-memory `SpanRingBuffer` (size 1000, configurable).
- Daemon emits its own spans for every RPC call.
- `observability.spans({since})` returns recent spans from the ring.
- `observability.metrics({range})` returns time-bucketed counters/gauges.

**Verify**: `milliwaysctl spans --since 1m` lists daemon-internal spans.

---

## Phase 2 — Wezterm fork

### TASK-2.1: Subtree-import wezterm into `crates/milliways-term/`

**Files**: `crates/milliways-term/` (entire wezterm tree), `crates/milliways-term/PATCHES.md`

- `git subtree add --prefix crates/milliways-term <wezterm-repo> <pinned-tag>`.
- Pinned-tag: latest release at fork time (record in `PATCHES.md`).
- Confirm `cargo build -p wezterm-gui` produces a working binary.

**Verify**: `cargo build --release -p wezterm-gui` succeeds; running the resulting binary opens an unmodified wezterm window.

---

### TASK-2.2: Rename binary to `milliways-term`

**Files**: `crates/milliways-term/wezterm/Cargo.toml` (or `wezterm-gui/Cargo.toml`), patch noted in `PATCHES.md`

- Bin name patched from `wezterm-gui` (or `wezterm`) to `milliways-term`.
- `Makefile`: `make term` runs `cargo build --release -p wezterm-gui` (or whichever subcrate) and copies the binary to `~/.local/bin/milliways-term`.

**Verify**: `~/.local/bin/milliways-term` exists, opens a window, behaves as wezterm.

---

### TASK-2.3: `crates/milliways-term/milliways/` subtree skeleton

**Files**: `crates/milliways-term/milliways/Cargo.toml`, `src/lib.rs`, `src/rpc/`, `src/agent_domain/`, `src/context_overlay/`, `src/status_helpers/`

- Add as a workspace member of the wezterm Cargo workspace (small patch to root `Cargo.toml`, recorded in `PATCHES.md`).
- Empty modules with `pub fn init() {}` placeholders.

**Verify**: `cargo build -p milliways` (the new crate) succeeds.

---

### TASK-2.4: Wire `milliways::init()` from wezterm-gui startup

**Files**: small patch in `crates/milliways-term/wezterm-gui/src/main.rs` (recorded in `PATCHES.md`)

- One call site only: after wezterm's main initialisation, invoke `milliways::init(&config)` to register the AgentDomain factory and any Lua helpers.

**Verify**: Start `milliways-term`, see a log line "milliways: initialised" in stderr.

---

## Phase 3 — `AgentDomain` MVP (claude only)

### TASK-3.1: `agent.open`, `agent.send`, `agent.stream` on the daemon

**Files**: `internal/daemon/agent/agent.go`, `internal/daemon/agent/stream.go`

- `agent.open({agent_id, session_id?})` returns `{handle, pty_size}`.
- `agent.send({handle, bytes})` forwards user input to the runner.
- `agent.stream({handle})` returns `{stream_id}`; daemon then opens an NDJSON push connection for output bytes.
- For MVP: only `agent_id == "claude"` is implemented. Other ids return `method not implemented`.

**Verify**: Two terminals, one runs `milliwaysd`, the other does `milliwaysctl agent open --id claude` then `milliwaysctl agent send --handle <h> --text "hello"` and `milliwaysctl agent stream --handle <h>` shows streamed bytes.

---

### TASK-3.2: `AgentDomain` Rust implementation

**Files**: `crates/milliways-term/milliways/src/agent_domain/mod.rs`, `crates/milliways-term/milliways/src/wezterm_compat/`

- Implement wezterm's `Domain` trait via the `wezterm_compat` shim (TASK-0.4 outputs the shim sketch).
- `spawn_pane` opens an `agent.open` over the daemon UDS, returns a pane backed by a virtual PTY.
- Read loop: NDJSON from daemon → bytes into the PTY master, with newline-flush input batching from `agent-domain/spec.md`.
- Write loop: PTY slave (user input) → `agent.send`.
- Happy-path only — reconnect logic and disconnect banner are out of scope here (see TASK-3.4).

**Verify**: Manually invoke `milliways-term -e milliways:open_agent claude` (Lua function exposed via TASK-3.3); a new pane streams Claude responses while the daemon stays up.

---

### TASK-3.3: Lua API + keybinding for agent picker

**Files**: `crates/milliways-term/milliways/src/lua_api.rs`, sample `milliways.lua`

- Expose `milliways.open_agent(id)` from Rust to Lua via wezterm's existing Lua hook surface.
- Sample config in `milliways.lua` binds `Cmd+Shift+A` to a wezterm `InputSelector` listing daemon-reported agents (one-shot `agent.list`).

**Verify**: Press `Cmd+Shift+A`, see picker, select `claude`, new agent pane opens.

---

### TASK-3.4: Pane-side reconnect state machine + banner

**Files**: `crates/milliways-term/milliways/src/agent_domain/reconnect.rs`

- State machine per pane: `Connected → Disconnected → Reconnecting(attempt_n) → Connected | GaveUp(reason)`.
- On UDS disconnect: render an in-pane banner (OSC + ANSI) with countdown, attempt reconnect every 2s for up to 30s. Use the `last_seen_offset` replay protocol from `term-daemon-rpc/spec.md` so resume is exact.
- On `GaveUp`: change banner to red error state with "Press R to retry, X to close" prompt, await user input.
- Manual retry binding: `R` while focused on a `GaveUp` pane re-runs the reconnect attempt; `X` closes the pane.
- Daemon-side cooperation lives in TASK-3.1 already (output ring + reservation timeout); this task is purely the pane's view of the state machine.

**Verify**: `kill milliwaysd`; pane shows banner with countdown. Restart daemon within 30s → banner clears, output resumes from last byte. Restart after 30s → red error state appears, `R` recovers, `X` closes.

---

## Phase 4 — Status bar

### TASK-4.1: `status.get` + `status.subscribe`

**Files**: `internal/daemon/status/status.go`

- `status.get` returns `Status` (active agent, session turn count, tokens in/out, cost, quota, error count).
- `status.subscribe` opens NDJSON stream that emits a `Status` diff each time anything changes (debounced to 4 Hz).

**Verify**: `milliwaysctl status --json` returns shape; `milliwaysctl status --watch` streams updates during a dispatch.

---

### TASK-4.2: `update-right-status` Lua hook

**Files**: `milliways.lua` (status bar section), helper in `crates/milliways-term/milliways/src/status_helpers/`

- Lua hook calls `milliwaysctl status --json` (one-shot) and renders a styled string.
- For sub-second updates during a dispatch: `milliwaysctl status --watch` writes latest line to `${state}/status.cur`; Lua tails it via `io.open` (cheap).
- Status format: `{agent} | turn:{n} | {in}↑/{out}↓ tok | ${cost} | quota: {pct}% | err:{n}`.

**Verify**: Open agent pane, dispatch a prompt; status bar tokens/cost update visibly during the dispatch.

---

## Phase 5 — `/context` cockpit

### TASK-5.1: `context.get`, `context.get_all`, `context.subscribe`

**Files**: `internal/daemon/context/context.go`

- `context.get({agent_id})` returns full `ContextSnapshot`: model, system-prompt summary, turns, tokens (in/out/cached), tools, MCP servers, files-in-context, session id, cost, last span, error count.
- `context.get_all()` returns `{agents: [ContextSnapshot...], aggregate: AggregateStats}`.
- `context.subscribe({agent_id?})` streams diffs.

**Verify**: `milliwaysctl context --agent claude --json` prints the snapshot.

---

### TASK-5.2a: Daemon `context.chart_data` RPC (data only)

**Files**: `internal/daemon/charts/data.go`

- Daemon exposes `context.chart_data({agent_id?, kind})` returning a structured payload — no presentation:
  - `donut` → `{kind:"donut", segments:[{label, value, hint}], total, data_hash}`
  - `sparkline` → `{kind:"sparkline", points:[number], range:{min,max}, hint, data_hash}`
  - `bars` → `{kind:"bars", series:[{label, values, hint}], x_labels, data_hash}`
  - `line` → `{kind:"line", series:[{label, points:[{x,y}], hint}], data_hash}`
- `hint` is a semantic name (`"ok"`, `"warn"`, `"err"`, `"accent"`, `"dim"`), not a colour. Theme resolution happens Rust-side.
- `data_hash` is a stable hash of the payload so the Rust renderer can skip redraws when unchanged.
- No PNG generation, no kitty-graphics escapes, no `image_id` allocation in Go.

**Verify**: `milliwaysctl context-chart-data --agent claude --kind tokens --json` prints structured data with a `data_hash`.

---

### TASK-5.2b: Rust chart renderer

**Files**: `crates/milliways-term/milliways/src/charts/{mod.rs,donut.rs,sparkline.rs,bars.rs,line.rs}`, `Cargo.toml` adds `plotters` dep

- Pull in `plotters` (or equivalent) as a workspace dep.
- One module per chart kind. Each takes `(ChartData, &Theme) → Vec<u8>` (PNG bytes).
- Renderer respects wezterm's font stack via `plotters`' font config; reads colour values from `Theme` resolved from `milliways.lua`.
- Wrap PNG in kitty-graphics escape with a stable image-id derived from `(kind, agent_id, data_hash)` so wezterm's image cache stays warm.
- Skip-rerender path: if `data_hash` unchanged from previous frame, emit only an image-id reference (no payload).

**Verify**: `cargo test -p milliways charts::*` passes a snapshot test per chart kind that asserts the rendered PNG is non-empty and contains the expected pixels (compared against a checked-in golden).

---

### TASK-5.3: `ContextOverlay` Rust implementation

**Files**: `crates/milliways-term/milliways/src/context_overlay/mod.rs`, `crates/milliways-term/milliways/src/context_overlay/layout.rs`

- Implement a wezterm overlay (or pane, per TASK-0.3 outcome) that fetches `context.get` (or `_all`) on open and subscribes for updates.
- Layout (per-agent): header row, donut+sparkline row (charts produced via TASK-5.2b), tools/MCP/files row, footer (cost, errors).
- Layout (aggregated): grid of mini-cards, one per agent, plus a totals header.
- Semantic palette in `milliways.lua` (`milliways.theme = { ok=..., warn=..., err=..., accent=..., dim=... }`); overlay reads palette via Lua API and passes it to the chart renderer as `Theme`.
- Pull chart data via `context.chart_data` per chart and per `data_hash`; render via TASK-5.2b.

**Verify**: `Cmd+Shift+C` over an agent pane opens the per-agent overlay; `Cmd+Shift+G` opens the aggregated view; charts are visible.

---

### TASK-5.4: Live updates during dispatch

**Files**: extend `crates/milliways-term/milliways/src/context_overlay/mod.rs`

- Overlay holds an active subscription handle. On `context.subscribe` push, mutates only the changed fields and triggers a redraw.
- Charts: only re-emit when underlying data changes (compare hash); wezterm's image cache absorbs the rest.

**Verify**: Open the overlay, then dispatch a prompt; tokens-in counter ticks up live; sparkline grows.

---

### TASK-5.5: Visual acceptance pass

**Files**: `specs/context-cockpit/spec.md` (already drafted), screenshots in `docs/screenshots/`

- Capture screenshots of `/context` per agent and aggregated.
- Side-by-side comparison with Claude Code `/context`.
- Address any gaps: typography weight, gradient fill, semantic colour, info density.

**Verify**: Reviewer signs off against the visual criteria in the spec.

---

## Phase 5.5 — Metrics roll-up

(Renumbered from Phase 6.5 — rollup is a prerequisite for the observability cockpit's sparklines being meaningful beyond the live tier, so it must land before Phase 6.)



### TASK-6.1: `observability.spans` + `observability.metrics`

**Files**: `internal/daemon/observability/rpc.go`

- `observability.spans({since, limit})` returns recent spans from the ring.
- `observability.metrics({range, bucket})` returns bucketed time-series.
- `observability.subscribe()` streams new spans as they land.

**Verify**: `milliwaysctl spans --since 1m` lists; `milliwaysctl metrics --range 5m --bucket 10s` returns buckets.

---

### TASK-6.2: Observability pane (Rust-rendered)

**Files**: `crates/milliways-term/milliways/src/observability_pane/{mod.rs,layout.rs,renderer.rs}`

- `AgentDomain::spawn_pane("_observability")` returns a pane backed by a Rust-internal renderer (NOT a virtual PTY fed by daemon bytes).
- Renderer subscribes to `observability.subscribe` (spans), `status.subscribe` (live counters), and polls `metrics.rollup.get` per region per repaint.
- Layout regions:
  - Span tail (top half) — pure ANSI rendering, severity-coloured per `theme`.
  - Throughput sparkline (top right) — chart renderer (TASK-5.2b) on `tokens_in+tokens_out` per second.
  - Latency p50/p95/p99 bars (middle) — chart renderer with bars kind.
  - Cost-per-hour line chart (bottom left) — chart renderer with line kind.
  - Error-rate badge + sparkline (bottom right) — text + chart renderer.
- Frame budget per `observability-cockpit/spec.md`: ≤ 1 frame/sec, ≤ 32KB/frame, image-id-only references for unchanged charts.

**Verify**: `Cmd+Shift+O` opens a pane that updates at 1 Hz with span tail and sparklines. `cargo test -p milliways observability_pane::*` covers layout snapshot tests.

---

### TASK-5.5.1: SQLite schema + migrations

**Files**: `internal/daemon/metrics/schema.go`, `internal/daemon/metrics/migrate.go`, `internal/daemon/metrics/store.go`

- Schema (one set of tables per tier, plus a `schema_version` table):
  - `samples_raw(metric TEXT, agent_id TEXT, ts INTEGER, value REAL, count INTEGER, sum REAL, min REAL, max REAL, p50 REAL, p95 REAL, p99 REAL)`
  - `samples_hourly`, `samples_daily`, `samples_weekly`, `samples_monthly` — same shape.
  - Indexes on `(metric, agent_id, ts)` per table.
- `metrics.db` opens at `${state}/metrics.db` with `PRAGMA journal_mode=WAL`.
- Forward-only migrations runner. Refuses to start if a migration fails.

**Verify**: `go test ./internal/daemon/metrics/...` covers schema creation, migration application, and idempotent re-run.

---

### TASK-5.5.2: Metric kind registry + observation API

**Files**: `internal/daemon/metrics/registry.go`, `internal/daemon/metrics/observe.go`

- `Register(name string, kind Kind)` where `Kind ∈ {Counter, Histogram, Gauge}`.
- `ObserveCounter(name, agent, delta)`, `ObserveHistogram(name, agent, value)`, `ObserveGauge(name, agent, value)`.
- Internal write buffer flushes to the `samples_raw` table once per second.
- Daemon registers MVP metrics on start: `tokens_in`, `tokens_out`, `cost_usd`, `dispatch_count`, `error_count` (counters); `dispatch_latency_ms` (histogram); `active_agents`, `mcp_servers_connected` (gauges).

**Verify**: Unit tests dispatch a fake observation and assert it lands in `samples_raw` within 2 ticks.

---

### TASK-5.5.3: Rollup scheduler

**Files**: `internal/daemon/metrics/rollup.go`

- 60s tick loop in the daemon. On each tick, in **a single SQLite write transaction** spanning all five steps:
  1. Demote all `samples_raw` rows with `ts < now-60min` into `samples_hourly` using the kind-specific aggregation.
  2. Demote `samples_hourly` rows with `ts < now-24h` into `samples_daily`.
  3. Demote `samples_daily` rows with `ts < now-7d` into `samples_weekly`.
  4. Demote `samples_weekly` rows with `ts < now-28d` into `samples_monthly`.
  5. Delete `samples_monthly` rows with `ts < now-12months` (calendar months in the configured timezone, default local).
- Single-transaction guarantee means cockpit reads never see a row in two tiers simultaneously, nor in neither tier mid-cascade.
- WAL mode (`PRAGMA journal_mode=WAL`) ensures concurrent reads do not block on the rollup write transaction.
- Resilient to skipped ticks: each pass processes *all* eligible rows, not just last-minute work.

**Verify**: Test with a fake clock that advances 2h, 25h, 8d, 30d, 13 months and asserts the right cascade happens at each boundary. Concurrency test: a tight loop of `metrics.rollup.get` calls during a fake-clock-driven cascade asserts no row appears in two tiers and no row disappears.

---

### TASK-5.5.4: Aggregation rules

**Files**: `internal/daemon/metrics/aggregate.go`

- `AggregateCounter(rows) → Bucket`: sum.
- `AggregateHistogram(rows) → Bucket`: exact `count`/`sum`/`min`/`max`; weighted-average `p50`/`p95`/`p99` flagged approximate.
- `AggregateGauge(rows) → Bucket`: count-weighted mean.

**Verify**: Unit tests assert each rule against hand-computed expected values.

---

### TASK-5.5.5: `metrics.rollup.get` RPC

**Files**: `internal/daemon/metrics/rpc.go`

- JSON-RPC method per `metrics-rollup/spec.md`. Accepts `metric`, `tier`, optional `range`, optional `agent_id`.
- Returns buckets ordered by `ts` ascending. Includes `approximate: true` for histogram percentiles above `raw`.

**Verify**: `milliwaysctl metrics --metric tokens_in --tier hourly --range 24h` prints buckets.

---

### TASK-5.5.6: Wire MVP metrics into the daemon

**Files**: `internal/daemon/agent/agent.go`, `internal/daemon/observability/sdk.go`, `internal/daemon/pantry/`

- Each runner dispatch increments `tokens_in`/`tokens_out`/`cost_usd`/`dispatch_count` and records `dispatch_latency_ms`.
- Each error increments `error_count` with the `agent_id` label.
- Daemon ticker updates `active_agents` and `mcp_servers_connected` once per minute.

**Verify**: After dispatching a few prompts, `milliwaysctl metrics --metric dispatch_count --tier raw --range 5m` shows a non-empty series.

---

## Phase 7 — Other agents

### TASK-7.1: Add codex, minimax, copilot to `agent.open`

**Files**: `internal/daemon/agent/agent.go`, `internal/daemon/runners/`

- Wire each runner's stream into the same `agent.open`/`agent.stream` interface.

**Verify**: `Cmd+Shift+A` lists all four; each opens a working pane.

---

### TASK-7.2: Per-agent `/context` shape parity

**Files**: `internal/daemon/context/context.go`, runner-specific adapters

- Each runner contributes its `ContextSnapshot`. Codex includes profile/sandbox; MiniMax includes API-key status; Copilot includes auth scope.

**Verify**: `/context` overlay renders correctly for each agent without missing fields.

---

## Phase 8 — Polish + parity

### TASK-8.1: Daemon crash reconnect UX

**Files**: `crates/milliways-term/milliways/src/agent_domain/mod.rs` (extend)

- Red banner with countdown.
- Auto-retry every 2s for 30s.
- After 30s: prompt user to manually reconnect or close pane.

**Verify**: `kill milliwaysd`; pane shows banner; restart daemon; banner clears, stream resumes.

---

### TASK-8.2: Default launcher logic

**Files**: `cmd/milliways/main.go` (the legacy binary's main)

- Default mode (no `--repl`): if daemon not running, start it (detached). Then `exec milliways-term`.
- `--repl`: existing behaviour unchanged.

**Verify**: Fresh shell, run `milliways`; daemon starts, terminal window opens.

---

### TASK-8.3: Documentation

**Files**: `README.md` (rewrite), `CONTRIBUTING.md` (new section), `crates/milliways-term/PATCHES.md`

- README: new architecture diagram (term + daemon + ctl + repl), keybinding reference, screenshots.
- CONTRIBUTING: how to build each layer, how to regenerate RPC types, how to add a new RPC method.
- PATCHES: enumerate every patched upstream file with a one-line rationale.

**Verify**: A new contributor can `make all` and run the cockpit using only README + CONTRIBUTING.

---

### TASK-8.4: CI matrix

**Files**: `.github/workflows/ci.yml`

- Jobs: `rust-build` (mac+linux), `go-build` (mac+linux), `integration-smoke-linux` (full — start daemon, run `milliwaysctl ping`, dispatch a stub prompt, exercise `agent.list`/`status.get`/`metrics.rollup.get`), `integration-smoke-macos` (headless subset — daemon up, `milliwaysctl ping`, `milliwaysctl status --json`, `milliwaysctl agent list`; skip GUI assertions).
- `make repl` job that builds and tests the legacy path.
- Codegen-drift check: `scripts/gen-rpc-types.sh && git diff --exit-code`.
- Upstream-stale check: warn (not fail) if the pinned wezterm tag is more than 60 days behind upstream. Fail (not warn) if any wezterm `Domain` trait method signature recorded in `PATCHES.md` differs between the pinned tag and the latest tag.

**Verify**: All CI jobs green on a clean PR.

---

### TASK-8.5: OpenSpec validation

**Files**: this directory

- `openspec validate milliways-emulator-fork --strict` passes.
- `openspec diff milliways-emulator-fork` reviewed by a human before merge.

**Verify**: CLI exits 0.
