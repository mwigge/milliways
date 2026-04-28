## 1. Session limit signal — runner layer

- [ ] 1.1 Add `"session.limit_reached"` to the progress event type constants in `dispatch.go`
- [ ] 1.2 Detect limit in `runner_claude.go`: match stderr `context window` / `session limit` patterns, emit `session.limit_reached` event before returning
- [ ] 1.3 Detect limit in `runner_codex.go`: match JSON event types `max_turns` / `context_length_exceeded`, emit `session.limit_reached`
- [ ] 1.4 Detect limit in `runner_minimax.go`: match HTTP 429 + `quota_exceeded` body, emit `session.limit_reached`
- [ ] 1.5 Detect limit in `runner_copilot.go`: match stderr `rate limit` pattern, emit `session.limit_reached`
- [ ] 1.6 Write unit tests for each runner's limit-detection logic using synthetic stderr/event inputs

## 2. Briefing generator

- [ ] 2.1 Create `internal/repl/briefing.go` with `GenerateBriefing(turns []ConversationTurn, cwd string) string`
- [ ] 2.2 Implement task extraction from last user turn initiating work
- [ ] 2.3 Implement progress summary: last 3 assistant turns → bullet points
- [ ] 2.4 Implement decision extraction: sentences containing `decided`, `we will`, `use X instead` heuristics
- [ ] 2.5 Implement next-step extraction: final paragraph of last assistant turn
- [ ] 2.6 Implement `git diff --name-only HEAD` file listing (skip if not a git repo, cap at 20 files)
- [ ] 2.7 Implement 500-token cap with truncation order: decisions first, then progress, preserve task + next step
- [ ] 2.8 Handle zero-turns case: return minimal briefing `[TAKEOVER] No prior context — starting fresh.`
- [ ] 2.9 Write unit tests for briefing generation covering all scenarios from the spec

## 3. `/takeover` command

- [ ] 3.1 Add `handleTakeover(ctx, r, args)` to `commands.go`
- [ ] 3.2 Parse optional runner argument; validate it is registered; reject same-runner and unknown-runner cases per spec
- [ ] 3.3 Call `GenerateBriefing` and prepend result as a synthetic `ConversationTurn{Role: "user"}` to session history
- [ ] 3.4 Execute runner switch (reuse `handleSwitch` internals)
- [ ] 3.5 Print confirmation `[takeover] <from> → <to> — briefing injected`
- [ ] 3.6 Fall through to sommelier if no argument and no ring; rotate ring if ring is active
- [ ] 3.7 Register `"takeover"` in the command map in `commands.go`
- [ ] 3.8 Write integration test for `/takeover` covering all spec scenarios

## 4. MemPalace snapshot on takeover

- [ ] 4.1 Extract MemPalace MCP call into a helper `snapshotToMemPalace(briefing string)` in `repl.go`
- [ ] 4.2 Fire snapshot asynchronously (goroutine) with `handoff/<iso8601>` as drawer key
- [ ] 4.3 Log failure at debug level; do not block the runner switch
- [ ] 4.4 Gate on `MILLIWAYS_MEMPALACE_MCP_CMD` being set (no-op if absent)

## 5. Rotation ring — `/takeover-ring` command

- [ ] 5.1 Add `RingConfig struct { Runners []string; Pos int }` to `PersistedSession` in `session.go`
- [ ] 5.2 Add `ring *RingConfig` field to the `REPL` struct in `repl.go`
- [ ] 5.3 Implement `handleTakeoverRing(ctx, r, args)` in `commands.go`: parse comma-separated runners, validate each, set ring, print confirmation
- [ ] 5.4 Handle `off` / `clear` subcommand to remove ring
- [ ] 5.5 Handle bare `/takeover-ring` to show current ring state
- [ ] 5.6 Persist ring to session on save; restore ring on session load
- [ ] 5.7 Register `"takeover-ring"` in command map
- [ ] 5.8 Write unit tests for ring configuration, validation, and persistence

## 6. Ring rotation logic

- [ ] 6.1 Implement `nextRingRunner(ring *RingConfig, pantry *pantry.Store) (string, error)` — advance position, skip zero-quota runners, wrap at end
- [ ] 6.2 Return error when all ring runners are exhausted
- [ ] 6.3 Track consecutive auto-rotation count per user turn; halt when count exceeds ring length
- [ ] 6.4 On `/takeover` without argument: call `nextRingRunner` when ring is active
- [ ] 6.5 Write unit tests for skip-exhausted, wrap-around, and cap scenarios

## 7. Auto-rotate on session limit

- [ ] 7.1 In REPL dispatch loop, intercept `session.limit_reached` event
- [ ] 7.2 If ring is active: call `nextRingRunner`, generate briefing, re-dispatch original prompt to next runner
- [ ] 7.3 Print `[auto-takeover] <from> session limit — continuing on <to>`
- [ ] 7.4 If no ring: print guidance message per spec (suggest `/takeover-ring`)
- [ ] 7.5 Increment auto-rotate counter per turn; halt on cap with error message per spec
- [ ] 7.6 Write integration test for auto-rotate path

## 8. Status bar ring indicator

- [ ] 8.1 Pass ring position into status bar render path
- [ ] 8.2 Append ` N/M` to runner segment when ring is active (e.g. `●codex 2/3`)
- [ ] 8.3 No suffix when ring is inactive
- [ ] 8.4 Write unit test for status bar rendering with and without ring

## 9. Documentation & README

- [ ] 9.1 Add `/takeover` and `/takeover-ring` to the Commands table in `README.md`
- [ ] 9.2 Add "Session rotation" section to README explaining the ring concept and auto-rotate
- [ ] 9.3 Update `CHANGELOG.md` with v0.4.13 entry for runner takeover feature
