## 1. Session limit signal — runner layer

- [ ] 1.1 Add `"session.limit_reached"` to the progress event type constants in `dispatch.go`
- [ ] 1.2 Detect limit in `runner_claude.go`: match stderr `context window` / `session limit` patterns, emit `session.limit_reached` event before returning
- [ ] 1.3 Detect limit in `runner_codex.go`: match JSON event types `max_turns` / `context_length_exceeded`, emit `session.limit_reached`
- [ ] 1.4 Detect limit in `runner_minimax.go`: match HTTP 429 + `quota_exceeded` body, emit `session.limit_reached`
- [ ] 1.5 Detect limit in `runner_copilot.go`: match stderr `rate limit` pattern, emit `session.limit_reached`
- [ ] 1.6 Write unit tests for each runner's limit-detection logic using synthetic stderr/event inputs

## 2. TTY transcript writer

- [ ] 2.1 Create `internal/repl/transcript.go` with `TranscriptWriter` — an `io.Writer` that strips ANSI escape sequences via a state machine and appends plain text to a `.log` sidecar file
- [ ] 2.2 Implement ANSI state machine: consume ESC sequences, CSI sequences (`\x1b[...m`), and OSC sequences; pass all other bytes through
- [ ] 2.3 Open the sidecar file path alongside the session file: `<session-id>.log` next to `<session-id>.json`
- [ ] 2.4 Wire `TranscriptWriter` into the terminal output path in `repl.go` at session initialisation
- [ ] 2.5 On session prune (keep-5 rule), delete the corresponding `.log` file alongside the `.json` file
- [ ] 2.6 Delete `.log` files older than 7 days on startup alongside session cleanup
- [ ] 2.7 Write unit tests for ANSI stripping: verify escape codes are removed, plain text passes through intact

## 3. Briefing generator

- [ ] 3.1 Create `internal/repl/briefing.go` with `GenerateBriefing(logPath string, turns []ConversationTurn, cwd string) string`
- [ ] 3.2 Prefer transcript log as source when file exists and is readable; fall back to `ConversationTurn` ring if absent
- [ ] 3.3 Implement task extraction: last user prompt that initiated a work block (from transcript or turns)
- [ ] 3.4 Implement progress summary: last 3 assistant responses → bullet points
- [ ] 3.5 Implement decision extraction: sentences containing `decided`, `we will`, `use X instead` heuristics
- [ ] 3.6 Implement next-step extraction: final paragraph of last assistant response
- [ ] 3.7 Implement `git diff --name-only HEAD` file listing (skip if not a git repo, cap at 20 files)
- [ ] 3.8 Implement 500-token cap with truncation order: decisions first, then progress, preserve task + next step
- [ ] 3.9 Handle zero-content case: return minimal briefing `[TAKEOVER] No prior context — starting fresh.`
- [ ] 3.10 Write unit tests for briefing generation: transcript path, fallback path, truncation, zero-content

## 4. `/takeover` command

- [ ] 4.1 Add `handleTakeover(ctx, r, args)` to `commands.go`
- [ ] 4.2 Parse optional runner argument; validate it is registered; reject same-runner and unknown-runner cases per spec
- [ ] 4.3 Call `GenerateBriefing` (passing transcript log path + turns + cwd) and prepend result as a synthetic `ConversationTurn{Role: "user"}` to session history
- [ ] 4.4 Execute runner switch (reuse `handleSwitch` internals)
- [ ] 4.5 Print confirmation `[takeover] <from> → <to> — briefing injected`
- [ ] 4.6 Fall through to sommelier if no argument and no ring; rotate ring if ring is active
- [ ] 4.7 Register `"takeover"` in the command map in `commands.go`
- [ ] 4.8 Write integration test for `/takeover` covering all spec scenarios

## 5. MemPalace snapshot on takeover

- [ ] 5.1 Extract MemPalace MCP call into a helper `snapshotToMemPalace(briefing string)` in `repl.go`
- [ ] 5.2 Fire snapshot asynchronously (goroutine) with `handoff/<iso8601>` as drawer key
- [ ] 5.3 Log failure at debug level; do not block the runner switch
- [ ] 5.4 Gate on `MILLIWAYS_MEMPALACE_MCP_CMD` being set (no-op if absent)

## 6. Rotation ring — `/takeover-ring` command

- [ ] 6.1 Add `RingConfig struct { Runners []string; Pos int }` to `PersistedSession` in `session.go`
- [ ] 6.2 Add `ring *RingConfig` field to the `REPL` struct in `repl.go`
- [ ] 6.3 Implement `handleTakeoverRing(ctx, r, args)` in `commands.go`: parse comma-separated runners, validate each, set ring, print confirmation
- [ ] 6.4 Handle `off` / `clear` subcommand to remove ring
- [ ] 6.5 Handle bare `/takeover-ring` to show current ring state
- [ ] 6.6 Persist ring to session on save; restore ring on session load
- [ ] 6.7 Register `"takeover-ring"` in command map
- [ ] 6.8 Write unit tests for ring configuration, validation, and persistence

## 7. Ring rotation logic

- [ ] 7.1 Implement `nextRingRunner(ring *RingConfig, pantry *pantry.Store) (string, error)` — advance position, skip zero-quota runners, wrap at end
- [ ] 7.2 Return error when all ring runners are exhausted
- [ ] 7.3 Track consecutive auto-rotation count per user turn; halt when count exceeds ring length
- [ ] 7.4 On `/takeover` without argument: call `nextRingRunner` when ring is active
- [ ] 7.5 Write unit tests for skip-exhausted, wrap-around, and cap scenarios

## 8. Auto-rotate on session limit

- [ ] 8.1 In REPL dispatch loop, intercept `session.limit_reached` event
- [ ] 8.2 If ring is active: call `nextRingRunner`, generate briefing, re-dispatch original prompt to next runner
- [ ] 8.3 Print `[auto-takeover] <from> session limit — continuing on <to>`
- [ ] 8.4 If no ring: print guidance message per spec (suggest `/takeover-ring`)
- [ ] 8.5 Increment auto-rotate counter per turn; halt on cap with error message per spec
- [ ] 8.6 Write integration test for auto-rotate path

## 9. Status bar ring indicator

- [ ] 9.1 Pass ring position into status bar render path
- [ ] 9.2 Append ` N/M` to runner segment when ring is active (e.g. `●codex 2/3`)
- [ ] 9.3 No suffix when ring is inactive
- [ ] 9.4 Write unit test for status bar rendering with and without ring

## 10. Documentation & README

- [ ] 10.1 Add `/takeover` and `/takeover-ring` to the Commands table in `README.md`
- [ ] 10.2 Add "Session rotation" section to README explaining the ring concept, TTY transcript, and auto-rotate
- [ ] 10.3 Update `CHANGELOG.md` with v0.4.13 entry for runner takeover feature
