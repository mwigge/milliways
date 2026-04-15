# Tasks — milliways-provider-continuity

## Service 1 — Conversation Core (6 SP)

### Course PC-1: Canonical conversation model [2 SP]

- [x] PC-1.1 Create `internal/conversation/model.go`: `Conversation`, `Turn`, `MemoryState`, `ContextBundle`, `ProviderSegment`, status enums
- [x] PC-1.2 Create helper methods: `StartSegment()`, `EndSegment()`, `AppendTurn()`, `UpdateMemory()`, `RecordContext()`
- [x] PC-1.3 Define `ConversationCheckpoint` serializable structure
- [x] PC-1.4 Unit tests: conversation creation, segment lifecycle, transcript append, checkpoint round-trip

### Course PC-2: Provider capability model [1 SP]

- [x] PC-2.1 Extend adapter layer with `Capabilities()` or equivalent metadata for native resume, interactive send, structured events, exhaustion detection
- [x] PC-2.2 Populate capability metadata for `claude`, `codex`, `gemini`, `opencode`, generic fallback
- [x] PC-2.3 Unit tests: capability matrix per adapter

### Course PC-3: Continuation prompt builder [1.5 SP]

- [x] PC-3.1 Create `internal/conversation/continue.go`: build continuation payload from conversation state
- [x] PC-3.2 Include original goal, recent transcript, working memory, active specs, CodeGraph context, MemPalace recall, open actions, failover reason
- [x] PC-3.3 Add transcript windowing and truncation strategy so payload stays bounded
- [x] PC-3.4 Unit tests: continuation prompt contains required sections, includes provider-switch reason, preserves recent context

### Course PC-4: Context bundle assembly [1.5 SP]

- [x] PC-4.1 Reuse pantry CodeGraph client to fetch task-specific code context for active conversation
- [x] PC-4.2 Reuse MemPalace client to fetch semantic memory relevant to the active conversation
- [x] PC-4.3 Define merge rules for transcript context + pantry context + spec context
- [x] PC-4.4 Unit tests: context bundle assembly with missing pantry dependencies degrades gracefully

- [x] 🍋 **Palate Cleanser 1** — Milliways can build and persist a canonical conversation independent of any single provider.

### Course PC-4b: Typed memory model [2 SP]

- [x] PC-4b.1 Extend conversation memory model with explicit working-memory fields such as `NextAction`
- [x] PC-4b.2 Define typed memory categories: working, episodic, semantic, procedural
- [x] PC-4b.3 Document which store owns which memory type
- [x] PC-4b.4 Unit tests: typed memory survives failover and resume without backend coupling

### Course PC-4c: Memory ingestion policy [2 SP]

- [x] PC-4c.1 Introduce a `MemoryCandidate` and `MemoryDecision` model
- [x] PC-4c.2 Add provenance, freshness, and dedupe rules before long-lived memory writes
- [x] PC-4c.3 Prevent arbitrary provider output from auto-promoting into semantic memory
- [x] PC-4c.4 Unit tests: trusted facts accepted, noisy or duplicate facts rejected

### Course PC-4d: Memory retrieval policy [1.5 SP]

- [x] PC-4d.1 Define failover retrieval order: working -> episodic -> procedural -> semantic -> repo context
- [x] PC-4d.2 Keep continuation payload bounded by retrieving the smallest sufficient context
- [x] PC-4d.3 Emit runtime events for memory retrieval decisions
- [x] PC-4d.4 Unit tests: retrieval degrades gracefully when MemPalace or CodeGraph is unavailable

---

## Service 2 — Observability Spine (4 SP)

### Course PC-5: Runtime event model [1.5 SP]

- [x] PC-5.1 Create `internal/observability/events.go`: `RuntimeEvent` model and event kinds
- [x] PC-5.2 Emit structured events for routing, provider output, tool use, failover, checkpointing, context fetch, user input, and jobs
- [x] PC-5.3 Define an in-memory event sink for TUI consumption and a persisted sink for replay/audit
- [x] PC-5.4 Unit tests: event emission for core runtime transitions

### Course PC-6: Process-map and jobs integration [1 SP]

- [x] PC-6.1 Refactor process-map state to consume structured runtime events
- [x] PC-6.2 Ensure ongoing jobs/subagent panels can read from the same event stream
- [x] PC-6.3 Unit tests: process map reflects routing/context/failover events from the shared stream

### Course PC-7: Event persistence [1.5 SP]

- [x] PC-7.1 Add persisted runtime-event storage or append-only event log linked to conversation IDs
- [x] PC-7.2 Add replay helper for rebuilding conversation state from runtime events plus checkpoints
- [x] PC-7.3 Unit tests: persisted events can reconstruct a minimal conversation timeline

- [x] 🍋 **Palate Cleanser 2** — Milliways has a transparent structured event spine that can drive UI, replay, and continuity.

---

## Service 3 — Exhaustion Detection (4 SP)

### Course PC-8: Structured exhaustion signals [1 SP]

- [x] PC-8.1 Extend `RateLimitInfo` with `IsExhaustion`, `RawText`, and `DetectionKind`
- [x] PC-8.2 Update structured adapters to mark true exhaustion explicitly
- [x] PC-8.3 Unit tests: structured rate-limit events map to normalized exhaustion signals

### Course PC-9: Plain-text exhaustion detection [2 SP]

- [x] PC-9.1 Add text detectors for Claude limit output such as `You've hit your limit · resets 10pm (Europe/Stockholm)`
- [x] PC-9.2 Parse absolute reset timestamps from text with timezone handling
- [x] PC-9.3 Add stdout/stderr text detectors for Codex, Gemini, OpenCode where needed
- [x] PC-9.4 Emit normalized exhaustion events from adapters when text detection matches
- [x] PC-9.5 Unit tests: parse reset strings with timezone labels, ignore unrelated text, handle missing timezone safely

