# Tasks — milliways-kitchen-parity

## Service 1 — MemPalace Conversation Primitive (Forked) (8 SP)

### Course KP-1: Fork setup [1 SP]

- [ ] KP-1.1 Fork MemPalace repo to `mempalace-milliways` (or chosen name), configure upstream tracking
- [ ] KP-1.2 Establish rebase cadence doc (target: monthly); add a `FORK.md` explaining delta scope and merge rules
- [ ] KP-1.3 Pin minimum milliways-side version in `go.mod` or install docs
- [ ] KP-1.4 CI on the fork: run MemPalace's own test suite unmodified to catch upstream regressions

### Course KP-2: Conversation schema & storage [2 SP]

- [ ] KP-2.1 Design and add `conversation`, `segment`, `turn`, `runtime_event`, `checkpoint` tables
- [ ] KP-2.2 Add indexes: `(conversation_id, ordinal)` on turns, `(conversation_id, at)` on events
- [ ] KP-2.3 Migrations: add-only, no breaking change to existing drawers/rooms/KG
- [ ] KP-2.4 Unit tests: schema round-trip, indexes exist, existing MemPalace tests still pass

### Course KP-3: Conversation CRUD MCP tools [1.5 SP]

- [ ] KP-3.1 Implement `mempalace_conversation_start` / `_end` / `_get` / `_list`
- [ ] KP-3.2 Implement `mempalace_conversation_append_turn`
- [ ] KP-3.3 Unit tests: tool invocation, argument validation, error cases

### Course KP-4: Segments & lineage [1.5 SP]

- [ ] KP-4.1 Implement `mempalace_conversation_start_segment` / `_end_segment`
- [ ] KP-4.2 Implement `mempalace_conversation_lineage` (returns chain of segments in order)
- [ ] KP-4.3 Unit tests: segment ordering, lineage correctness with multiple switches

### Course KP-5: Working memory, context bundle, events, checkpoints [2 SP]

- [ ] KP-5.1 Implement `mempalace_conversation_working_memory_get` / `_set`
- [ ] KP-5.2 Implement `mempalace_conversation_context_bundle_get` / `_set`
- [ ] KP-5.3 Implement `mempalace_conversation_events_append` / `_query`
- [ ] KP-5.4 Implement `mempalace_conversation_checkpoint` / `_resume`
- [ ] KP-5.5 Unit tests: typed memory fields survive round-trip; event query by kind+time window; checkpoint/resume restores full state

- [ ] 🍋 **Palate Cleanser 1** — The forked MemPalace can hold a live conversation with segments, typed memory, events, and checkpoints. Existing MemPalace features continue to pass their own tests.

---

## Service 2 — Milliways as MemPalace Client (6 SP)

### Course KP-6: Substrate client [1.5 SP]

- [ ] KP-6.1 Create `internal/substrate/` with a typed client wrapping MemPalace MCP calls
- [ ] KP-6.2 Keep `internal/substrate/` pure translation — no business logic
- [ ] KP-6.3 Connection handling: startup check, reconnect, timeout, error surfacing
- [ ] KP-6.4 Unit tests with a fake MCP server; integration test against the real forked mempalace binary

### Course KP-7: Read path migration [1.5 SP]

- [ ] KP-7.1 Route all orchestrator reads of conversation state through `internal/substrate/`
- [ ] KP-7.2 Preserve in-memory caching for the active conversation to avoid round-trips on every read
- [ ] KP-7.3 Unit tests: read paths hit the substrate, cache invalidation on writes

### Course KP-8: Write path migration [1.5 SP]

- [ ] KP-8.1 Route all orchestrator writes through `internal/substrate/`
- [ ] KP-8.2 Batch turn appends within a segment; flush on segment end and on checkpoint
- [ ] KP-8.3 Unit tests: writes reach substrate, batching does not lose events on crash/exit

### Course KP-9: Legacy SQLite retirement [1.5 SP]

