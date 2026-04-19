## ADDED Requirements

### Requirement: Normalized Event channel
Every kitchen adapter SHALL communicate through a `<-chan Event` returned by `Exec()`. The channel SHALL be closed when the kitchen process exits. Event types SHALL include: Text, CodeBlock, ToolUse, Question, Confirm, Cost, RateLimit, Error, Done.

#### Scenario: Channel closed on process exit
- **WHEN** the kitchen process exits normally or abnormally
- **THEN** the adapter SHALL close the event channel so callers can detect completion without goroutine leaks

#### Scenario: TUI contains no kitchen-specific parsing
- **WHEN** the TUI receives events from any adapter
- **THEN** it SHALL handle only the normalized Event types and SHALL NOT contain any kitchen-specific protocol parsing logic

### Requirement: Code block detection in adapter output
Adapters SHALL detect fenced code blocks (triple backtick) in kitchen text output and emit them as EventCodeBlock with Language and Code fields. Text outside code blocks SHALL be emitted as EventText. If language detection fails, Language SHALL be set to `""` (empty string).

#### Scenario: Fenced code block extracted
- **WHEN** the adapter scans a line starting with triple backtick
- **THEN** subsequent lines SHALL be buffered and emitted as a single EventCodeBlock when the closing triple backtick is encountered

#### Scenario: Auto-detect language on empty lang field
- **WHEN** the opening fence has no language tag
- **THEN** the Language field SHALL be set to `""` and the renderer SHALL rely on chroma auto-detection

### Requirement: ClaudeAdapter invocation and session resume
The ClaudeAdapter SHALL invoke claude with `--print --verbose --output-format stream-json --input-format stream-json`. It SHALL parse `rate_limit_event`, emit EventRateLimit with parsed `resetsAt`, parse `result` event and emit EventCost, store `session_id` from the init event, and support `--resume <session_id>` for subsequent dispatches in the same milliways session.

#### Scenario: Rate limit event parsed
- **WHEN** claude emits a `rate_limit_event` JSON line
- **THEN** the adapter SHALL emit EventRateLimit with the parsed `resetsAt` timestamp

#### Scenario: Session resumed with prior session_id
- **WHEN** a milliways session already has a stored session_id for claude
- **THEN** the adapter SHALL pass `--resume <session_id>` on subsequent dispatches

#### Scenario: Cost emitted on result event
- **WHEN** claude emits a `result` JSON line with `total_cost_usd` and token counts
- **THEN** the adapter SHALL emit EventCost carrying those values

### Requirement: GeminiAdapter quota error handling
The GeminiAdapter SHALL invoke gemini with `--prompt --output-format stream-json`, capture stderr, parse `TerminalQuotaError` messages, extract the reset duration, compute an absolute `resetsAt` time, and emit EventRateLimit. `Send()` SHALL return `ErrNotInteractive` because gemini `--prompt` is one-shot.

#### Scenario: TerminalQuotaError detected on stderr
- **WHEN** gemini writes a line containing `TerminalQuotaError` to stderr
- **THEN** the adapter SHALL emit EventRateLimit with a computed `resetsAt` derived from the error message

#### Scenario: Send returns ErrNotInteractive
- **WHEN** the caller invokes Send() on GeminiAdapter
- **THEN** it SHALL return ErrNotInteractive immediately without writing to the process

### Requirement: CodexAdapter JSONL event parsing
The CodexAdapter SHALL invoke codex with `exec --json`, parse JSONL events from stdout, open a stdin pipe for dialogue via Send(), and emit EventDone with exit code on process completion.

#### Scenario: JSONL events parsed from stdout
- **WHEN** codex emits a JSONL event line
- **THEN** the adapter SHALL parse it and emit the corresponding normalized Event

#### Scenario: EventDone on exit
- **WHEN** the codex process exits
- **THEN** the adapter SHALL emit EventDone with the process exit code and close the channel

### Requirement: OpenCodeAdapter session resume
The OpenCodeAdapter SHALL invoke opencode with `run --format json`, support `--continue` and `--session <id>` flags for session resume, and parse JSON events from stdout. `Send()` MAY return ErrNotInteractive depending on opencode stdin support.

#### Scenario: Session continues with --continue flag
- **WHEN** a prior opencode session id is available
- **THEN** the adapter SHALL pass `--session <id>` or `--continue` on subsequent dispatches

### Requirement: GenericAdapter line-by-line fallback
The GenericAdapter SHALL use `bufio.Scanner` for line-by-line reading of stdout, emit each line as EventText, emit EventDone with exit code on process completion, return ErrNotInteractive from Send(), and support all existing kitchens (aider, goose, and any future unknown kitchen).

#### Scenario: Each stdout line becomes EventText
- **WHEN** the kitchen subprocess writes a line to stdout
- **THEN** the adapter SHALL emit one EventText event per line

#### Scenario: Send blocked — returns ErrNotInteractive
- **WHEN** the caller invokes Send() on GenericAdapter
- **THEN** it SHALL return ErrNotInteractive without writing to the process stdin
