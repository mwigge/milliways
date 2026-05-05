## Context

Milliways today is a Go REPL frontend (`cmd/milliways/`) that runs inside a host terminal. It draws a status bar with ANSI scroll regions and shells out to runner CLIs (claude/codex/minimax/copilot) via `creack/pty` for short-lived subprocess invocations. All of the actual cockpit-shaped value — sessions, MCP, MemPalace, sommelier routing, pantry quotas, OTel spans, runner state — already exists, but is invisible to anyone who is not currently typing into the REPL.

We want a native window where:
- A shell pane and an agent pane sit in the same tab.
- The status bar shows live cockpit state at all times, not just during a dispatch.
- A keypress opens a `/context` overlay that renders the *full* state of one agent (or all of them) with the visual fidelity of Claude Code's `/context` — typography, semantic colour, sparklines, charts, not box-drawing ASCII.
- Recent OTel spans are visible in a pane, not buried in a log file.

Previously evaluated alternative (rejected): from-scratch Go terminal with cgo+GLFW+OpenGL. Months of reinvention; loses to wezterm/Alacritty on perf; Go GC pauses are visible on heavy scroll. The full proposal artifact for that path was archived to `/tmp/milliways-emulator-superseded-*` for reference.

Reference implementations that informed this design:
- **wezterm** (wez/wezterm) — the fork target. Rust, GPU, kitty graphics protocol, OSC 8, Lua config, Domain abstraction, mux protocol over UDS. Apache-2.0.
- **Claude Code's `/context`** — the visual target for the cockpit overlay. Rich-text density, gradient bars, sparklines.
- **kitty's remote control protocol** — prior art for "terminal exposes structured state to scripts." We don't implement kitty's protocol; we put structured state behind our own RPC.

## Goals / Non-Goals

**Goals:**
- A single binary launch (`milliways`) brings up a ready-to-use cockpit: shell tab, agent tab, status bar, keybindings.
- Agent panes feel native — same scroll, search, copy/paste, image protocol as a shell pane.
- `/context` overlay renders in <100ms after keypress, updates live during a dispatch, and is "beautiful" by the Claude Code bar — meaning: kitty-graphics charts, real typography, semantic colour, no ASCII art.
- Daemon survives terminal restarts. Pane reconnect resumes the in-flight stream.
- Legacy `milliways --repl` keeps working until parity is declared.

**Non-Goals:**
- Replacing wezterm's existing surface (tabs, splits, search, copy mode, config). We add; we do not subtract.
- Cross-process collaboration (multi-user, remote daemons). Single user, single host this round.
- A custom config DSL. Wezterm's Lua config is the config; we add a `milliways.lua` import.
- Replacing the legacy REPL in this change. Separate change.
- Windows support. Wezterm itself supports Windows; the Go daemon and UDS path do not target it this round.

## Decisions

**1. Fork wezterm, do not vendor it as a library.**

Wezterm is not designed to be embedded as a library; its crates are tightly coupled and the public API surface is unstable. Forking is the pragmatic move. Patch surface in upstream files is kept minimal (target: <500 lines patched across all upstream files); all additions live in `crates/milliways-term/milliways/` as a sibling subtree to upstream's `wezterm/`, `wezterm-gui/`, `mux/`, etc.

Upstream sync: pin to a specific wezterm **commit hash** at fork time. Originally this was specified as "release tag," but wezterm has not tagged a release since 2024-02-03 (verified at fork time, 2026-04-27); upstream's de-facto release model is rolling main. We therefore pin a commit hash and treat it as a tag-equivalent: recorded in `PATCHES.md`, immutable until the next sync cycle. Sync cadence: monthly, OR whenever milliways cuts a new release — whichever comes first. Each sync runs `git subtree merge --prefix crates/milliways-term --squash <wezterm-repo> <new-commit>` (merge with squash, not rebase, to keep monthly boundaries reviewable as one commit per cycle). The new pinned commit is recorded at the top of `PATCHES.md`; the inventory of patched files is updated in the same commit.

