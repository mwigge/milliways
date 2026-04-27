# Review — milliways-emulator-fork

**Verdict:** proceed-with-changes

The shape is right; the boundary protocol and several Phase-bundling decisions need tightening before code starts. Nothing here is direction-changing, but at least four items will burn time during implementation if not resolved up front.

---

## Top risks (ordered by severity)

### 1. The NDJSON sidecar handshake is a resource leak waiting to happen

`term-daemon-rpc/spec.md` says: client calls `agent.stream`, gets a `stream_id`, then opens a *second* UDS connection and writes `STREAM <id>\n`. Two unstated failure modes:

- **Orphaned stream_ids.** If the client never opens the second connection (crash between calls 1 and 2; ENOMEM on socket open; user kills the terminal), the daemon is holding a `handle → stream_id` reservation indefinitely. The spec only says "daemon SHALL detect disconnect within 1s" *after* a connection exists. Nothing says what happens before one ever exists.
- **Race between attach and emit.** First bytes from the runner can land before the client's sidecar attaches. Where do they go — buffered, dropped, blocked? The spec doesn't say. Reconnect after daemon restart (which the agent-domain spec promises is exact) requires a buffer; the buffer needs a documented bound.

**Fix:** Add to the spec — `stream_id` reservations expire after 5s if the sidecar connection has not been established (`-32003 stream_attach_timeout`). Daemon SHALL maintain a per-handle output ring (size configurable, default 256KB) so the sidecar replays from the last delivered offset on reconnect. Sidecar attach line becomes `STREAM <stream_id> <last_seen_offset>\n`.

### 2. `Content-Length`-framed JSON-RPC is the wrong framing for a UDS-only, single-language-pair channel

LSP uses `Content-Length` because LSP runs over stdio with arbitrary embedded `\n` and competes with subprocess noise. None of that applies here: it is a dedicated UDS, both sides are us, the rest of the protocol (the sidecar) is already newline-delimited. Mixing framings ("unary uses Content-Length, streams use NDJSON") doubles the parser surface, doubles the bug surface, and makes `jq` at the wrong moment hang.

**Fix:** Use newline-delimited JSON-RPC for unary too. One framing across the whole protocol; the sidecar is just NDJSON with a one-line preamble. `jsonrpsee` does not natively do NDJSON, but `sourcegraph/jsonrpc2` does — and on the Rust side a 30-line stream adapter is trivial. Document this as Decision 3a.

### 3. Wezterm `Domain` trait stability is hand-waved

Decision 2 leans on `Domain` as if it were a public, stable extension point. It is not — wezterm has no API stability guarantees and `Domain` has been refactored multiple times in the upstream history. The "monthly merge" plan (Decision 1) collides with this directly: a `Domain` signature change in upstream means every monthly merge can break the agent-domain crate.

The deeper asymmetry: real PTY panes have a real fd that wezterm reads; agent panes have a virtual PTY fed from a tokio task. Wezterm's pane lifecycle (resize, close, restart, copy mode, search) makes assumptions about that fd. Search and scrollback against a virtual PTY whose history is not really in a tty buffer is going to surface edge cases nobody has hit upstream.

**Fix:**
- Add a Phase 2.5 spike: implement a no-op `AgentDomain` that just spawns `cat` over a virtual PTY and run wezterm's full pane test surface (resize, copy mode, search, split, scrollback). Sign-off gate before Phase 3 starts.
- Capture the upstream `Domain` trait surface area in `PATCHES.md` — every method, with the version it appeared in. The CI "stale fork" warning becomes "if `Domain` trait changed, this becomes a fail not a warn."
- Acknowledge in design.md that the patch budget (<500 LoC) may need to grow to wrap `Domain` in a milliways-side shim that absorbs upstream churn.

### 4. Kitty graphics in overlays — unverified assumption that breaks the visual contract

The context-cockpit spec hard-commits to "no Unicode block characters, no ASCII art — kitty graphics PNGs." The design assumes wezterm renders kitty graphics inside overlays the same as in panes. **This is unverified.** Wezterm's overlay surface (used today for `CommandPalette`, `LaunchMenu`) is a separate render path; whether it consumes kitty-protocol escapes is not documented and historically these surfaces have been text-only.

If the assumption is wrong, the entire visual fidelity bar in `context-cockpit/spec.md` is unreachable with overlays — you'd have to change the implementation to a real pane (like the observability cockpit) and redesign the keybinding semantics.

**Fix:** Move this from "Open Question 2" (which only asks about caching) to a **blocking spike in Phase 0**. One afternoon of `wezterm-gui` experimentation answers it. If the answer is "no, overlays don't render kitty graphics," `/context` becomes a pane (not an overlay) before Phase 5 starts. Don't discover this in Phase 5.

### 5. Status-bar tmp-file fallback has a real atomicity hole on macOS

`status-bar/spec.md` says the watch sidecar atomically writes `${state}/status.cur` (write-temp, rename) and the Lua hook reads it on each `update-right-status` tick. Two issues:

