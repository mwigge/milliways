## ADDED Requirements

### Requirement: Only one runner active at a time
The REPL SHALL execute prompts through exactly one runner at a time. No concurrent dispatch within a single REPL instance.

#### Scenario: Only one runner runs
- **WHEN** a prompt is submitted while a runner is active
- **THEN** the prompt waits until the current runner completes before executing

### Requirement: Explicit runner switching
The user SHALL switch runners via the `/switch <runner>` command. No automatic routing.

#### Scenario: Switch from claude to codex
- **WHEN** user types `/switch codex`
- **THEN** the current runner (claude) is stopped and codex becomes active

### Requirement: Runner manages own subagents
Each runner (claude, codex, minimax) SHALL manage its own internal parallelization. Milliways does not coordinate subagent parallelism within a runner.

#### Scenario: Claude manages own subagents
- **WHEN** claude is the active runner
- **THEN** claude decides internally how to parallelize work

### Requirement: Runner stickiness
The user SHALL be able to sticky the current runner with `/stick` to prevent accidental switches. The runner remains until explicitly switched or `/stick` is released.

#### Scenario: Stick to claude
- **WHEN** user types `/stick`
- **THEN** the current runner remains active until `/stick` or `/switch` is used

### Requirement: Switch reversal
The user SHALL be able to reverse the most recent `/switch` with `/back`.

#### Scenario: Reverse last switch
- **WHEN** user types `/back`
- **THEN** the previous runner becomes active again

### Requirement: Parallelism via multiple sessions
Users who want parallel execution SHALL run multiple milliways REPL sessions in separate terminal windows.

#### Scenario: Two parallel sessions
- **WHEN** user runs `milliways` in two terminal windows
- **THEN** each session operates independently with its own runner
