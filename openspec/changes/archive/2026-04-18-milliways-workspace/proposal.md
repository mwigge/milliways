# milliways-workspace — From Dispatch Shell to AI Workspace

> The maitre d' doesn't just take your order and disappear into the kitchen.
> They seat you, explain the menu, bring each course with commentary,
> and check back after every dish.

## Why

Milliways today is a dispatch shell: type a prompt, wait in silence, get output. This is the equivalent of a restaurant where the waiter takes your order and vanishes — no confirmation, no "your food is being prepared," no "the chef has a question." Every competing tool (Claude Code, OpenCode, Gemini CLI, Codex) does better at making the user feel present and in control of their session.

To become a primary AI-assisted developer tool — VS Code but in CLI, for AI tools only — milliways needs three things it currently lacks:

1. **Presence**: the TUI must feel alive. Prompt echo, routing feedback, streaming status, kitchen dialogue — the user should never wonder "did it hear me?"

2. **Transparency**: the tiered routing model (multiple CLIs, multiple models, different cost tiers) must be visible and accountable. Who handled it, why, what it cost, was that the right call.

3. **Resilience**: when a kitchen hits its rate limit or quota, milliways must automatically route around it. No manual intervention, no lost prompts, no silent failures.

## What Changes

### Dialogue Adapters

Replace the single `GenericKitchen` line-by-line scanner with kitchen-specific adapters that speak each tool's native structured protocol:

- **ClaudeAdapter**: `--print --verbose --output-format stream-json --input-format stream-json` — full bidirectional JSON events, cost tracking, rate limit detection, session resume
- **GeminiAdapter**: `--prompt --output-format stream-json` — structured events, quota error parsing from stderr
- **CodexAdapter**: `codex exec --json` — JSONL event stream, stdin pipe
- **OpenCodeAdapter**: `opencode run --format json` — JSON events, `--continue` for session resume
- **GenericAdapter**: `bufio.Scanner` line-by-line fallback for aider, goose, and unknown kitchens

All adapters normalize output to a common `Event` channel that the TUI consumes uniformly.

### Session Model

The TUI becomes a continuous session, not a series of independent dispatches:

- Output viewport accumulates sections (one per dispatch) with scrollback — never clears
- Every output line is prefixed with `[kitchen]` and color-coded to the kitchen
- Code blocks get syntax highlighting via chroma
- Raw markdown by default; Ctrl+G toggles glamour-rendered view
- Cross-kitchen summaries aggregate all dispatches in a session

### Dispatch Presence

A 9-state dispatch FSM replaces the current `dispatching bool`:

```
Idle → Routing → Routed → Streaming → Done/Failed/Cancelled
                                    ↕
                              Awaiting/Confirming
```

With prompt echo, routing reasoning in the process map, step-by-step pipeline visibility, and interactive dialogue (question/answer/confirm overlays).

### Quota-Gated Routing

The sommelier checks kitchen quotas before routing. When a kitchen is exhausted:

- The dispatch routes to the next-best kitchen with a clear reason
- The status bar shows `kitchen (resets HH:MM)`
- Rate limit events from structured adapters auto-update the quota store
- Manual `daily_limit` in carte.yaml for kitchens without structured rate info
- Warning at 80% threshold, hard skip at 100%

Failover is **Option C** for the older quota-gated-routing design only. It is superseded by `milliways-provider-continuity`, where provider exhaustion may trigger immediate same-block continuation and the quota store still records the exhausted kitchen with its `resetsAt` timestamp.

### Feedback Loop

Users can rate dispatch outcomes (`Ctrl+F` in TUI, `milliways rate` in CLI) to feed the learned routing model. This closes the loop: dispatch → outcome → feedback → better routing next time.

## Capabilities

### New Capabilities

- `dialogue-adapters`: per-kitchen structured protocol adapters (claude, gemini, codex, opencode) normalizing to a common Event stream, with bidirectional communication support
- `session-model`: continuous scrollback session with kitchen-prefixed color-coded output, code syntax highlighting, and raw/glamour markdown toggle
- `dispatch-presence`: 9-state FSM with prompt echo, routing reasoning display, pipeline step visibility, and kitchen dialogue overlays
- `quota-gated-routing`: sommelier quota gate that skips exhausted kitchens, auto-detects rate limits from structured adapters, and shows quota state in status bar
- `feedback-loop`: explicit good/bad rating per dispatch that feeds the learned routing model

### Modified Capabilities

- `tui-process-map`: shows routing reasoning (tier + why) and pipeline steps, not just kitchen name and status
- `kitchen-exec`: GenericKitchen stays as fallback; structured kitchens use adapter-specific execution paths
- `sommelier-routing`: gains a quota gate check before every candidate kitchen

## Impact

### New Packages

- `internal/kitchen/adapter/` — Event type, Adapter interface, per-kitchen adapter implementations
- `internal/kitchen/dialogue/` — protocol constants, helpers

### Modified Packages

- `internal/tui/` — session model, FSM, overlays, syntax highlighting, glamour toggle, feedback keybinding
- `internal/kitchen/kitchen.go` — Task struct gains adapter-aware fields
- `internal/kitchen/generic.go` — becomes the fallback adapter, gains stdin pipe support
- `internal/sommelier/sommelier.go` — quota gate wrapping every candidate check
- `internal/pantry/quotas.go` — IsExhausted(), ResetsAt(), threshold warning
- `cmd/milliways/main.go` — adapter wiring, session management, feedback subcommand

### Dependencies

- `github.com/alecthomas/chroma/v2` — syntax highlighting for code blocks
- `github.com/charmbracelet/glamour` — markdown rendering (Ctrl+G toggle)

## Supersedes

This change supersedes and replaces:

- `milliways-tui-presence` — absorbed into dispatch-presence and dialogue-adapters
- `milliways-repl-experience` — absorbed into session-model and quota-gated-routing
