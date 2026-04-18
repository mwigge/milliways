# Design — milliways-kitchen-parity

## D1: MemPalace fork with a conversation primitive

The existing MemPalace has drawers, rooms, wings, a KG, a diary, tunnels, and search. It does not have a first-class live-conversation primitive with ordered turns, provider segments, and runtime events.

We fork MemPalace under a name that signals intent (`mempalace-milliways` or similar) and add the primitive. The fork is maintained with regular rebases from upstream, and all additions are add-only so existing drawers, rooms, and tools keep working.

### D1.1 Conversation schema

```
conversation
  id                 string (uuid)
  prompt             string (original user goal)
  status             enum(active, paused, done, failed)
  created_at         timestamptz
  updated_at         timestamptz
  working_memory     json (typed: summary, open_questions, active_goals, next_action)
  context_bundle     json (spec_refs, codegraph_text, mempalace_text, file_context, prior_outputs)
  active_segment_id  string | null

segment
  id                 string
  conversation_id    string
  provider           string (claude, codex, gemini, ...)
  native_session_id  string | null
  status             enum(active, done, failed, exhausted, cancelled)
  started_at         timestamptz
  ended_at           timestamptz | null
  end_reason         string | null
  exhausted_resets_at timestamptz | null
  index              int (ordinal within conversation)

turn
  id                 string
  conversation_id    string
  segment_id         string
  role               enum(user, assistant, system, tool)
  provider           string
  text               string
  timestamp          timestamptz
  ordinal            int

runtime_event
  id                 string
  conversation_id    string
  segment_id         string | null
  block_id           string | null
  kind               string (route, provider_output, tool_use, failover, checkpoint, context_fetch, user_input, job, memory_retrieve, memory_promote, memory_reject, memory_invalidate, switch)
  provider           string | null
  payload            json
  at                 timestamptz

checkpoint
  id                 string
  conversation_id    string
  at                 timestamptz
  state_snapshot     json (serializable conversation state)
  reason             string (periodic, pre_switch, pre_exhaustion_failover, manual)
```

Tables live inside MemPalace's storage backend (SQLite today). Indexes at least on `(conversation_id, ordinal)` for turns and `(conversation_id, at)` for events.

### D1.2 MCP tool surface

Add-only. Existing tools unchanged.

| Tool | Purpose |
|---|---|
| `mempalace_conversation_start` | Create new conversation, return id |
| `mempalace_conversation_end` | Mark conversation done/failed |
| `mempalace_conversation_get` | Fetch full state |
| `mempalace_conversation_list` | List by status / date |
| `mempalace_conversation_append_turn` | Append one turn |
| `mempalace_conversation_start_segment` | Begin provider segment |
| `mempalace_conversation_end_segment` | End segment with reason |
| `mempalace_conversation_working_memory_get` / `_set` | Read/write typed working memory |
| `mempalace_conversation_context_bundle_get` / `_set` | Read/write context bundle |
| `mempalace_conversation_events_append` | Add runtime event |
| `mempalace_conversation_events_query` | Query events by kind/time |
| `mempalace_conversation_checkpoint` | Snapshot current state |
| `mempalace_conversation_resume` | Restore from checkpoint |
| `mempalace_conversation_lineage` | Return segment chain |

### D1.3 Fork governance

- Fork named `mempalace-milliways`, upstream tracked.
- Monthly rebase cadence to keep drift bounded.
- All added tools namespaced `mempalace_conversation_*` so they are visibly part of the fork delta.
- Upstream-contribution-back is welcome if the primitive matures; not required.
- Release notes for milliways pin a minimum `mempalace-milliways` version.

## D2: Milliways as a MemPalace client

Today `internal/conversation/` holds typed models and a SQLite-backed store. Under this change, storage moves to MemPalace; the typed models stay as the in-memory representation.

```
┌─────────────────────────────────────────┐
│                Milliways                │
│                                         │
│  ┌────────────┐   ┌──────────────────┐  │
│  │  models    │   │ internal/        │  │
│  │  (structs) │◀──│ substrate/       │  │
│  │            │   │  MemPalace MCP   │  │
│  └────────────┘   │  client          │  │
│                   └─────────┬────────┘  │
└─────────────────────────────┼───────────┘
                              │ MCP (stdio/http)
                              ▼
                  ┌──────────────────────┐
                  │  mempalace-milliways │
                  │  (forked MCP server) │
                  └──────────────────────┘
```

Key design rule: **no business logic in `internal/substrate/`** — it's a thin translation layer. Orchestrator logic lives in `internal/orchestrator/` and operates on the typed in-memory models after a round-trip to the substrate.

Migration: on first run, existing `internal/conversation/` SQLite rows are read once and pushed into the substrate. The local SQLite becomes read-only archive. After one release cycle, it is removed.

## D3: User-initiated switch

### D3.1 TUI commands

| Command | Effect |
|---|---|
| `/switch <kitchen>` | End current segment with reason=`user_switch`, start new segment with the named kitchen, emit `runtime_event` with kind=`switch`, inject continuation payload. |
| `/stick` | Toggle a sticky flag on the active conversation; sommelier's continuous-routing evaluator becomes a no-op until `/stick` is toggled off. |
| `/back` | Reverse the most recent switch if the previous segment is still resumable; otherwise, print a warning and no-op. |
| `/kitchens` | List available kitchens and their current status. |

### D3.2 Headless surface

```
milliways --session <name> --switch-to <kitchen> "continue"
```

Requires an existing session in `paused` or `active` state. Resolves the conversation via session name, performs the switch via the same path as `/switch`, and continues with the given prompt.

### D3.3 Continuation payload on switch

