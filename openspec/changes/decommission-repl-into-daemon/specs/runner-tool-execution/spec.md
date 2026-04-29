## ADDED Requirements

### Requirement: Uniform tool registry for HTTP runners

Every HTTP-based runner (`minimax`, `copilot`, `local`) SHALL expose the agentic tool loop using `internal/tools/.NewBuiltInRegistry()`. The built-in tool set covers `bash`, file `read`/`write`/`edit`, `grep`, `glob`, and `web_fetch`.

#### Scenario: HTTP runner declares tools to the model

- **WHEN** an HTTP runner sends its first request in a dispatch
- **THEN** the request payload includes a `tools` array derived from the registered tool schemas
- **AND** every tool in `NewBuiltInRegistry()` appears in that array

#### Scenario: CLI runners do not run the internal tool loop

- **WHEN** a CLI-based runner (`claude`, `codex`, `gemini`) dispatches
- **THEN** no `tools.Registry` is invoked from within the runner
- **AND** tool execution is delegated to the underlying CLI process

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

### Requirement: Stream integrity checks for minimax

The minimax runner SHALL detect incomplete streams (unclosed code fences, unclosed shell heredocs) and surface a structured warning when an integrity violation is observed without a clean completion.

#### Scenario: Stream ends with an unclosed code fence

- **WHEN** the SSE stream closes before the model reaches `finish_reason: stop` and a code fence (` ``` `) was opened but never closed
- **THEN** the runner emits a warning identifying "unclosed code fence"
- **AND** the dispatch returns an error wrapping the integrity reason
- **AND** no tool that the model only partially described inside the fence is executed

#### Scenario: Stream contains a heredoc-style file write that never executes

- **WHEN** the stream contains `... >> file <<MARKER ... MARKER` outside an executed tool call
- **THEN** the runner emits a warning that "generated file-write command was not executed"
- **AND** the dispatch result reflects that no workspace mutation occurred via that path