**2. Agent panes via a new `Domain` implementation, not a new pane type.**

Wezterm already has the concept of `Domain` — the source of panes. Local domain = local PTY+shell. SSH domain = SSH session. Mux domain = remote wezterm-mux server. We add `AgentDomain` whose `spawn_pane` connects to `milliwaysd` over UDS, opens an agent stream, and returns a pane backed by a virtual PTY whose master is fed by daemon bytes.

This means agent panes inherit, free of charge: scrollback, search, copy/paste, kitty graphics protocol rendering, hyperlinks, mouse selection, font rendering, all keybindings. We do not write any of that.

**3. RPC protocol: JSON-RPC 2.0 over UDS, newline-delimited (NDJSON) throughout.**

JSON-RPC because:
- Both Rust and Go have mature stream-friendly implementations.
- Trivial to inspect with `jq`.
- Schema lives in `proto/milliways.json` (JSON Schema), regenerated to types in both languages.

**One framing across the whole protocol** — newline-delimited JSON-RPC for unary calls and NDJSON for streams. We explicitly reject `Content-Length`-framed JSON-RPC (LSP-style) because we have no requirement for it: this is a single-language-pair, dedicated UDS channel; LSP's reasons (stdio with subprocess noise) do not apply, and mixing two framings would double the parser surface for no benefit.

Streams (`agent.stream`, `context.subscribe`, `status.subscribe`, `observability.subscribe`) cannot use bare JSON-RPC notifications because the spec doesn't define server-pushed responses cleanly. Convention: client calls `agent.stream(opts)` and gets back `{stream_id, output_offset}`; client opens a separate NDJSON-framed connection on the same UDS path, identified by `STREAM <stream_id> <last_seen_offset>\n` preamble, until either side closes. Documented in `term-daemon-rpc/spec.md`.

**Stream replay**: every stream carries a per-handle output ring (default 256KB). On reconnect, the client passes its `last_seen_offset` and the daemon replays missed bytes before resuming live emission. Reconnect window is 30s; reservation expiry on initial sidecar non-attach is 5s. Both bounded explicitly so a crashed terminal cannot leak resources indefinitely.

UDS path: `${XDG_RUNTIME_DIR:-$HOME/.local/state/milliways}/sock`, mode 0600. No token auth; file-mode is the auth surface. Single-instance lock via `flock` on a sibling `pid` file.

**4. `/context` rendered via wezterm overlay + kitty graphics protocol — contingent on Phase-0 spike.**

