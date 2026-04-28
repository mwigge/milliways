## Why

AI runners have hard session limits (token budgets, daily quotas, rate limits). When a runner hits its limit mid-task, the user is blocked — they must manually copy context, switch runners, and re-orient the new one. The same problem appears when deliberately handing off between runners (e.g., Claude plans, Codex implements): context must be rebuilt by hand. A structured takeover mechanism eliminates both failure modes: the user gets an explicit `/takeover` command for intentional handoffs, and the system can auto-rotate through a priority ring when any runner hits a limit.

## What Changes

- Add `/takeover [runner]` command — generates a structured briefing from the current session (task, decisions, file changes, next steps), injects it as a high-priority context block, and switches to the target runner
- Add `/takeover-ring <r1,r2,...>` command — configures an ordered rotation ring; milliways cycles to the next runner automatically when the active one hits a session limit or quota
- Add session-limit detection — runners signal `SessionLimitReached` in their event stream; milliways intercepts this before surfacing an error and triggers a takeover if a ring is configured
- MemPalace snapshot on takeover — key facts from the ending session are written to MemPalace before the switch so the new runner can query them
- Status bar shows ring position — `[●claude 1/5]` when a rotation ring is active

## Capabilities

### New Capabilities

- `takeover-command`: `/takeover [runner]` — on-demand structured handoff between runners with context briefing injection
- `rotation-ring`: automatic runner rotation when session limits are hit; configurable priority order cycling 1→2→3→N→1
- `session-limit-detection`: runners surface a `SessionLimitReached` signal; milliways intercepts and triggers auto-takeover when a ring is active

### Modified Capabilities

- none

## Impact

- `internal/repl/commands.go` — new `handleTakeover`, `handleTakeoverRing` commands
- `internal/repl/dispatch.go` — `ConversationTurn` ring buffer feeds the briefing generator
- `internal/repl/runner_claude.go`, `runner_codex.go`, `runner_minimax.go` — each must emit `SessionLimitReached` event type
- `internal/repl/repl.go` — ring state, auto-rotate logic on dispatch error/signal
- `internal/session/session.go` — `PersistedSession` may carry ring config
- MemPalace MCP integration — snapshot call on takeover
- Status bar — ring position indicator
