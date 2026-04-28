## 1. Session limit signal — runner layer

- [x] 1.1 Add typed session-limit signaling via `ErrSessionLimit` in `dispatch.go`
- [x] 1.2 Detect limit in `runner_claude.go`: match stderr `context window` / `session limit` patterns, return typed session-limit error
- [x] 1.3 Detect limit in `runner_codex.go`: match JSON event types `max_turns` / `context_length_exceeded`, return typed session-limit error
- [x] 1.4 Detect limit in `runner_minimax.go`: match HTTP 429 + `quota_exceeded` body, return typed session-limit error
- [x] 1.5 Detect limit in `runner_copilot.go`: match stderr `rate limit` pattern, return typed session-limit error
- [x] 1.6 Write unit tests for each runner's limit-detection logic using synthetic stderr/event inputs

## 2. TTY transcript writer

- [x] 2.1 Create `internal/repl/transcript.go` with `TranscriptWriter` — an `io.Writer` that strips ANSI escape sequences via a state machine and appends plain text to a `.log` sidecar file
- [x] 2.2 Implement ANSI state machine: consume ESC sequences, CSI sequences (`\x1b[...m`), and OSC sequences; pass all other bytes through
- [x] 2.3 Open a stable sidecar file path in the session store: `current-<cwd-hash>.log`
- [x] 2.4 Wire `TranscriptWriter` into the terminal output path in `repl.go` at session initialisation
- [x] 2.5 On session prune (keep-5 rule), delete the corresponding `.log` file alongside the `.json` file
- [x] 2.6 Delete `.log` files older than 7 days on startup alongside session cleanup
- [x] 2.7 Write unit tests for ANSI stripping: verify escape codes are removed, plain text passes through intact

## 3. Briefing generator

- [x] 3.1 Create `internal/repl/briefing.go` with `GenerateBriefing(logPath string, turns []ConversationTurn, cwd string) string`
- [x] 3.2 Prefer transcript log as source when file exists and is readable; fall back to `ConversationTurn` ring if absent
- [x] 3.3 Implement task extraction: last user prompt that initiated a work block (from transcript or turns)
- [x] 3.4 Implement progress summary: last 3 assistant responses → bullet points
- [x] 3.5 Implement decision extraction: sentences containing `decided`, `we will`, `use X instead` heuristics
- [x] 3.6 Implement next-step extraction: final paragraph of last assistant response
- [x] 3.7 Implement `git diff --name-only HEAD` file listing (skip if not a git repo, cap at 20 files)
- [x] 3.8 Implement 500-token cap with truncation order: decisions first, then progress, preserve task + next step
- [x] 3.9 Handle zero-content case: return minimal briefing `[TAKEOVER] No prior context — starting fresh.`
- [x] 3.10 Write unit tests for briefing generation: transcript path, fallback path, truncation, zero-content

## 4. `/takeover` command

- [x] 4.1 Add `handleTakeover(ctx, r, args)` to `commands.go`
- [x] 4.2 Parse optional runner argument; validate it is registered; reject same-runner and unknown-runner cases per spec
- [x] 4.3 Call `GenerateBriefing` (passing transcript log path + turns + cwd) and prepend result as a synthetic `ConversationTurn{Role: "user"}` to session history
- [x] 4.4 Execute runner switch (reuse `handleSwitch` internals)
- [x] 4.5 Print confirmation `[takeover] <from> → <to> — briefing injected`
- [x] 4.6 Rotate ring if ring is active; otherwise require explicit `/takeover <runner>`
- [x] 4.7 Register `"takeover"` in the command map in `commands.go`
- [x] 4.8 Write integration test for `/takeover` covering all spec scenarios

## 5. MemPalace snapshot on takeover

- [x] 5.1 Extract MemPalace MCP call into `snapshotToMemPalaceAsync(briefing string)`
- [x] 5.2 Fire snapshot asynchronously (goroutine) with `handoff/<iso8601>` as drawer key
- [x] 5.3 Log failure at debug level; do not block the runner switch
- [x] 5.4 Gate on `MILLIWAYS_MEMPALACE_MCP_CMD` being set (no-op if absent)

## 6. Rotation ring — `/takeover-ring` command

- [x] 6.1 Add `RingConfig struct { Runners []string; Pos int }` to `PersistedSession` in `session.go`
- [x] 6.2 Add `ring *RingConfig` field to the `REPL` struct in `repl.go`
- [x] 6.3 Implement `handleTakeoverRing(ctx, r, args)` in `commands.go`: parse comma-separated runners, validate each, set ring, print confirmation
- [x] 6.4 Handle `off` / `clear` subcommand to remove ring
- [x] 6.5 Handle bare `/takeover-ring` to show current ring state
- [x] 6.6 Persist ring to session on save; restore ring on session load
- [x] 6.7 Register `"takeover-ring"` in command map
- [x] 6.8 Write unit tests for ring configuration, validation, and persistence

## 7. Ring rotation logic

- [x] 7.1 Implement `nextRingRunner(ring *RingConfig, pantry *pantry.Store) (string, error)` — advance position, skip zero-quota runners, wrap at end
- [x] 7.2 Return error when all ring runners are exhausted
- [x] 7.3 Track consecutive auto-rotation count per user turn; halt when count exceeds ring length
- [x] 7.4 On `/takeover` without argument: call `nextRingRunner` when ring is active
- [x] 7.5 Write unit tests for skip-exhausted, wrap-around, and cap scenarios

## 8. Auto-rotate on session limit

- [x] 8.1 In terminal dispatch loop, intercept typed session-limit error
- [x] 8.2 If ring is active: call `nextRingRunner`, generate briefing, re-dispatch original prompt to next runner
- [x] 8.3 Print `[auto-takeover] <from> session limit — continuing on <to>`
- [x] 8.4 If no ring: print guidance message per spec (suggest `/takeover-ring`)
- [x] 8.5 Increment auto-rotate counter per turn; halt on cap with error message per spec
- [x] 8.6 Write integration test for auto-rotate path

## 9. Status bar ring indicator

- [x] 9.1 Pass ring position into status bar render path
- [x] 9.2 Append ` N/M` to runner segment when ring is active (e.g. `●codex 2/3`)
- [x] 9.3 No suffix when ring is inactive
- [x] 9.4 Write unit test for status bar rendering with and without ring

## 10. Documentation & README

- [x] 10.1 Add `/takeover` and `/takeover-ring` to the Commands table in `README.md`
- [x] 10.2 Add "Session rotation" section to README explaining the ring concept, TTY transcript, and auto-rotate
- [x] 10.3 Update `CHANGELOG.md` with v0.4.13 entry for runner takeover feature