- [ ] KP-9.1 Add `--use-legacy-conversation` flag defaulting to off (new substrate)
- [ ] KP-9.2 On startup with new substrate and legacy data present, copy legacy conversations into MemPalace once
- [ ] KP-9.3 Mark legacy DB read-only; emit a one-time log message on migration completion
- [ ] KP-9.4 Unit tests: migration copies all conversations; second run is a no-op

- [ ] 🍋 **Palate Cleanser 2** — Milliways orchestrator reads and writes conversation state through MemPalace. Legacy SQLite storage is migrated and no longer written.

---

## Service 3 — User-Initiated Switch (5 SP)

### Course KP-10: `/switch` command [1.5 SP]

- [ ] KP-10.1 Parse `/switch <kitchen>` in TUI input handler
- [ ] KP-10.2 Validate kitchen name against available kitchens; reject with helpful error if unavailable
- [ ] KP-10.3 End current segment with `end_reason=user_switch`; start new segment with named kitchen
- [ ] KP-10.4 Inject continuation payload with `switch_reason="user requested"`
- [ ] KP-10.5 Emit `runtime_event` with kind=`switch`, payload includes from/to kitchens and reason
- [ ] KP-10.6 Unit + TUI render tests

### Course KP-11: `/stick`, `/back`, `/kitchens` [1.5 SP]

- [ ] KP-11.1 `/stick` toggles sticky mode on the active conversation (stored in working memory or session config)
- [ ] KP-11.2 `/back` finds the most recent switch event and reverses it via a fresh `/switch` to the previous kitchen
- [ ] KP-11.3 `/kitchens` prints kitchen list with availability and current status
- [ ] KP-11.4 Unit + TUI render tests

### Course KP-12: Headless `--switch-to` [1 SP]

- [ ] KP-12.1 Add `--switch-to <kitchen>` flag to milliways CLI
- [ ] KP-12.2 Require `--session <name>`; resolve conversation, perform switch, continue with given prompt
- [ ] KP-12.3 Print switch notice to stderr in `--verbose` mode
- [ ] KP-12.4 Integration test: session in `paused` state switched headlessly, continues

### Course KP-13: System lines and reason visibility [1 SP]

- [ ] KP-13.1 Every switch — user, auto, failover — emits a TUI system line with reason and reversal hint
- [ ] KP-13.2 Process map shows switch events inline with other runtime events
- [ ] KP-13.3 Render tests: system lines appear in correct order; multi-switch transcripts render coherently

- [ ] 🍋 **Palate Cleanser 3** — A user in the TUI can type `/switch codex` mid-block and the conversation continues in codex with full context. `/stick`, `/back`, `/kitchens` work. Headless `--switch-to` works for paused sessions.

---

## Service 4 — Continuous Routing (4 SP)

### Course KP-14: Turn-boundary evaluator [1.5 SP]

- [ ] KP-14.1 Add a sommelier evaluation point after every user turn is appended
- [ ] KP-14.2 Refactor existing sommelier into a `Router` interface composed of keyword, pantry, learned tiers
- [ ] KP-14.3 Add a `LocalModelRouter` interface slot (no implementation) to anticipate the future tier
- [ ] KP-14.4 Unit tests: evaluator called at correct points; composes tiers correctly

### Course KP-15: Stickiness logic [1 SP]

- [ ] KP-15.1 Implement `RoutingDecision.ShouldSwitch` with stickiness delta and hard-signal paths
- [ ] KP-15.2 Default `stickiness_delta = 0.30`; make configurable via `carte.yaml`
- [ ] KP-15.3 Sticky-mode flag honoured (from `/stick` or `carte.yaml`)
- [ ] KP-15.4 Hard-signal list: "search the web", "search online", explicit `--kitchen` override — extensible
- [ ] KP-15.5 Unit tests: decision matrix covers sticky, hard-signal, below-threshold, above-threshold

### Course KP-16: Reversal via `/back` [0.5 SP]

