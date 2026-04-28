# Spec: kitchen-dialogue

## Overview

During a live dispatch, the user MUST be able to answer questions, grant confirmations, and inject additional context — without cancelling and restarting the dispatch.

## Requirements

### Protocol

- The dialogue protocol MUST use stdout line prefixes: `?MW> ` for questions and `!MW> ` for confirm/deny prompts
- Protocol constants MUST be defined in `internal/kitchen/dialogue.go`
- Kitchen CLIs that do not use these prefixes MUST be unaffected

### Task struct

- `Task` MUST gain `OnQuestion func(string)`, `OnConfirm func(string)`, and `AnswerCh chan string` fields
- When `OnQuestion`, `OnConfirm`, or `AnswerCh` are nil (headless mode), the kitchen MUST auto-answer with an empty string and continue without blocking

### GenericKitchen stdin pipe

- `GenericKitchen.Exec()` MUST open a stdin pipe to the subprocess and hold it open for the duration of the process
- When a `?MW>` line is scanned, `OnQuestion` MUST be called and `Exec()` MUST block on `AnswerCh` before writing the answer to stdin
- When a `!MW>` line is scanned, `OnConfirm` MUST be called and `Exec()` MUST block on `AnswerCh` before writing the answer to stdin

### TUI question overlay

- WHEN a `questionMsg` is received, the TUI MUST enter `Awaiting` state
- An overlay input field MUST appear, styled distinctly from the main input (yellow/amber border)
- The question text MUST be shown in the process map panel
- WHEN the user submits the overlay input, the answer MUST be written to `AnswerCh` and the TUI MUST return to `Streaming` state

### TUI confirm prompt

- WHEN a `confirmMsg` is received, the TUI MUST enter `Confirming` state
- An inline `[confirm] <text> [y/N]` line MUST be appended to the output viewport
- `y`, `n`, or Enter (= no) MUST write the answer to `AnswerCh` and return to `Streaming` state

### Ctrl+I context injection

- DURING `Streaming` state, Ctrl+I MUST open the overlay input with placeholder `"+ context: "`
- WHEN submitted, the text MUST be written to `AnswerCh` and a `[+context] <text>` line MUST be appended to the output viewport in muted style
- Context injection MUST NOT require a `?MW>` line from the kitchen — it is user-initiated
