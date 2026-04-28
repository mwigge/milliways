## Context

milliways routes prompts across runners (claude, codex, minimax, copilot) through a terminal REPL. Each runner maintains its own server-side session; milliways holds a shared `ConversationTurn` ring buffer (max 20 turns, plain text, runner-agnostic) that is injected as history into every `DispatchRequest`. When `/switch` is used, the new runner already receives the transcript — but this is silent and unstructured. There is no explicit signal to the new runner that a handoff occurred, no summary of in-progress work, and no automatic trigger when a runner's session limit is reached.

Runners communicate session exhaustion today by failing with error text (e.g. `claude: session limit reached`, Codex exit code, MiniMax API 429). These are surfaced as error lines in the REPL with no follow-up action.

MemPalace is optionally wired via MCP (`MILLIWAYS_MEMPALACE_MCP_CMD`). If present, it can store and retrieve persistent facts across runners.

## Goals / Non-Goals

**Goals:**
- `/takeover [runner]` command: generate a structured handoff briefing from current session state and switch to the named runner (or best available if omitted)
- `/takeover-ring <r1,r2,...>` command: configure a priority rotation ring persisted for the session
- Auto-rotate: when a runner signals `SessionLimitReached`, silently rotate to the next ring member and continue
- MemPalace snapshot: on any takeover, write a `handoff` drawer to the active palace with task summary, key decisions, and recent file changes
- Status bar ring indicator: `[●claude 2/4]` when ring is active
- The new runner receives the briefing as its first `ConversationTurn` so it knows it is continuing work, not starting fresh

**Non-Goals:**
- Transferring the runner's native server-side session (Claude session_id, Codex session ID) — the model's KV cache cannot be moved; only the transcript is portable
- Resuming from further back than the 20-turn buffer without MemPalace (that is a separate MemPalace feature)
- Load-balancing across runners — ring is strictly sequential, not round-robin by load
- Multi-machine session sharing — ring operates within a single milliways process

## Decisions

### D1: Briefing format — structured markdown injected as a synthetic turn

**Decision:** The takeover briefing is a synthetic `ConversationTurn{Role: "user"}` prepended to the new runner's history, formatted as:

```
[TAKEOVER from claude → codex]
## Current task
<last user prompt that started the task>
## Progress
<last 3 assistant turns summarised as bullet points>
## Files changed this session
<git diff --name-only since session start>
## Key decisions
<extracted from turns: sentences containing "decided", "we will", "use X instead">
## Next step
<last assistant message's final paragraph>
```

**Alternatives considered:**
- Inject as `Rules` field: too passive — runners treat rules as background, not active context
- Send a summary request to the outgoing runner first: adds latency and requires the outgoing runner to still be functional (it may have just errored)
- System-level `Context` fragment: viable but doesn't appear in conversation history, so the new runner can't reference it in replies

### D2: SessionLimitReached signal — sentinel exit code / event type per runner

**Decision:** Each runner's `Execute` method emits a progress event with type `"session.limit_reached"` when it detects exhaustion. Detection logic per runner:

| Runner | Detection |
|--------|-----------|
| Claude | Stderr contains `session limit` / `context window` / exit code 130 after context error |
| Codex | Stderr `max_turns` / `context_length_exceeded` JSON event |
| MiniMax | HTTP 429 with `quota_exceeded` body |
| Copilot | Stderr `rate limit` pattern |

The REPL dispatch loop already receives events on a channel. A new branch checks for `session.limit_reached` before surfacing the error, and if a ring is configured, calls `rotateRing()` and re-dispatches.

**Alternatives considered:**
- Parse exit codes only: fragile — Claude and Codex use overlapping exit codes for different errors
- Poll quota before dispatch: adds latency on every turn; quotas are checked by `pantry` already but don't catch mid-session context exhaustion

### D3: Ring state — in-memory, persisted to PersistedSession

**Decision:** Ring state is kept in a `RingConfig` struct on the REPL (`runners []string`, `pos int`). On session save, it is serialised into `PersistedSession` so `/takeover-ring` survives a restart.

```go
type RingConfig struct {
    Runners []string `json:"runners"`
    Pos     int      `json:"pos"`
}
```

**Alternatives considered:**
- Store ring in `carte.yaml`: would apply globally rather than per-session; user may want different rings per project
- Ephemeral only (no persist): ring must be re-declared after every restart — too fragile for the auto-rotate use case

### D4: MemPalace snapshot — best-effort, non-blocking

**Decision:** If `mempalace` MCP is wired, the takeover path fires a `mempalace_add_drawer` call asynchronously. Failure is logged but does not block the takeover. The drawer key is `handoff/<timestamp>` and the content is the same structured markdown as the briefing.

**Alternatives considered:**
- Block takeover until snapshot completes: MemPalace can be slow or unavailable; the runner switch must not be gated on it
- Always require MemPalace: takeover should work without MemPalace for users who haven't set it up

### D5: Briefing generation — local, no LLM call

**Decision:** The briefing is generated locally from `ConversationTurn` data using heuristics (last N turns, keyword extraction for decisions). No outgoing LLM call is made during the briefing step.

**Rationale:** The outgoing runner may be in a broken state (limit reached). Sending a "summarise yourself" prompt would fail or produce a low-quality summary. Local generation is deterministic and instant.

## Risks / Trade-offs

- **Context loss past 20 turns** → Mitigation: MemPalace snapshot captures key facts; document the limit clearly in `/takeover` output
- **False positive limit detection** → Mitigation: detection patterns are conservative (require specific substrings, not just non-zero exit); log the raw signal for debugging
- **Ring loops back to an exhausted runner** → Mitigation: ring skips runners whose quota is `0` in `pantry`; if all are exhausted, surface a clear error rather than looping infinitely
- **Briefing noise** → The synthetic turn adds tokens to the new runner's context; it is capped at 500 tokens and the oldest real turn is dropped to compensate
- **Multi-instance race** → Two milliways instances sharing the same session file could clobber ring state; no locking today — document as known limitation, same as existing session save race

## Migration Plan

1. Land runner `SessionLimitReached` events (each runner independently, no user-visible change yet)
2. Land `handleTakeover` + briefing generator behind the command (no auto-rotate yet)
3. Land ring config + auto-rotate
4. Land status bar ring indicator
5. Land MemPalace snapshot (gated on `MILLIWAYS_MEMPALACE_MCP_CMD` presence)

Each step is independently deployable and tested. No schema migration needed (new `ring` field in `PersistedSession` is additive).

## Open Questions

- Should `/takeover` without a runner argument pick the next ring member, or the sommelier's best choice? (Proposal assumes ring-next if ring active, sommelier otherwise)
- Cap the auto-rotate count per session? (Prevent silent infinite loops if all runners fail for a non-limit reason)
- Should the status bar always show ring position, or only when ring is active?
