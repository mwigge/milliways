# Spec: dispatch-presence

## Overview

Every dispatch in the TUI MUST provide visible feedback at each pipeline stage so the user always knows their prompt was received and what the system is doing.

## Requirements

### Prompt echo

- WHEN the user submits a prompt, the prompt text MUST appear in the output viewport as the first line, prefixed with `▶ `, before any kitchen output arrives
- The prompt echo line MUST be styled in muted/dim colour to distinguish it from kitchen output
- A separator line MUST follow the echo line

### Pipeline state

- The TUI MUST track dispatch state as an explicit enum: `Idle`, `Routing`, `Routed`, `Streaming`, `Done`, `Failed`, `Cancelled`, `Awaiting`, `Confirming`
- The process map panel MUST display the current state with an icon and label (see design D1)
- The process map MUST transition to `Routing` state immediately when a prompt is submitted
- The process map MUST transition to `Routed` and show the kitchen name as soon as the sommelier decision is made — this MUST happen before any kitchen output lines arrive
- The process map MUST transition to `Streaming` on the first output line from the kitchen
- Elapsed time MUST be shown in the process map and update at least every 100ms during active dispatch

### routedMsg event

- The dispatch goroutine MUST emit a `routedMsg` to the TUI as soon as the routing decision is made
- `routedMsg` MUST carry the kitchen name and the full `sommelier.Decision`
- The `DispatchFunc` interface MUST support an `onRouted func(sommelier.Decision)` callback for this purpose

### Headless

- In non-TUI dispatch, when `--verbose` is set, a `[routed] <kitchen>` line MUST be printed to stderr as soon as the routing decision is made
