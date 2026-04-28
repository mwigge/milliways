## Why

When a user submits a prompt in the Milliways TUI, their text vanishes and the interface goes silent until a response arrives — no echo, no routing status, no sign the kitchen received the order. Kitchens like claude also ask clarifying questions mid-task, request confirmation before destructive actions, or need additional context; currently there is no mechanism in the TUI to receive or reply to these messages. The result is a tool that feels like shouting into a void rather than collaborating with an intelligent kitchen.

## What Changes

- **Prompt echo**: submitted text is displayed in the output viewport immediately before output begins, so the user always knows what was sent
- **Dispatch pipeline indicator**: a visible state machine (`routing → routed(kitchen) → streaming → done`) with per-state labels and elapsed time replaces the current binary silent/done experience
- **`routedMsg` event**: the dispatch goroutine emits a new message to the TUI as soon as the sommelier decision is made, before any kitchen output arrives — the process map shows the correct kitchen name from the start
- **Inline question/answer channel**: kitchens can write structured question lines to stdout (a defined prefix, e.g., `?MW> `) which the TUI intercepts, pauses streaming, opens a focused input field, and forwards the user's answer back to the kitchen process stdin
- **Mid-task context injection**: a keyboard shortcut (Ctrl+I) opens an overlay input at any point during streaming where the user can append additional information or context that is piped into the kitchen's stdin
- **Confirm/deny prompts**: a structured prefix (`!MW> `) triggers a blocking yes/no prompt in the TUI before the kitchen continues (e.g., "About to delete 14 files. Proceed? [y/N]")

## Capabilities

### New Capabilities

- `dispatch-presence`: visible feedback for every stage of the dispatch pipeline (echo, routing, routed, streaming, done/failed) with per-state elapsed time and kitchen badge
- `kitchen-dialogue`: bidirectional communication between TUI and kitchen during a live dispatch — questions, answers, context injection, and confirm/deny prompts over stdin/stdout

### Modified Capabilities

- `tui-process-map`: the process map panel must show the kitchen name as soon as routing resolves (not only after dispatch completes) — requires `routedMsg` to arrive before `dispatchDoneMsg`

## Impact

- `internal/tui/app.go` — primary change surface: new message types, state transitions, input overlay, stdin pipe
- `internal/kitchen/kitchen.go` — `Task` struct gains a `Stdin io.Reader` field so the TUI can wire up an interactive pipe
- `internal/kitchen/generic.go` — `GenericKitchen.Exec()` must attach `Task.Stdin` to the subprocess stdin when non-nil
- `DispatchFunc` signature — needs to communicate routing decision back before result (new callback or channel)
- No changes to pantry, sommelier logic, ledger, or recipe engine