- [ ] KP-16.1 Auto-switches are reversible via `/back` exactly like user switches
- [ ] KP-16.2 Visible system line includes the reversal hint
- [ ] KP-16.3 Integration test: auto-switch triggered, `/back` reverses, conversation continues in original kitchen

### Course KP-17: Visible reasons [1 SP]

- [ ] KP-17.1 Every auto-switch emits a TUI system line naming the trigger (hard signal, score delta, tier)
- [ ] KP-17.2 Process map shows the decision payload for replay
- [ ] KP-17.3 Render tests

- [ ] 🍋 **Palate Cleanser 4** — The sommelier re-routes only on high-confidence signals, every switch is visible with its reason, and the user can always override with `/stick` or `/back`.

---

## Service 5 — Smoke Harness & CI (3 SP)

### Course KP-18: Promote smoke rig [1 SP]

- [ ] KP-18.1 Move `/tmp/mw-smoke/` content into `testdata/smoke/` as version-controlled fixtures
- [ ] KP-18.2 Rewrite fake kitchens as deterministic bash/Go binaries; no timing assumptions in assertions
- [ ] KP-18.3 Carte.yaml for smoke points at the fixture binaries, not system paths

### Course KP-19: Scenario suite [1.5 SP]

- [ ] KP-19.1 Scenarios: normal, exhaustion-text, exhaustion-struct, crash, hang, malformed, user-switch, continuous-route, native-resume
- [ ] KP-19.2 Each scenario asserts on structured output (ledger JSON, conversation state, event log), not TUI rendering
- [ ] KP-19.3 `scripts/smoke.sh` runs all scenarios and returns non-zero on any failure
- [ ] KP-19.4 `make smoke` target in Makefile

### Course KP-20: CI integration [0.5 SP]

- [ ] KP-20.1 Add `make smoke` as a required CI step after `go test`
- [ ] KP-20.2 CI runs against the same built binary it tests
- [ ] KP-20.3 CI failure blocks merge

- [ ] 🍋 **Palate Cleanser 5** — The class of bug that blocked PC-21.1 (missing allowlist entry) would now fail CI, not manual verification.

---

## Service 6 — Verification (3 SP)

### Course KP-21: End-to-end integration [1.5 SP]

- [ ] KP-21.1 E2E: start in claude, `/switch codex` mid-block, transcript + context preserved, codex continues
- [ ] KP-21.2 E2E: restart milliways mid-conversation, resume from MemPalace, continue in same segment
- [ ] KP-21.3 E2E: second milliways process reads live conversation from same MemPalace drawer (read-only co-presence)
- [ ] KP-21.4 E2E: auto-switch triggered by hard signal, `/back` reverses, conversation continues
- [ ] KP-21.5 E2E: continuous-routing respects `/stick` — no auto-switch after `/stick` enabled
- [ ] KP-21.6 `go test ./...` passes; `make smoke` passes

### Course KP-22: Manual verification [1 SP]

- [ ] KP-22.1 Manual TUI: `/switch`, `/stick`, `/back`, `/kitchens` all behave as described
- [ ] KP-22.2 Manual TUI: auto-switch on realistic prompt like "now search the web for X"
- [ ] KP-22.3 Manual headless: `milliways --session foo --switch-to codex "continue"` works against a real paused session
- [ ] KP-22.4 Manual: upgrade path from legacy conversation SQLite to MemPalace substrate works on a pre-existing install

### Course KP-23: Documentation [0.5 SP]

- [ ] KP-23.1 Update `README.md` with switch commands, sticky mode, substrate dependency
- [ ] KP-23.2 Add `FORK.md` in `mempalace-milliways` documenting delta and rebase process
- [ ] KP-23.3 Add release notes describing the upgrade path and MemPalace dependency

- [ ] 🍽️ **Grand Service** — Any kitchen, any time, same memory. The conversation is MemPalace's, the orchestrator is Milliways', and the user is always in control of which kitchen cooks.