- **Tight read loop.** Even gated to "fresher than 2s," the Lua hook calls `io.open` on every tick (default 1 Hz). That's fine for cost — but on Lua's blocking `io.open` it adds a syscall to the UI thread every second. Acceptable, but document that the hook MUST `os.time() - mtime < 2` *before* the open, not after.
- **Atomic rename caveat on APFS.** `rename(2)` is atomic at the directory-entry level on APFS, so a concurrent reader will see either the old file or the new one — but Lua's `io.open` followed by `:read("*a")` is not one syscall; the file can be replaced between open and read on Linux (where `open` returns a dangling fd that still reads the old inode — fine) and on macOS APFS the same. The risk is not partial state — it's stale state right after a rename, which is fine.

**Fix:** Change spec wording from "atomically (write-temp, rename)" to "write to `${state}/status.cur.tmp`, fsync, rename to `${state}/status.cur`. Reader SHALL `stat` first, skip if `mtime - now > 2s`, then `open + read + close` (do not retain handle)." Also add: the watch sidecar SHALL hold a debounce so it does not rewrite the file faster than 4 Hz.

### 6. The metrics-rollup transactional model has a cross-tier race

TASK-6.5.3 says "each step SHALL run in its own transaction." Cockpit clients query buckets across tiers (e.g., "give me the last 24 hours: any raw newer than 60min, plus hourly older than that"). Between the per-tier transactions, the scheduler can have demoted a row out of `raw` into `hourly` such that a concurrent reader sees the row in *neither* tier (committed-out-of-raw, not-yet-committed-into-hourly) **or in both** (different transaction order).

The spec's scenario "Concurrent reads do not block the rollup scheduler … SHALL never observe a partially-aggregated bucket" only addresses within-tier consistency, not cross-tier.

**Fix:** Either (a) wrap the entire cascade in one transaction (simple, slightly larger lock window — fine at this data volume), or (b) make `metrics.rollup.get` accept multi-tier queries and serve them from a single read transaction. Option (a) is the right call. Update TASK-6.5.3 and the spec scenario accordingly.

### 7. Phase 3 buries reconnect logic under AgentDomain MVP

TASK-3.2 lists "Implement Domain trait" *and* "Reconnect logic: red banner via OSC, retry every 2s for 30s" in the same task. Reconnect is its own animal — it requires daemon-side stream resume (item 1 above), pane-side state machine, OSC-rendering banner, keybinding for manual retry. It is not a checkbox at the end of the AgentDomain task.

**Fix:** Split into:
- TASK-3.2: AgentDomain spawn_pane + read/write loops.
- TASK-3.4: Pane-side reconnect state machine + banner rendering.
- TASK-8.1 keeps the "30s exhausted, prompt user" UX but drops the redundant 2s/30s loop description.

### 8. "Beautiful" as an acceptance criterion lacks teeth

`context-cockpit/spec.md` lists five visual criteria and a side-by-side screenshot review. None of those criteria is falsifiable enough to let a reviewer say "no" without it sounding like taste. "Comparable visual fidelity" is the kind of phrase that gets argued for two weeks.

**Fix:** Add measurable thresholds:
- Information density: number of distinct labelled data points visible above the fold ≥ N (set N from a Claude Code screenshot count).
- Typography: at least 3 weight levels in use (e.g., bold model name, regular attributes, dim metadata).
- Charts: at least 2 distinct kitty-graphics images per overlay (donut + sparkline minimum).
- Colour: zero raw hex literals in `context_overlay/` source — only `theme.*` references. Enforce via `cargo clippy` lint or a `grep` test.

The semantic-palette no-hex-literal rule is the only one of the five that's actually mechanically checkable today. The fix above makes the others mechanically checkable too.

---

## Per-decision notes

**Decision 3 (RPC framing).** See risk 2. Drop `Content-Length` framing. Newline-delimited JSON-RPC throughout.

**Decision 4 (overlay + kitty graphics).** See risk 4. Make this contingent on a Phase-0 spike outcome.

**Decision 6 (observability as a pane).** Right call, but the spec ("Pane backed by daemon stream, not Rust-side rendering") is tighter than the design implies. The daemon emitting cursor-home + clear + redraw frames is fine, but the observability spec should document the byte budget per frame (say, ≤32KB for the full layout including PNG re-uploads) and the redraw cadence (1 Hz). Otherwise a busy daemon can saturate the UDS with frame chatter and starve agent streams.

**Decision 9 (metrics-rollup).** See risks 6 and the histogram-percentile concern below. The "weighted-average percentiles documented as approximate" line is honest, but the magnitude of the error needs a sentence: weighted-averaging p95s across heterogeneous buckets can produce cross-tier values that misrank by 10–30% for long-tailed latency distributions. For the comparison overlay use case this is fine; for "is my agent slower today than yesterday at the second decimal" it is not. Document that rollup percentiles should not be used for SLO violation detection — only the `raw` and `hourly` tiers are SLO-grade.

**Decision 10 (publish gate).** Fine.

---

## Per-spec notes

**term-daemon-rpc/spec.md.** Add error code table — at minimum `-32001 stream_not_found`, `-32002 version_mismatch`, `-32003 stream_attach_timeout`, `-32004 method_disabled`, `-32005 quota_exceeded`. Today only `-32601` is named. Versioning requirement is missing the *handshake order*: when does the version check happen? It needs to be the first call, before any stream open.

