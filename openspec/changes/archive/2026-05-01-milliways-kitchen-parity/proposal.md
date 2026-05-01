# milliways-kitchen-parity — Any Kitchen, Any Time, Same Memory

> No kitchen owns the meal.
> The table, the order, the notes, and the memory are MemPalace's.
> The user picks who cooks, whenever they like, for whatever reason.

## Positioning

Milliways is the **maitre d'** of your AI workflow: a terminal-first, keyboard-driven router that seats your task at the right kitchen, remembers what happened across the meal, and keeps the conversation alive when any single kitchen runs out of steam.

Every AI CLI — `claude`, `codex`, `gemini`, `opencode`, `aider`, `goose`, `cline` — is a **kitchen**. Milliways doesn't cook. It decides where to send each course, carries the conversation between courses, and never touches your subscriptions, tokens, or credentials.

Category shorthand: a light, terminal-native AI workspace in the `OpenCode`/`Claude Code` tradition — narrower surface, no subscription management, no hosted sync, no web UI. Multi-kitchen routing and shared conversation memory are the differentiators.

## Why

`milliways-provider-continuity` solved one question: *how does the conversation survive when a provider runs out of tokens?* The answer — canonical Milliways-owned conversation, exhaustion detection, same-block continuation — still stands.

But the user's operating model has shifted. Provider continuity is no longer only a damage-control story. It is the normal case.

The real requirement is:

> "I want to share all memory, context, and where we are right now between all agentic clients, and be able to switch between them seamlessly — either to utilise each CLI's strength, or if I run out of tokens."

Under this frame:

- Failover is **one** trigger for a switch. Others are: the user wants to, the task changed shape, a cheaper kitchen fits now.
- The conversation is not a Milliways-internal object any more. It is a **shared substrate** that any participating CLI can read and write.
- Routing is not a one-shot decision at dispatch time. It is a continuous decision at turn boundaries, with the user always able to override.
- Kitchens become **peers**, not a primary-plus-fallback chain.

Two further decisions taken in exploration:

- **Substrate = MemPalace, forked.** MemPalace already solves persistent memory, semantic recall, and MCP surfacing. It does not yet have a live-conversation primitive. We fork, add the primitive, and live with the fork-maintenance cost because the alternative (reinventing MemPalace inside Milliways) is worse.
- **Focus = TUI as a tmux pane.** The canonical UX is a bubble-tea TUI that lives alongside nvim, lazygit, yazi, and a server pane in a tmux session — matching the terminal-first, keyboard-driven, tmux-pane-per-tool workflow that defines the category. Headless `milliways "..."` keeps working for scripting.
- **Collab = `tmate`, not custom.** Pair-programming is covered by sharing the tmux session via `tmate`. No custom CRDT, OT, or live-share project is on the milliways roadmap. Users on the nvim plugin can additionally layer `live-share.nvim` for finer-grained buffer co-editing. Milliways' own collab contribution is the substrate — two milliways processes can point at the same MemPalace drawer, which is read-only co-presence.
- **Nvim plugin evolution is out of scope here.** The existing thin wrapper keeps working. A future deepening to context-aware L2 (hydrate current buffer, selection, cursor, LSP diagnostics into dispatches) is anticipated as a separate change — not bundled into this one.

## What Changes

### Substrate: MemPalace owns the conversation

The canonical `Conversation` object moves out of `internal/conversation/` (milliways SQLite) and into a new **conversation primitive** in a forked MemPalace. Milliways becomes an orchestrator that reads and writes conversation state via MemPalace MCP calls. Kitchens that speak MCP can read directly; kitchens that don't still get the continuation-prompt bootstrap as a fallback.

New MCP tools required (on the fork):

- `mempalace_conversation_start` / `_end`
- `mempalace_conversation_append_turn`
- `mempalace_conversation_start_segment` / `_end_segment`
- `mempalace_conversation_checkpoint` / `_resume`
- `mempalace_conversation_working_memory_get` / `_set`
- `mempalace_conversation_events_append` / `_query`

These are add-only: existing MemPalace tools and drawers keep working unchanged.

### User-initiated switching

New TUI commands and headless flags:

- `/switch <kitchen>` — explicitly move the current conversation to another kitchen mid-block.
- `/stick` — pin the current kitchen; disable auto-switching until released.
- `/back` — reverse the most recent switch.
- `milliways --switch-to <kitchen>` for paused / resumable sessions in headless.

Every switch — user-initiated or automatic — emits a visible system line with the reason.

### Continuous (not one-shot) routing

The sommelier re-evaluates routing at **turn boundaries**, not only at dispatch start:

- Evaluation is **cheap** — keyword / pantry / learned-history tiers only. No LLM call.
- Switches require **high confidence** — a stickiness threshold (default: +30% confidence delta, or explicit signal like "search the web").
- Switches are always **reversible** via `/back`.
- The user can disable continuous routing for a session with `/stick`.
- A future local-model tier (tiny classifier) is anticipated via an interface slot, but not built in this change.