Same builder as `milliways-provider-continuity` D5, sourced from MemPalace. No new prompt structure — the only new input is `switch_reason`:

```
Why you are taking over:
User requested switch from claude to codex to leverage codex's code-editing strength.
```

vs. the existing failover reason:

```
Why you are taking over:
Previous provider claude became exhausted at 2026-04-14 22:00 Europe/Stockholm.
```

## D4: Continuous routing

### D4.1 Evaluation points

The sommelier evaluates routing at:

- Initial dispatch (existing behaviour).
- Each new user turn appended to the conversation.
- Explicit request via `/reroute` (new command).

Not evaluated at:

- Each assistant turn (too noisy).
- Within-segment streaming events.

### D4.2 Stickiness and thresholds

```go
type RoutingDecision struct {
    CurrentKitchen    string
    CandidateKitchen  string
    CurrentScore      float64  // 0..1
    CandidateScore    float64  // 0..1
    Tier              string   // keyword, pantry, learned, forced
    HardSignal        bool     // explicit phrase like "search the web"
}

func (d RoutingDecision) ShouldSwitch(cfg RoutingConfig) bool {
    if cfg.Sticky { return false }
    if d.HardSignal { return true }
    return (d.CandidateScore - d.CurrentScore) >= cfg.StickinessDelta
}
```

Default config:

- `StickinessDelta = 0.30` (candidate must be 30% more confident).
- Sticky mode default: **off**, but users who dislike auto-switching can turn it on with `/stick`.
- A session-level config option `routing.sticky = true` in `carte.yaml` makes sticky mode the default for that user.

### D4.3 Visibility

Every auto-switch emits a TUI system line:

```
[milliways] switched claude → gemini — task mentioned "search the web" (hard signal). /back to reverse, /stick to disable.
```

### D4.4 Future local-model router slot

The sommelier exposes an interface:

```go
type Router interface {
    Route(ctx context.Context, convo *Conversation, turn Turn) (RoutingDecision, error)
}
```

Current implementation composes `KeywordRouter`, `PantryRouter`, `LearnedRouter`. A future `LocalModelRouter` (tiny classifier) plugs in as a fourth tier without interface changes. Out of scope here; the slot is the only thing we ship.

## D5: Smoke harness in CI

### D5.1 Layout

```
testdata/smoke/
├── bin/
│   ├── fake-claude-normal           # one turn, exit 0
│   ├── fake-claude-exhausted-text   # emits "You've hit your limit · resets 10pm (Europe/Stockholm)"
│   ├── fake-claude-exhausted-struct # emits structured rate_limit_event
│   ├── fake-claude-crash            # segfault mid-stream
│   ├── fake-claude-hang             # sleeps forever
│   ├── fake-claude-malformed        # emits invalid JSON
│   ├── fake-codex-normal            # accepts continuation, emits one item
│   ├── fake-codex-refuses           # exits non-zero on start (continuity broken)
│   └── ... per-kitchen equivalents
├── config/
│   └── carte.yaml
├── scenarios/
│   ├── normal.sh                    # asserts one block, one segment, exit 0
│   ├── exhaustion-text.sh           # asserts switch, second segment succeeds
│   ├── exhaustion-struct.sh
│   ├── crash.sh                     # asserts block marked failed, user notified
│   ├── hang.sh                      # asserts timeout, block marked failed
│   ├── user-switch.sh               # explicit /switch mid-block
│   └── continuous-route.sh          # hard signal triggers auto-switch
└── README.md
```

### D5.2 Makefile target

```makefile
smoke:
    @scripts/smoke.sh testdata/smoke/scenarios/*.sh
```

### D5.3 CI integration

Add `make smoke` to the existing CI workflow after `go test`. Failure blocks merge. Smoke runs against the built `milliways` binary from the same CI job — same binary that ships.

## D6: Migration path

1. Ship `mempalace-milliways` v0.1 with the conversation primitive.
2. Ship milliways with `internal/substrate/` and a `--use-legacy-conversation` escape hatch (default: new substrate).
3. One release later: remove the escape hatch, delete the legacy SQLite tables.
4. Existing users on upgrade: milliways auto-migrates in-flight conversations once, then logs completion.

## D7: Out of scope — collaborative TUI

Explicitly not designed here. The substrate *enables* collab (two milliways instances can point at the same MemPalace drawer), but the actual collab UX — cursor presence, input-buffer OT/CRDT, session join/leave — is a separate project. Anticipated name: `milliways-collab` or similar. It depends on this change but ships independently.

Read-only co-presence (second instance reads the live conversation) is a free side effect of the substrate and can be demonstrated as part of this change's verification.

## D8: Risks and open questions

| Risk | Mitigation |
|---|---|
| MemPalace fork drifts from upstream and becomes unmaintainable | Monthly rebase cadence; add-only changes; conversation primitive kept in its own module for easy porting back. |
| Continuous routing feels annoying | High default stickiness; `/stick` mode easy to invoke; hard signals only, no speculative switches. |
| Switch mid-task loses provider-native optimisation (e.g. claude's cache) | Continuation payload bootstraps; native resume retained when same provider recovers. |
| Smoke harness is flaky | Fake kitchens are deterministic bash scripts; scenarios assert on structured output, not timing. |
| MCP round-trip latency for every turn write | Batch writes within a segment; flush on segment end and checkpoint. Benchmark before shipping. |

Open questions to resolve during implementation:

- Does MemPalace's current SQLite backend scale to the event-log write rate? Measure in KP-1.
- What's the right working-memory-summary budget to keep continuation payloads bounded? Start with 2000 tokens, tune from real runs.
- Should `/back` restore the exact conversation state or just re-switch forward? Start with re-switch; revisit if confusing.
