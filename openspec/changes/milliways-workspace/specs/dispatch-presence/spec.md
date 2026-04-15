# Spec: dispatch-presence

## Overview

Every dispatch MUST provide visible feedback at each pipeline stage. The user MUST always know what the system is doing, which kitchen is handling the task, and why that kitchen was chosen.

## Requirements

### State machine

- The TUI MUST track dispatch state as an explicit 9-state enum: Idle, Routing, Routed, Streaming, Done, Failed, Cancelled, Awaiting, Confirming
- State transitions MUST be driven by Event stream events, not ad-hoc boolean flags
- The process map panel MUST update on every state transition

### Routing feedback — Tier 1

- The process map MUST display the kitchen name as a colored badge as soon as routing completes
- The process map MUST display the routing reason (truncated to panel width)
- The process map MUST display the routing tier (keyword, enriched, learned, forced, fallback)
- The process map MUST display the risk level when available
- Elapsed time MUST be shown and update at least every 100ms during active dispatch

### Pipeline steps — Tier 2

- The process map MUST show a pipeline step list below the routing info when vertical space permits
- Steps: sommelier.route, kitchen.exec, ledger.write, quota.update
- Each step MUST show: icon (✓ done, ● active, · pending), name, duration when complete
- Steps MUST update in real-time as the dispatch progresses

### Dialogue overlays

- When EventQuestion arrives, the TUI MUST enter Awaiting state and show a yellow-bordered overlay input
- When EventConfirm arrives, the TUI MUST enter Confirming state and show an inline `[y/N]` prompt
- When Ctrl+I is pressed during Streaming, the TUI MUST open a context injection overlay
- Overlay input MUST call `adapter.Send()` on submit
- If `adapter.Send()` returns ErrNotInteractive, the TUI MUST log a warning and auto-answer (empty for questions, "n" for confirms)

### Headless compatibility

- In non-TUI mode with `--verbose`, routing decisions MUST be printed to stderr as `[routed] kitchen_name`
- All dialogue states MUST have safe headless defaults (auto-answer, no blocking)