### Course PC-10: Adapter stderr/stdout plumbing [1 SP]

- [x] PC-10.1 Stop discarding provider stderr that may contain exhaustion text
- [x] PC-10.2 Route exhaustion-relevant stderr/stdout through adapter detectors before logging
- [x] PC-10.3 Unit tests: detected exhaustion on stderr produces adapter event without breaking normal streaming

- [x] 🍋 **Palate Cleanser 3** — A real provider exhaustion message is detected and converted into a normalized failover trigger.

---

## Service 4 — Orchestrated Failover (8 SP)

### Course PC-11: Orchestrator foundation [2 SP]

- [x] PC-11.1 Create `internal/orchestrator/orchestrator.go` with long-lived conversation runner
- [x] PC-11.2 Replace one-shot adapter execution with segment-based execution loop
- [x] PC-11.3 Define policy for eligible next providers and provider exclusion after exhaustion
- [x] PC-11.4 Unit tests: normal single-provider completion, exhausted provider triggers re-route

### Course PC-12: Same-block continuation [2 SP]

- [x] PC-12.1 Update TUI dispatch path to run blocks through orchestrator instead of direct adapter execution
- [x] PC-12.2 Keep the same block alive while swapping active provider segment
- [x] PC-12.3 Emit system lines for exhaustion and continuation events
- [x] PC-12.4 Unit tests: one block receives events from multiple providers in sequence

### Course PC-13: Provider-native resume optimization [1 SP]

- [x] PC-13.1 Wire stored native session IDs back into adapter creation when same-provider resume is possible
- [x] PC-13.2 Fall back to continuation payload when switching providers or when native resume is unavailable
- [x] PC-13.3 Unit tests: same-provider resume path and cross-provider reconstruction path

### Course PC-14: Headless failover [1 SP]

- [x] PC-14.1 Route headless `dispatch()` through orchestrator
- [x] PC-14.2 Print failover notices to stderr in `--verbose` mode
- [x] PC-14.3 Preserve final stdout output as one logical task even across provider segments

### Course PC-15: Sommelier continuation routing [2 SP]

- [x] PC-15.1 Add continuation-aware routing API that can exclude exhausted provider(s)
- [x] PC-15.2 Prefer providers with better continuity capabilities when continuing an in-flight conversation
- [x] PC-15.3 Preserve forced-provider semantics where possible, but degrade safely when forced provider is exhausted
- [x] PC-15.4 Unit tests: exclusion rules, fallback ordering, forced provider exhausted

- [x] 🍋 **Palate Cleanser 4** — A task starts in `claude`, hits a limit, and continues in `codex` in the same block without manual intervention.

---

## Service 5 — Persistence, Ledger, and UI (6 SP)

### Course PC-16: Conversation persistence [2 SP]

- [x] PC-16.1 Extend TUI session persistence to store canonical conversation state, not only rendered block lines
- [x] PC-16.2 Add resume support that restores provider lineage, memory, and transcript
- [x] PC-16.3 Unit tests: save/load with multiple provider segments

### Course PC-17: Ledger lineage [1.5 SP]

- [x] PC-17.1 Extend pantry ledger entries with `conversation_id`, `segment_id`, `segment_index`, `end_reason`
- [x] PC-17.2 Write one ledger entry per provider segment plus conversation linkage
- [x] PC-17.3 Add queries/report helpers for failover chains
- [x] PC-17.4 Unit tests: multi-segment conversation writes correct lineage

### Course PC-18: Block and process-map continuity indicators [1.5 SP]

- [x] PC-18.1 Add provider lineage and active segment indicator to blocks
- [x] PC-18.2 Show continuity events in process map and/or system lines
- [x] PC-18.3 Keep existing ongoing-tasks and jobs layout intact
- [x] PC-18.4 Render tests: block shows `claude -> codex` continuity without fragmenting the task into separate blocks

### Course PC-19: Report prioritization cleanup [1 SP]

- [x] PC-19.1 Update reporting so tiered stats do not imply continuity support that does not exist
- [x] PC-19.2 Add failover/continuity visibility to reports or summaries
- [x] PC-19.3 Reword existing docs/specs where they still claim "next dispatch only" failover

- [x] 🍋 **Palate Cleanser 5** — Conversation state survives provider switching, restart, and session resume.

---

## Service 6 — Verification (3 SP)

### Course PC-20: End-to-end integration [2 SP]

- [x] PC-20.1 Integration test: `claude` emits exhaustion text, orchestrator switches to `codex`, block remains continuous
- [x] PC-20.2 Integration test: same-provider native resume reuses stored session ID when provider recovers
- [x] PC-20.3 Integration test: persisted session reload restores multi-provider conversation and can continue again
- [x] PC-20.4 Integration test: runtime events replay into a coherent conversation timeline
- [x] PC-20.5 `go test ./...` passes
- [x] PC-20.6 `go build ./...` passes

### Course PC-21: Manual verification [1 SP]

- [ ] PC-21.1 Manual smoke: start a long task in TUI, simulate Claude exhaustion, verify same block continues in Codex
- [ ] PC-21.2 Manual smoke: verify transcript, history, specs, and context remain visible after failover
- [ ] PC-21.3 Manual smoke: verify ongoing tasks/jobs panels remain intact
- [ ] PC-21.4 Manual smoke: verify process map transparently shows context fetch, checkpoint, and failover events
- [ ] PC-21.5 Manual smoke: verify resumed session after restart preserves provider lineage and can continue

- [ ] 🍽️ **Grand Service** — Milliways owns the conversation. Providers can fail, but the work keeps moving.
