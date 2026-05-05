## Why

Milliways' differentiator is not "we wrote a terminal." It is a coherent **agentic cockpit** — first-class panes for claude, codex, minimax, copilot; MCP and MemPalace plumbing; sommelier routing; pantry quotas; rich session and observability state. Today, all of that lives behind a REPL inside someone else's terminal (kitty, iTerm, Alacritty), so we can't surface it as the user sees it: status, context, traces, cost, routing decisions.

We considered building our own terminal in Go (cgo + GLFW + OpenGL) and rejected it: pure-Go-with-cgo loses to Alacritty/wezterm on latency and frame consistency due to GC pauses, **and** would force us to reinvent the VT parser, GPU renderer, tab/window manager, kitty graphics protocol, OSC 8, sixel, mouse, clipboard, ligatures — months of work to reach a baseline that already exists.

The pragmatic move is to **fork wezterm** (Rust, mature, GPU, kitty-protocol-compatible, scriptable in Lua), keep our existing Go code as a long-running daemon (`milliwaysd`), and spend our entire engineering budget on the cockpit surface: agent panes, the `/context` overlay (per-agent + aggregated), the status bar, the observability showcase. Wezterm gives us terminal perf for free; we ship the experience that no terminal has ever shipped.

## What Changes

This proposal covers MVP scope only — the smallest credible cockpit that hosts agent panes alongside shell panes in a forked wezterm, with first-class `/context` and observability views.

**MVP (this change):**

- New repository layout: monorepo with a Rust `crates/milliways-term/` (the wezterm fork) plus existing Go code under `cmd/milliwaysd/` (daemon), `cmd/milliwaysctl/` (CLI client), and `internal/` (reused runners and platform code).
- New Go daemon `milliwaysd`: long-running process that owns sessions, MCP, MemPalace, OTel SDK, sommelier, pantry, and runners. JSON-RPC 2.0 over a Unix domain socket at `~/.local/state/milliways/sock`.
- New protocol `term-daemon-rpc`: methods for `agent.open / agent.send / agent.stream`, `context.get / context.subscribe`, `status.get / status.subscribe`, `observability.spans / observability.metrics`, `quota.get`, `routing.peek`. Server-pushed streams via NDJSON over a long-lived connection.
- Wezterm fork (`milliways-term`): minimal patches to upstream core; all additions live in a `milliways/` subtree. New `AgentDomain` exposes agent panes that consume daemon RPC and emit a VT-byte stream that wezterm renders the same as a shell pane.
- `/context` cockpit overlay: per-agent view (model, system prompt summary, turns, tokens in/out/cached, tools, files in context, MCP servers, session id, cost, last span, error count) and an aggregated view (totals across all agents, routing decisions, pantry quota, active agents). Rendered in the focused tab as a wezterm overlay using rich text + kitty graphics protocol for charts and sparklines. **Chart rendering is Rust-side**: the daemon emits structured chart data (kind, buckets, semantic colour hints); the Rust overlay assembles PNGs via the `plotters` crate against wezterm's font stack and the `milliways.theme` palette, then encodes them as kitty-graphics escapes. Clean split: daemon owns data, Rust owns presentation.
- Observability cockpit pane: live tail of OTel spans, sparkline of token throughput, latency p50/p95, cost per hour, error rate. Opened with a keybinding.
- Metrics roll-up subsystem: five-tier retention in SQLite (1Hz raw for 60min, hourly buckets for 24h, daily buckets for 7d, weekly buckets for ~4 weeks, monthly buckets for ~12 months). Rollup scheduler in the daemon demotes samples between tiers every minute. Powers future "compare to last hour / yesterday / last week / same month last year" overlays — the comparison UI itself ships in a follow-up change, but the data must be collected from MVP because it cannot be backfilled.
- Wezterm Lua status bar: pulls live state from the daemon via `milliwaysctl status --json`, shows active agent, session turn count, tokens in/out, cost, quota remaining, OTel error count.
- Keybindings: `Cmd+Shift+A` open agent picker, `Cmd+Shift+C` toggle per-agent `/context`, `Cmd+Shift+G` aggregated `/context`, `Cmd+Shift+O` observability pane, `Cmd+T` new shell tab (unchanged from wezterm), `Cmd+1..9` tab nav (unchanged).
- REPL fallback: existing `milliways` binary preserved as `milliways --repl` until the cockpit reaches parity for daily use.

**P2 (explicitly out of scope, follow-up changes):**

