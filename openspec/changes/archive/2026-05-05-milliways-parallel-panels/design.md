## Context

milliways routes prompts to a single provider per session today. The daemon already manages 7 concurrent runners, each capable of independent `agent.open` / `agent.stream` sessions. MemPalace (the shared knowledge graph, accessible via MCP) already stores cross-session findings. WezTerm is the default terminal with established pane-split usage in the launcher. The chat loop is readline + raw ANSI — no bubbletea or lipgloss in the codebase. The pantry package owns all SQLite state via `schema.go` migrations.

## Goals / Non-Goals

**Goals:**
- `/parallel <prompt>` fans the same prompt to N providers simultaneously and supervises them in a split-pane dashboard matching the agent-deck terminal aesthetic.
- ParallelGroup state (group-id, slot handles, status, tokens) survives daemon restarts.
- Each agent's findings are written to MemPalace post-session and used as prior context on the next `/parallel` run targeting the same path.
- Confidence-weighted consensus summary available on demand (c key) or automatically on group completion.
- Zero new external Go dependencies beyond what is already in go.mod.

**Non-Goals:**
- Merging conversation histories across agents (each thread stays independent).
- `/inject <handle>` cross-panel context injection (deferred to a follow-on story).
- Non-WezTerm terminal emulators as a first-class target (graceful fallback only).
- Changing how single-session routing, stickiness, or takeover work.

## Decisions

### D1: WezTerm native pane splitting — not a single-process TUI renderer

**Decision**: The parallel layout uses `wezterm cli split-pane` to create real terminal panes, one per slot plus one for the navigator. Each content pane runs `milliways attach <handle>` — a new sub-mode that connects to a daemon session and tails its output stream.

**Alternatives considered**:
- *bubbletea + lipgloss*: Correct tool for a single-process split view, but adds two new charmbracelet dependencies and requires refactoring the chat input loop. Deferred until the TUI layer is redesigned.
- *Raw ANSI cursor positioning in one process*: Fragile under resize and doesn't compose with existing readline input handling.

**Rationale**: WezTerm pane splitting is already in use (the launcher's welcome screen documents the Leader+a split shortcut). Each pane is a real TTY so streaming output, color, and cursor handling work exactly as in normal chat. The navigator pane uses the same raw ANSI approach as the existing chat — no new rendering primitives needed.

**Fallback**: When `TERM_PROGRAM != WezTerm` or `wezterm` is not on PATH, `/parallel` runs all slots headlessly and prints a summary to the calling session when they all complete. An informational note tells the user to open panes manually with `milliways attach <handle>`.

### D2: ParallelGroup stored in pantry SQLite, not in-memory

**Decision**: Two new tables (`mw_parallel_groups`, `mw_parallel_slots`) added via a schema version bump in `internal/pantry/schema.go`. All reads/writes go through a new `ParallelStore` in pantry, following the existing `CheckpointStore` / `JobStore` pattern.

**Rationale**: The daemon is already the single SQLite owner. In-memory state loses group status on daemon restart; the user may want to query historical group results or re-run consensus after a restart. Keeping it in pantry is consistent and requires no new storage backend.

### D3: MemPalace write is daemon-side, post-session, not agent-side

**Decision**: When a slot transitions to Done, the daemon's parallel coordinator parses the agent's final assistant message for structured findings (file path lines matching `internal/.+\.go`, issue descriptions following them) and calls `kg_add` via the MemPalace MCP client with `source=<provider>` and `group_id=<group>` tags.

**Alternatives considered**:
- *Agent writes via tool call*: Requires prompting each agent to use MemPalace tools, adds prompt complexity and model-specific fragility.
- *User manually calls `milliwaysctl parallel aggregate`*: Inconvenient; findings accumulation should be automatic.

**Rationale**: The daemon already has the MemPalace MCP client wired (`internal/mempalace/`). Parsing the final assistant message is a deterministic post-processing step that doesn't change how the agent is prompted.

### D4: Consensus aggregation via MemPalace kg_query + token-overlap deduplication

**Decision**: The aggregator calls `kg_query` with `subject_prefix=file:<path>` and `predicate=has_finding`, collects all matching triples tagged with the group_id, groups by subject (file), deduplicates using a simple token-overlap similarity score (Jaccard on unigrams ≥ 0.65 = duplicate), and buckets by source count into HIGH / MEDIUM / LOW confidence tiers.

**Rationale**: No semantic embedding model required. Jaccard on the finding text is sufficient for the primary deduplication case (two agents flagging the same line in different words). The threshold is a compile-time constant that can be tuned without a spec change.

### D5: `milliways attach <handle>` as a new Cobra sub-command

**Decision**: `cmd/milliways/attach.go` adds an `attach` command to the root Cobra tree. It dials the daemon, opens a streaming subscription for the given handle, and prints events to stdout. `--json` flag emits NDJSON events for the navigator to parse slot status.

**Rationale**: Keeps the attach mode a proper CLI citizen (discoverable, testable, usable outside parallel context). The navigator pane calls it as a subprocess, which means it inherits the TTY and streaming works without any IPC complexity.

### D6: Slot selection by provider list from carte.yaml pool config

**Decision**: `/parallel <prompt>` with no `--providers` flag reads the `pool.members` list from the active carte.yaml configuration and opens one slot per member. `--providers claude,codex,local` overrides this.

**Rationale**: Users who have a custom pool already express their preferred provider set in carte.yaml. Defaulting to that avoids requiring extra flags for the common case.

## Risks / Trade-offs

- **WezTerm dependency for full UX**: Users without WezTerm get the headless fallback. The fallback is useful but loses the live supervision view. Documented prominently in `/parallel --help`.
- **MemPalace parsing fragility**: The post-session finding parser depends on the agent's response format. If an agent doesn't mention file paths explicitly, no findings are written. The aggregator handles empty MemPalace results gracefully (renders "no structured findings — check individual panes").
- **SQLite contention**: The parallel coordinator and the chat session share the same pantry DB. Parallel writes are already handled by pantry's WAL mode; adding two new tables introduces no new contention risk.
- **`wezterm cli split-pane` availability**: Requires WezTerm ≥ 20230712 (when `wezterm cli` became stable). The launcher already assumes this version; no regression.

## Migration Plan

1. Schema version bump in `pantry/schema.go` (additive — new tables only, no column changes to existing tables). Applied automatically on daemon start.
2. No config changes required; `/parallel` reads existing `pool.members`.
3. No changes to existing RPC methods; new methods are additive.
4. Roll back: remove `internal/parallel/`, revert `schema.go` bump, drop the two tables manually. Existing sessions unaffected.

## Open Questions

- **Token budget per slot**: Should the parallel coordinator enforce a per-slot token limit to prevent one expensive agent from blocking consensus? Initial answer: no — use existing quota enforcement per provider. Revisit if pool members have very different throughput.
- **Consensus trigger**: Auto-trigger on all-done vs. explicit `c` key. Current spec: both. If auto-trigger produces noisy output in the navigator pane, consider making auto-trigger opt-in via `--auto-consensus` flag.
- **Finding parser v2**: The regex-based post-session parser will miss findings from agents that output structured JSON or diff format. A follow-on story could add a provider-specific parser registry.