Wezterm exposes overlays — full-window or partial-window surfaces drawn over the active pane. We add a `ContextOverlay` that:
- On open, calls `context.get` for the focused agent (or `context.get_all` for aggregated).
- Subscribes to `context.subscribe` for live updates.
- Layout: header (agent name, model, session id), token-budget donut chart, conversation timeline sparkline, tool list, MCP server list, files in context (with file-tree rendering), cost meter, error badge.
- Charts and sparklines: daemon emits PNG bytes via kitty graphics protocol (escape sequence `ESC _ G ... ESC \`). The overlay just embeds the escape sequence in its draw output. Wezterm renders it.
- Typography: use wezterm's existing font stack at the milliways baseline (JetBrains Mono 14pt, configurable); semantic colour comes from a small Lua palette (`milliways.theme`).

**Spike-blocked decision.** Wezterm's overlay surface is a separate render path from regular panes; it is not documented to consume kitty-graphics escapes. A Phase-0 spike (TASK-0.3) MUST verify that overlays render kitty graphics. If the spike returns negative, `/context` falls back to being a real pane (like the observability cockpit), with its own keybinding semantics — `Cmd+Shift+C` opens `/context` as a tab rather than an overlay, and Esc closes the tab. The visual contract (kitty-graphics charts, no ASCII art) is preserved either way; only the surface changes.

**5. Status bar via wezterm's `update-right-status` Lua hook.**

Wezterm fires `update-right-status` periodically (default 1 Hz, configurable). Our hook calls `milliwaysctl status --json` (one-shot, fast — daemon already has the state cached) and renders an enriched status string. For sub-second updates during a dispatch, we use `milliwaysctl status --watch` which streams over a long-lived RPC and writes the freshest line to a tmp file the Lua hook reads. (Lua coroutine + UDS would be cleaner; tmp-file fallback is a hedge if coroutine support is awkward.)

**6. Observability cockpit as a pane, Rust-rendered.**

The observability cockpit is a real pane (not an overlay) because users want to keep it open while they work. It is opened via `Cmd+Shift+O`, lands as a new tab (or split, if user splits it), and renders:
- Live span tail (top half): scrollable list of recent spans, severity-coloured, click-to-expand attributes.
- Throughput sparkline (top right): tokens/sec across all agents — Rust-side rendered PNG via `plotters`.
- Latency percentiles (middle): p50/p95/p99 per agent — Rust-side bars.
- Cost-per-hour (bottom left): Rust-side line chart, last 60 minutes.
- Error rate (bottom right): badge + Rust-side sparkline.

The pane uses the `_observability` reservation in `AgentDomain` (so the picker/keybinding wiring is uniform), but its `spawn_pane` returns a **Rust-internal renderer** rather than a virtual PTY fed by daemon bytes. The renderer:
1. Subscribes to `observability.subscribe` for spans and `metrics.rollup.get`/`status.subscribe` for time series.
2. Composes the layout Rust-side, including chart PNGs assembled from `plotters` against `milliways.theme`.
3. Writes the resulting frame (ANSI text + kitty-graphics escapes) directly into the pane's display surface.

This is the consequence of Decision 12 (chart rendering Rust-side). It is cleaner than the original "daemon emits the whole frame" plan because charts and ANSI rows are now produced by the same system that owns wezterm's font stack and image cache.

**Frame budget.** Renderer emits at most 1 frame per second under steady state. A single frame budgets ≤ 32KB of bytes written into the pane (including any new PNG payloads). PNG payloads are emitted with stable kitty-graphics image ids so unchanged charts cost nothing on subsequent frames. Captured in `observability-cockpit/spec.md`.

**7. Daemon lifecycle and crash semantics.**

Daemon starts on first `milliways` launch (default). If already running, the launcher just exec's `milliways-term`. Daemon stops with `milliwaysctl shutdown` or on SIGTERM. On crash, panes display a red banner + reconnect button; reconnect re-opens the same `agent_id` and resumes the stream (daemon persists conversation buffers per session).

Single-instance enforcement: `flock` on `${state_dir}/pid`. Stale lock detection: if locked but `kill -0 <pid>` fails, take over.

**8. `milliwaysctl status --json` as the canonical cockpit-state read API.**

One CLI, one JSON shape, used by Lua status bar, by humans, by future scripts, by tests. Consistent. Documented in `term-daemon-rpc/spec.md` as the human-facing wrapper around `status.get`.

**9. Metrics retention in five SQLite tiers.**

Live cockpit needs 1Hz samples for the last 60 minutes. For long-term comparison ("this hour vs same hour yesterday", "today vs last 7 days average", "this week vs last 4 weeks", "this month vs same month last year") we add four roll-up tiers:

| Tier    | Bucket   | Retention            | Rows per metric |
|---------|----------|----------------------|-----------------|
| raw     | 1s       | 60 min               | 3600            |
| hourly  | 1h       | 24 h                 | 24              |
| daily   | 1d       | 7 d                  | 7               |
| weekly  | 1 wk     | ~4 weeks (28 d)      | 4               |
| monthly | 1 month  | 12 months (~365 d)   | 12              |

Storage: SQLite at `${state}/metrics.db` (~3647 rows per metric per agent — dominated by the raw tier; the four roll-up tiers add only 47 rows total. Still kilobytes, not megabytes). Schema in `metrics-rollup/spec.md`.

Rollup scheduler: a 1-minute tick in the daemon (a) flushes raw samples older than 60 min into hourly buckets, (b) flushes hourly buckets older than 24h into daily buckets, (c) flushes daily buckets older than 7d into weekly buckets, (d) flushes weekly buckets older than 28d into monthly buckets, (e) prunes monthly buckets older than 12 months.

Aggregation rules per metric kind:
- **Counter** (tokens_in, tokens_out, dispatch_count, error_count, cost_usd): `SUM`. Roll-ups stay sums of the smaller buckets — exact, lossless.
- **Histogram** (latency_ms): retain `count`, `sum`, `min`, `max`, `p50`, `p95`, `p99` per bucket. Higher tiers compute approximate percentiles by `count`-weighted averaging across constituent buckets — lossy. Concretely, weighted-averaged percentiles can misrank by **10–30% on long-tailed latency distributions across heterogeneous buckets**. They are suitable for trend comparison ("is this week slower than last week?") but NOT for SLO-violation detection. Only the `raw` and `hourly` tiers are SLO-grade. Documented in `metrics-rollup/spec.md`.
- **Gauge** (active_agents, mcp_servers_connected): `AVG` weighted by sample count — also lossy across boundaries but adequate for monitoring intent.

**Cross-tier transactional guarantee.** The entire demotion cascade across all five tiers SHALL run in a **single SQLite write transaction** so cockpit reads via `metrics.rollup.get` cannot observe a row that has been demoted out of one tier but not yet committed into the next, nor a row appearing in two tiers. The daemon's metrics write path uses WAL mode (`PRAGMA journal_mode=WAL`) so reads are not blocked by the rollup write transaction.

**Calendar-month bucketing timezone.** Calendar-month boundaries default to the **user's local timezone**, configurable as `milliways.metrics.timezone = "UTC"` in `milliways.lua`. Local is the default because "this month vs same month last year" is a calendar comparison from the user's perspective; UTC bucketing produces month boundaries that drift from the user's intuition (especially in non-UTC timezones near midnight). On a timezone change between runs (laptop travel), existing buckets retain their original `ts` and a one-time discontinuity is logged.

The comparison UI ("this hour vs same-hour-yesterday", "this month vs same month last year", etc.) is **out of scope this change** but the *data collection* is in scope — the comparison overlay ships in a follow-up change reading from `metrics.rollup.get`. Collecting from MVP avoids the trap of the comparison overlay landing with no data behind it. The monthly tier specifically gives us a year-on-year baseline that takes 12 months to accrue, so the cost of *not* starting collection now is large.

**10. Publish `milliways-term` as a public crate only after end-to-end cockpit works.**

The Rust binary is internal until: (a) all eight new capabilities pass their sign-off scenarios, (b) at least one user has driven the cockpit for a full week without daemon crashes or pane stalls, (c) the visual acceptance pass on `/context` is signed off. Until then, builds happen from the monorepo only and there is no `crates.io` publish step.

**12. Chart rendering lives Rust-side, not in the daemon.**

Charts (donut, sparkline, bars, line) are rendered by the Rust crate `plotters`, against wezterm's font stack and the `milliways.theme` palette resolved from `milliways.lua`. The daemon does NOT pull in a Go charting library. The split:

- **Daemon**: exposes `context.chart_data({agent_id?, kind})` returning `{kind, buckets|segments|series, semantic_hints}` — structured data, no presentation. Stable identifiers per chart so the Rust side can cache.
- **Rust**: `crates/milliways-term/milliways/src/charts/` — a thin renderer per chart kind that takes `(chart_data, theme)` and returns PNG bytes. Caller wraps the PNG in a kitty-graphics escape with a stable image-id and stable rendering parameters (so wezterm's image cache absorbs repeats).

Why Rust-side:
- Wezterm already has a font stack and image pipeline; reusing it gives charts that match the rest of the terminal's rendering exactly.
- Chart presentation belongs with the rest of the cockpit visual code, not split across a language boundary.
- Theme is a Lua-resolved structure already on the Rust side; daemon doesn't need to know hex codes.
- Avoids a real Go dependency (charting libs are heavy and `image/png`-only would not meet the visual bar).

Trade-offs:
- Adds Rust complexity in `milliways/src/charts/`. ~500 LoC for the four chart kinds. Acceptable.
- Daemon must keep chart-data RPC granularity tight enough to avoid Rust-side recompute on every frame. Mitigation: data hash returned with each `chart_data` response; Rust skips re-render if hash unchanged.

**Renderer ownership.** Both the `/context` overlay (Decision 4) and the observability pane (Decision 6) use the same Rust chart renderer. One implementation, two consumers.

**11. Wezterm `Domain` trait stability via a milliways shim.**

The `Domain` trait in upstream wezterm has no API stability guarantee and has been refactored multiple times historically. Our patch budget (target <500 LoC) cannot absorb a trait signature change naively. Mitigation:

- All milliways code that touches wezterm types SHALL go through a thin `milliways::wezterm_compat` shim under `crates/milliways-term/milliways/src/wezterm_compat/`. The shim re-exports wezterm types we depend on and provides a stable adapter facing milliways code.
- `PATCHES.md` SHALL list every wezterm trait we depend on, with the upstream tag at which it was last reviewed.
- The monthly upstream-merge CI job SHALL fail (not warn) if any of those trait signatures changed between the previous and current pinned tag, forcing a deliberate `wezterm_compat` update before the merge lands.

The patch budget envelope MAY grow modestly to host the shim; the shim itself is milliways code (in our subtree) so the upstream-patch line count is unaffected.

## Risks / Trade-offs

- **[Risk]** Wezterm upstream merges break our patches.
  → Mitigation: keep patch surface tiny (<500 LoC across upstream files), document each patch, automate merge with a CI job that fails on conflict so we see them early.

- **[Risk]** JSON-RPC schema drift between Rust and Go (a field renamed in Go, not in Rust).
  → Mitigation: single `proto/milliways.json` JSON Schema, codegen for both languages, CI fails if generated files are out of date.

- **[Risk]** Kitty graphics protocol overhead inside an overlay (each redraw re-uploads PNG bytes).
  → Mitigation: wezterm caches images by hash; daemon emits a stable ID per chart and only re-emits when data changes.

- **[Risk]** Cockpit update latency budget (<100ms perceived) blown by RPC roundtrip + render.
  → Mitigation: aggressive client-side caching of last seen `context.get`; subscriptions push only deltas; render is a Rust-side overlay so it doesn't compete with the GL frame loop.

- **[Risk]** Daemon crash while a pane is open leaves the user with a frozen pane.
  → Mitigation: pane shows a red reconnect banner with countdown; auto-retry every 2s for 30s, then ask. Conversation state persisted server-side, so resume is exact.

- **[Risk]** "Beautiful" is subjective; the cockpit could ship and feel mediocre.
  → Mitigation: define explicit visual acceptance criteria in `context-cockpit/spec.md` — kitty-graphics charts, semantic palette, fixed-width information density target — and review against side-by-side screenshots of Claude Code's `/context`.

- **[Risk]** Two-language toolchain (Rust + Go) raises the contributor bar.
  → Mitigation: clear `make` targets, separate CI jobs, and a `CONTRIBUTING.md` section per layer. The boundary between layers is the JSON Schema, which is approachable from either side.

- **[Risk]** Apache-2.0 fork attribution requirements.
  → Mitigation: keep upstream `LICENSE` and `NOTICE` files, add a `MILLIWAYS_NOTICE.md` documenting our additions, do not strip wezterm credits.

## Migration Plan

**Phase 0 — Repo restructure (no behaviour change).**
- Add `crates/` directory; move nothing yet.
- Add `Makefile` targets `term`, `daemon`, `repl`.
- Add `proto/milliways.json` skeleton + codegen scripts.

**Phase 1 — `milliwaysd` daemon stand-up.**
- Create `cmd/milliwaysd/` and `internal/daemon/` with RPC server scaffold.
- Implement `status.get`, `quota.get`, `routing.peek`, `agent.list`. No agent panes yet.
- `milliwaysctl status --json` works against the running daemon.
- Smoke test: daemon up, ctl prints status.

**Phase 2 — Wezterm fork import.**
- `git subtree add --prefix crates/milliways-term wezterm/main` from upstream wezterm.
- Verify `cargo build` produces a working `wezterm-gui` binary, renamed `milliways-term`.
- Add `crates/milliways-term/milliways/` skeleton with empty modules.
- CI: Rust build job green.

**Phase 3 — `AgentDomain` MVP.**
- One agent (`claude`) end-to-end. Open via `Cmd+Shift+A`, see streamed text in a pane.
- `agent.open`, `agent.send`, `agent.stream` on the daemon side.
- `AgentDomain::spawn_pane` on the Rust side.
- Smoke test: open agent pane, type prompt, see response.

**Phase 4 — Status bar.**
- `update-right-status` Lua hook → `milliwaysctl status --json`.
- Renders active agent, tokens, cost, quota.
- Manual: dispatch a prompt, watch numbers update.

**Phase 5 — `/context` cockpit overlay.**
- `context.get`, `context.subscribe` on the daemon.
- Per-agent layout in `crates/milliways-term/milliways/context_overlay/`.
- Kitty-graphics chart emit.
- Aggregated layout (`Cmd+Shift+G`).
- Manual: open overlay during dispatch, watch live updates.

**Phase 6 — Observability cockpit pane.**
- `observability.spans`, `observability.metrics`.
- Pane implementation reuses `AgentDomain` with `_observability` id.
- Manual: open pane, run a few prompts, see spans + sparklines.

**Phase 7 — Other agents (codex, minimax, copilot).**
- Same `AgentDomain`, more `agent_id` values.
- Picker UI in `Cmd+Shift+A`.

**Phase 8 — Polish + parity declaration.**
- Reconnect on daemon crash.
- Visual acceptance pass against `context-cockpit/spec.md`.
- Documentation: `README.md` rewrite, `CONTRIBUTING.md`, `PATCHES.md`.
- `milliways --repl` deprecation note (removal scheduled in a follow-up change).

## Open Questions

1. **Lua coroutine support for streaming status updates.** If wezterm's Lua VM blocks on the RPC call, the tmp-file fallback is required. To be confirmed during Phase 4 with a spike.
2. **Kitty graphics protocol caching key.** Wezterm caches images by hash of PNG bytes — does that interact with overlay redraws cleanly, or do we need to tag images with our own stable ID? To be confirmed during Phase 5.
3. **Span ring-buffer size.** Default 1000 spans (live tier). Tunable via `milliways.lua`. Metric-sample retention is now fixed by `metrics-rollup` (Decision 9).
4. **Authentication for runners that need browser flows.** Today, runner login flows assume an existing TTY. With agent panes we have one (the pane *is* a TTY). Confirm the existing claude/codex auth flows work unchanged when launched from inside an agent pane during Phase 3.
5. **Histogram percentile aggregation accuracy.** Roll-ups compute weighted-average percentiles (lossy). For latency comparison this is fine; if we ever need exact percentiles across long windows, we'd switch to t-digests or HDR histograms. Defer until users ask.
6. **Fork license filename collision.** Wezterm's `LICENSE.md` is at the repo root; we already have `README.md` and `CHANGELOG.md` at the root. Subtree merge will land wezterm's files at `crates/milliways-term/`, no collision. Confirmed.

## Resolved Decisions (previously open)

- **Wezterm version pin** → pin a specific release tag at fork time, sync monthly to a newer release, never `main`. Captured in Decision 1.
- **Publish `milliways-term` crate** → keep internal until full cockpit works end-to-end. Captured in Decision 10.