**agent-domain/spec.md.** "User input flows back via `agent.send` … no less frequent than every 16ms" — that's a 60Hz tick on the input path. Fine, but the spec should also say what happens to single-keystroke latency (we don't want to add 16ms to every `Enter`). Recommended: flush immediately on newline, batch otherwise.

**context-cockpit/spec.md.** "every field listed above SHALL be visible without scrolling on a 1920x1080 display at default font size" — what *is* default font size? Wezterm's default depends on platform. Pick a number (e.g., 14pt JetBrains Mono) and write it in the spec.

**metrics-rollup/spec.md.** Cross-tier transaction issue (risk 6). Also: schema does not specify timezone handling. "Calendar months, not 365d, to avoid drift" — calendar months *in which timezone*? The user's local? UTC? This matters for the year-on-year comparison story.

**status-bar/spec.md.** Risk 5. Also: "`milliways.init` (Lua side) SHALL spawn `milliwaysctl status --watch &` once" — what process tree owns this background process? If the user closes the wezterm window, does the watcher leak? Add: SHALL be killed on `gui-detached` Lua event, and SHALL exit when its parent `milliways-term` exits (via `prctl PDEATHSIG` on Linux; equivalent on macOS via kqueue NOTE_EXIT).

**wezterm-fork/spec.md.** License attribution: missing requirement that source files patched by milliways retain wezterm's per-file copyright header (Apache-2.0 §4(c)). Add a scenario. Also missing: `NOTICE` distribution in shipped binaries — `MILLIWAYS_NOTICE.md` has to be reachable from the running binary somehow (e.g., `milliways-term --notice` prints it). Apache-2.0 §4(d) requires it.

**repl-fallback/spec.md.** "wait up to 5s for the socket to become reachable" — what happens at 5.001s? The scenario stops there. Add the timeout outcome (print error, suggest `--repl`).

---

## Phasing notes

- **Phase 0 missing the kitty-graphics-in-overlay spike** (risk 4). Add as TASK-0.3.
- **Phase 0 missing the AgentDomain-with-cat smoke spike** (risk 3). Add as TASK-0.4 (or a new Phase 2.5).
- **Phase 3 too dense** (risk 7). Split reconnect.
- **Phase 5 TASK-5.2** ("kitty-graphics chart emitter … pure-Go PNG generation via `image/png`. Minimal vector lib (or hand-drawn lines).") is dramatically optimistic. Donut charts, gradient fills, anti-aliased sparklines from raw `image/png` is real work. Either: (a) accept lower visual quality and say so, (b) bring in a Go charting lib (`go-chart` or similar — adds a real dep), or (c) move chart rendering to the Rust side (it has access to wezterm's font stack and existing image pipelines). Recommend (b) with `go-chart` and document it as a new dep in proposal.md.
- **Phase 6.5 numbering** ("Phase 6.5") is a smell — it suggests it was inserted late and not properly ordered. Metrics rollup is a *prerequisite* for the observability pane's sparklines (Phase 6) being meaningful beyond the live tier, not a footnote after it. Reorder: 6.5 → 5.5 (before observability pane consumes it), or fold rollup writes (raw tier only) into Phase 1 and rollup demotion into Phase 6.
- **TASK-8.4 CI matrix** treats integration-smoke as Linux-only. macOS is the *primary* target (the keybindings are `Cmd+...`). Add a macOS smoke job, even if it skips GUI assertions.

---

## Things that are right

- Decision 1 (fork, not vendor; merge, not rebase; pin to release tags). Right call, well-justified.
- Decision 5 (status bar via `update-right-status` + ctl) — pragmatic, doesn't depend on speculative Lua coroutine support.
- The MVP / P2 split in proposal.md is clean. Out-of-scope items are named.
- Single JSON Schema source of truth + codegen drift CI check (TASK-0.2). This is the right way to avoid Rust/Go field drift.
- The reserved underscore-prefixed agent ids (`_observability`) is a clean extensibility primitive.

---

## Open questions to resolve before code starts

1. **Does wezterm render kitty graphics inside `Overlay` surfaces?** Phase-0 spike. If no, `/context` is a pane, not an overlay.
2. **Does wezterm's `Domain` trait survive a no-op AgentDomain over a virtual PTY for resize / copy / search / scrollback?** Phase-0 spike. If no, the patch budget grows.
3. **What is the stream output buffer size and replay protocol for sidecar reconnects?** Decide before TASK-3.1.
4. **Calendar month timezone for monthly rollups: local or UTC?** Decide before TASK-6.5.1.
5. **Go charting library: accept `go-chart` (or equivalent) as a dep, or downgrade the visual contract?** Decide before TASK-5.2.
6. **Pinned upstream wezterm tag at fork time.** What version? Stamp it in `PATCHES.md` before Phase 2 begins.
7. **macOS smoke test scope in CI** — what subset of the integration smoke is feasible without a GUI session?

---

handoff: user reviews REVIEW.md, decides on resubmit or proceed
