## ADDED Requirements

### Requirement: CLI kitchens execute as subprocess
The kitchens claude and codex SHALL execute as `exec.CommandContext` subprocesses with piped stdout for streaming output.

#### Scenario: Execute claude subprocess
- **WHEN** user submits a prompt with claude as active runner
- **THEN** milliways executes `claude [args]` as a subprocess and streams stdout

#### Scenario: Execute codex subprocess
- **WHEN** user submits a prompt with codex as active runner
- **THEN** milliways executes `codex [args]` as a subprocess and streams stdout

### Requirement: minimax uses HTTP API
The minimax kitchen SHALL use direct HTTP API calls to the MiniMax endpoint (MiniMax-M2.7 model), streaming via SSE.

#### Scenario: Execute minimax API call
- **WHEN** user submits a prompt with minimax as active runner
- **THEN** milliways sends HTTP POST to minimax API with SSE streaming response

### Requirement: Unified streaming pattern
All three kitchens SHALL produce line-by-line streaming output written directly to terminal stdout without buffering.

#### Scenario: Streaming from CLI kitchen
- **WHEN** claude or codex streams output line by line
- **THEN** each line appears immediately on stdout

#### Scenario: Streaming from minimax
- **WHEN** minimax streams SSE response
- **THEN** each line appears immediately on stdout

### Requirement: PTY for auth flows
When a runner requires interactive authentication, milliways SHALL allocate a PTY and attach it to the subprocess for the auth flow only.

#### Scenario: Claude auth login
- **WHEN** user runs `/login` and claude requires interactive auth
- **THEN** milliways allocates a PTY and runs `claude auth login` interactively

#### Scenario: Codex auth login
- **WHEN** user runs `/login` and codex requires interactive auth
- **THEN** milliways allocates a PTY and runs `codex login` interactively

### Requirement: CLI execution uses piped I/O
During normal prompt execution, CLI kitchens SHALL use piped stdin/stdout without PTY overhead.

#### Scenario: Normal CLI prompt execution
- **WHEN** user submits a prompt to claude or codex (already authenticated)
- **THEN** execution uses piped stdin/stdout without PTY

### Requirement: Runner subprocess lifecycle (CLI kitchens)
Milliways SHALL manage the CLI subprocess lifecycle: start on `/switch`, stream output, and terminate cleanly on next `/switch` or `/exit`.

#### Scenario: CLI runner lifecycle
- **WHEN** user switches from claude to codex
- **THEN** the claude subprocess is terminated and codex subprocess is started
