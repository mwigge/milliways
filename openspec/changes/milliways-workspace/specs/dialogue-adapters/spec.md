# Spec: dialogue-adapters

## Overview

Every kitchen MUST communicate through a normalized Event channel. Kitchen-specific protocol parsing is encapsulated in adapter implementations. The TUI MUST NOT contain any kitchen-specific parsing logic.

## Requirements

### Event interface

- All adapters MUST emit events through a `<-chan Event` returned by `Exec()`
- The channel MUST be closed when the kitchen process exits
- The caller MUST drain the channel to prevent goroutine leaks
- Event types MUST include: Text, CodeBlock, ToolUse, Question, Confirm, Cost, RateLimit, Error, Done

### Code block detection

- Adapters MUST detect fenced code blocks (triple backtick) in kitchen text output
- Detected code blocks MUST be emitted as EventCodeBlock with Language and Code fields
- Text outside code blocks MUST be emitted as EventText
- If language detection fails, Language MUST be set to "" (empty) — chroma auto-detects

### ClaudeAdapter

- MUST invoke claude with `--print --verbose --output-format stream-json`
- MUST support bidirectional communication via `--input-format stream-json`
- MUST parse `rate_limit_event` and emit EventRateLimit with parsed `resetsAt`
- MUST parse `result` event and emit EventCost with `total_cost_usd` and token counts
- MUST store `session_id` from init event for session resume
- MUST support `--resume <session_id>` for subsequent dispatches in the same milliways session
- MUST include `--include-partial-messages` for streaming text
- Send() MUST write `{"type":"say","content":{"type":"text","text":"..."}}` to stdin

### GeminiAdapter

- MUST invoke gemini with `--prompt --output-format stream-json`
- MUST capture stderr and parse `TerminalQuotaError` messages
- MUST extract reset duration from error message and compute absolute resetsAt time
- MUST emit EventRateLimit when quota error is detected
- Send() MUST return ErrNotInteractive (gemini --prompt is one-shot)

### CodexAdapter

- MUST invoke codex with `exec --json`
- MUST parse JSONL events from stdout
- MUST open a stdin pipe for dialogue via Send()
- MUST emit EventDone with exit code on process completion

### OpenCodeAdapter

- MUST invoke opencode with `run --format json`
- MUST support `--continue` and `--session <id>` for session resume
- MUST parse JSON events from stdout
- Send() behaviour depends on opencode's stdin support — MAY return ErrNotInteractive

### GenericAdapter

- MUST use `bufio.Scanner` line-by-line reading (existing GenericKitchen logic)
- Each line MUST be emitted as EventText
- MUST emit EventDone with exit code on process completion
- Send() MUST return ErrNotInteractive
- MUST support all existing kitchens (aider, goose, any future unknown kitchen)
