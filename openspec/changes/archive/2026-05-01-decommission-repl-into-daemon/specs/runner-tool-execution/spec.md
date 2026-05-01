## ADDED Requirements

### Requirement: Uniform tool registry for HTTP runners

Every HTTP-based runner SHALL drive the agentic tool loop using `internal/tools/.NewBuiltInRegistry()`. The built-in tool set covers `bash`, file `read`/`write`/`edit`, `grep`, `glob`, and `web_fetch`. The HTTP-based runner set is `minimax` and `local`. Tool execution is a core capability of every milliways runner — not an opt-in mode — because milliways is a development/deployment/devops surface where tool calls (file edit, shell, web fetch) are the workload.

#### Scenario: HTTP runner declares tools to the model

- **WHEN** an HTTP runner sends its first request in a dispatch
- **THEN** the request payload includes a `tools` array derived from the registered tool schemas
- **AND** every tool in `NewBuiltInRegistry()` appears in that array

#### Scenario: CLI runners delegate tool execution to the subprocess

- **WHEN** a CLI-based runner (`claude`, `codex`, `copilot`, `gemini`, `pool`) dispatches
- **THEN** no `tools.Registry` is invoked from within the runner
- **AND** tool execution is delegated to the underlying CLI subprocess (which has its own tool surface — claude/codex CLIs ship with file/bash tools; copilot uses `--allow-all-tools`; gemini and pool likewise expose tools natively)
- **AND** the daemon runner's job is to parse the CLI's output stream (text and structured events) and surface them via the daemon's `Pusher`

### Requirement: Streaming tool-call accumulation

HTTP runners SHALL accumulate streaming `tool_call` deltas across SSE chunks until the streamed assistant turn completes, then execute every fully assembled tool call before the next assistant turn begins.

#### Scenario: Multi-chunk tool-call arguments are reassembled

- **WHEN** the model streams a tool call whose `function.arguments` JSON arrives across multiple SSE chunks
- **THEN** the runner concatenates the argument fragments by `index`
- **AND** the assembled JSON is parsed once before invocation
- **AND** parse failure produces a structured error fed back to the model as the tool result

#### Scenario: Multiple tool calls in one assistant turn

- **WHEN** a single assistant turn emits two or more tool calls (`finish_reason: tool_calls`)
- **THEN** the runner executes every tool call in order
- **AND** appends one `tool` role message per tool call to the conversation history before the next turn

### Requirement: Loop bound

The agentic tool loop SHALL terminate after a maximum of 10 assistant→tool→assistant turns within a single dispatch.

#### Scenario: Loop hits the turn cap

- **WHEN** the model issues tool calls on every one of 10 consecutive turns
- **THEN** the runner stops executing tools after the 10th turn
- **AND** emits a structured warning identifying the runner and the cap value
- **AND** returns the last assistant message to the caller

#### Scenario: Loop exits cleanly on stop

- **WHEN** the model returns `finish_reason: stop` (or equivalent)
- **THEN** the runner exits the loop immediately
- **AND** does not invoke any tool whose schema was streamed in the same turn but not requested

### Requirement: Tool result formatting

Tool execution results SHALL be appended to the conversation history as messages with `role: "tool"`, `tool_call_id` matching the originating call, and `content` containing the tool output (or a structured error message on failure).

#### Scenario: Tool succeeds

- **WHEN** a tool invocation returns output without error
- **THEN** a message with `role: "tool"`, the matching `tool_call_id`, and the output as `content` is appended to history
- **AND** that message is included in the next request payload

#### Scenario: Tool fails

- **WHEN** a tool invocation returns an error
- **THEN** a `role: "tool"` message containing the error string (prefixed `error: `) is appended in place of the output
- **AND** the loop continues so the model can recover or surrender

### Requirement: System prompt for HTTP runners

HTTP runners SHALL prepend a clean system message to every dispatch instructing the model to use markdown formatting, call available tools rather than narrate intended actions, and respond directly. The Claude-Code-specific orchestration content from `req.Rules` SHALL NOT be forwarded to HTTP runners.

#### Scenario: System message present and req.Rules excluded

- **WHEN** an HTTP runner builds its first request
- **THEN** the `messages` array begins with a `role: "system"` entry containing the standard guidance
- **AND** the contents of `req.Rules` are not included in any message

### Requirement: Stream integrity for HTTP runners

HTTP runners SHALL surface stream truncation and incomplete tool-call assembly as structured errors rather than silently presenting partial results as a clean stop.

#### Scenario: Stream ends with content but no terminal event

- **WHEN** the SSE stream closes before the model emits `finish_reason: stop` (or any other finish_reason) AND no tool calls were assembled
- **THEN** the runner returns an error identifying "incomplete stream: EOF before terminal event"
- **AND** the daemon pushes a structured `err` event before any final `chunk_end`
- **AND** the loop does NOT treat the partial assistant turn as a clean completion

#### Scenario: Tool call assembled without a function name

- **WHEN** the SSE stream produced tool-call deltas (id, args fragments) but the `function.name` arrived empty by stream end
- **THEN** the runner surfaces a structured error to the model as the tool result with content `error: incomplete tool call (id=<id>)`
- **AND** the model can recover (issue another turn, abandon the call) on the next loop iteration

#### Scenario: SSE line exceeds the scanner buffer

- **WHEN** an SSE line (e.g. a tool-call arguments JSON over 1MB) exceeds `bufio.Scanner`'s buffer cap
- **THEN** the runner surfaces a structured error containing `bufio.ErrTooLong` (or equivalent)
- **AND** does not silently process partial buffer contents

### Requirement: Lexical integrity checks (deferred, follow-up)

Detection of unclosed code fences and unclosed shell heredocs in assistant content (used by REPL's runner_minimax.go to warn when a model described a file-write but never invoked the tool) is **deferred** to a follow-up change. With the agentic tool loop now wiring file/bash tools into HTTP runners, a model that wants to write a file is expected to invoke the `Write` or `Bash` tool rather than narrate a heredoc — making the integrity check a less load-bearing safety net than it was in the REPL world. Tracked in `openspec/changes/decommission-repl-into-daemon/follow-ups.md`.