- Splits inside a tab beyond what wezterm already supports (we inherit it; we don't extend it).
- Per-pane authentication flows beyond what runners already do.
- Custom kitty.conf-style config beyond what wezterm's existing config supports.
- `milliwaysd` clustering / multi-host (single-user, single-host this round).
- Windows support (wezterm runs on Windows; the daemon does not target it).
- Migration of every existing OpenSpec capability under `milliways-tui-*` into the new shape — the legacy `--repl` path keeps them.
- Replacement of the legacy REPL (separate change, after parity).

## Capabilities

### New Capabilities

- `wezterm-fork`: Repo + build + upstream-sync policy. Defines where the Rust fork lives, what is patched, how upstream merges work, and how the binary is named and shipped.
- `milliwaysd`: Long-running Go daemon that hosts runners, sessions, MCP, MemPalace, OTel SDK, sommelier, pantry. Lifecycle (start, stop, reload), socket path, single-instance lock, structured logging, OTel self-instrumentation.
- `term-daemon-rpc`: JSON-RPC 2.0 protocol between the Rust terminal and the Go daemon. Method catalogue, server-push stream encoding, error codes, versioning, auth (UDS file-mode-only, no token).
- `agent-domain`: New wezterm `Domain` implementation that vends agent panes. Each pane is a virtual PTY whose master end is fed bytes pulled from the daemon's `agent.stream`. User input on the pane is sent back via `agent.send`.
- `context-cockpit`: The `/context` overlay surface. Per-agent and aggregated layouts. Rendered inside a wezterm overlay using rich text + kitty graphics protocol images (sparklines, donut charts) emitted by the daemon. Live updates via `context.subscribe`.
- `observability-cockpit`: A pane (or overlay) showing recent OTel spans, throughput sparklines, latency percentiles, cost-per-hour, error rate. Powered by `observability.spans` and `observability.metrics`.
- `metrics-rollup`: SQLite-backed five-tier retention (raw / hourly / daily / weekly / monthly) plus a daemon-internal scheduler that demotes samples between tiers. Exposes `metrics.rollup.get` for future comparison overlays. Aggregation rules per metric kind (counter → sum, histogram → quantiles, gauge → mean).
- `status-bar`: Lua-driven wezterm status bar that calls `milliwaysctl status --json` (or subscribes to `status.subscribe` if Lua coroutine support allows). Shows active agent, tokens, cost, quota, OTel error count.

### Modified Capabilities

- `repl-fallback` (modifies `milliways-repl`): Existing in-host REPL is preserved behind `milliways --repl`. New default (no flag) launches the daemon-plus-cockpit if not already running, then exec's `milliways-term`. Document deprecation timeline; keep parity for at least one release.

## Impact

New paths:

- `crates/milliways-term/` — Rust fork of wezterm. Cargo workspace member.
- `crates/milliways-term/milliways/` — all in-fork additions (agent domain, cockpit overlay, status hook helpers, kitty-graphics-emit utilities).
- `crates/milliways-term/PATCHES.md` — running list of files patched in upstream wezterm with one-line rationale per patch.
- `cmd/milliwaysd/` — Go daemon main.
- `cmd/milliwaysctl/` — Go thin client (used by wezterm Lua + by humans).
- `internal/daemon/` — daemon-only glue (RPC server, subscription multiplexer, single-instance lock).
- `internal/rpc/` — shared RPC types (Go side); Rust side regenerates from a single schema file in `proto/milliways.json` via codegen.
- `proto/milliways.json` — JSON Schema for RPC types, source of truth for both languages.
- `${state}/metrics.db` — SQLite file for metrics roll-up tiers. Created on first daemon start, schema-migrated by the daemon.

Reused existing paths:

- `internal/repl/runner_*.go` — runner adapters lifted into `internal/daemon/runners/`. The legacy REPL keeps its own copy until removed.
- `internal/session/`, `internal/mcp/`, `internal/mempalace/`, `internal/sommelier/`, `internal/pantry/` — moved or imported from `internal/daemon/*`. No semantic change.
- `internal/observability/` — daemon installs the OTel SDK; cockpit reads spans from a ring buffer maintained alongside.

New dependencies:

- Rust: whatever wezterm already pulls in. Plus `serde_json`, `tokio` for the RPC client (wezterm already uses tokio). Plus **`plotters`** (or equivalent) for Rust-side chart rendering — donut, sparkline, bars, line — output as PNG bytes wrapped in kitty-graphics escapes. Plotters is `MIT/Apache-2.0` and lightweight (one transitive: `image`).
- Go: `github.com/sourcegraph/jsonrpc2` (or stdlib `net/rpc/jsonrpc` + custom transport). No new heavy deps. **No charting lib in Go** — the daemon only emits structured chart data.

Build implications:

- Two toolchains required: Rust stable (matching wezterm's toolchain pin) and Go 1.22+.
- `Makefile` targets: `make term` (Rust build), `make daemon` (Go build), `make all` (both), `make repl` (legacy fallback).
- macOS + Linux first; Windows tracked but not blocking MVP.
- CI: separate jobs for Rust build/test, Go build/test, and an integration smoke job that starts the daemon and runs `milliwaysctl ping`.

Backward compatibility:

- `milliways --repl` is identical to today's `milliways`. Same flags, same config, same session store path.
- Default `milliways` (no flag) becomes the new launcher: ensures daemon is up, then exec's `milliways-term`. Documented in `--help`.
- Runner CLI compatibility unchanged.
- Existing `~/.config/milliways/` config unchanged for the legacy path; new `~/.config/milliways/term.lua` introduced for wezterm-side config.

Risks called out in `design.md`: upstream wezterm merge friction, JSON-RPC schema drift between Rust and Go, kitty graphics rendering inside an overlay, latency budget for cockpit updates, daemon crash recovery while a pane is open.