### Smoke harness in CI

Promote the `/tmp/mw-smoke/` rig used in manual PC-21 verification into `testdata/smoke/` with:

- A set of fake-kitchen binaries covering: normal completion, exhaustion (structured), exhaustion (text), crash, hang, malformed output.
- A `make smoke` target that runs end-to-end against these fakes.
- A CI step that fails the build if any smoke scenario regresses.

This closes the gap that let the `codex` allowlist bug reach manual verification.

## Capabilities

### New Capabilities

- `shared-memory-substrate` — MemPalace fork with conversation primitive; Milliways orchestrator reads/writes conversation state via MCP.
- `user-initiated-switch` — explicit kitchen switching via TUI commands and headless flag.
- `continuous-routing` — turn-boundary re-routing with stickiness, reversal, and user override.
- `smoke-harness` — repeatable end-to-end test rig with fake kitchens, run in CI.

### Modified Capabilities

- `provider-continuity` — conversation state now persisted via MemPalace substrate; native-resume and continuation-prompt paths unchanged in shape but re-sourced from MemPalace.
- `exhaustion-detection` — unchanged in logic, now emits `RuntimeEvent` entries into MemPalace event log instead of the Milliways-local sink.
- `memory-architecture` — typed memory layers (working / episodic / semantic / procedural) map onto MemPalace primitives; episodic replays from MemPalace runtime events.
- `dispatch-presence` — TUI block continues to consume runtime events, but the event source is now MemPalace-backed.

## Explicit Non-Goals

- **Custom collab layer (CRDT / OT / live-share clone).** Not built. Not on the roadmap. Pair programming and co-presence are delivered by `tmate` (tmux session sharing) and optionally `live-share.nvim` (for nvim-plugin users). Milliways contributes read-only substrate co-presence — two milliways instances pointing at the same MemPalace drawer — and that is the extent of first-party collab work.
- **New kitchens.** This change does not add adapter implementations for previously-unsupported CLIs. Existing kitchens and the generic adapter are sufficient.
- **LLM-based routing.** Routing stays cheap (keywords + pantry + learned history). A future tiny-model tier has an interface slot but is not implemented here.
- **Native-resume expansion.** The existing native-resume support from `milliways-provider-continuity` is preserved but not extended to more providers in this change.

## Impact

### New Packages

- `mempalace-milliways/` (forked repo) — MemPalace fork with conversation primitive, maintained separately.
- `internal/substrate/` — MemPalace MCP client wrapper, replaces direct calls to `internal/conversation/` SQLite storage.

### Modified Packages

- `internal/conversation/` — shrinks to typed models + helpers; storage moves to `internal/substrate/`.
- `internal/orchestrator/` — reads/writes via substrate; switch API added.
- `internal/sommelier/` — turn-boundary evaluation loop; stickiness state; `/stick` mode.
- `internal/tui/` — `/switch`, `/stick`, `/back` command handling; visible reason lines.
- `internal/observability/` — runtime events emitted to MemPalace substrate.
- `cmd/milliways/` — `--switch-to` flag; kitchen selection validation.

### New Test Assets

- `testdata/smoke/` — fake kitchen binaries, carte.yaml, expected outputs.
- `testdata/smoke/scenarios/` — per-scenario setup: normal, exhaustion, crash, hang, malformed, switch.

### Migration

Existing `internal/conversation/` SQLite storage is read-only after migration. On first run under the new substrate, in-flight conversations are copied into MemPalace. No user action required. Forked MemPalace MCP server must be installed and configured before upgrade — documented in release notes.

## Conflicts / Supersedes

This change:

- **Supersedes** the `internal/conversation/` SQLite storage introduced in `milliways-provider-continuity`.
- **Extends** provider-continuity from exhaustion-only failover to arbitrary user- or system-initiated switching.
- **Preserves** every behavioural requirement of provider-continuity — nothing currently working breaks.

## Success Criteria

Milliways is successful on this change when:

1. A conversation started in `claude` can be switched to `codex` by typing `/switch codex` in the TUI, and the codex segment begins with the full transcript, working memory, and context available.
2. The same conversation survives milliways restart — on resume, it reads from MemPalace and continues from the exact last state, including provider lineage.
3. A second milliways instance connected to the same MemPalace drawer can read the live conversation — read-only co-presence is proof the substrate is sound. Any richer collab (cursors, co-editing) is delivered by `tmate` for tmux-pane users or `live-share.nvim` for nvim users, not by milliways itself.
4. Continuous routing can suggest a kitchen switch mid-conversation; the user can accept, reject, or silence via `/stick`.
5. `testdata/smoke/` runs in CI and would catch the class of bug that blocked PC-21.1.
6. The `milliways-provider-continuity` closeout is archived and no behaviour it delivered is regressed.
