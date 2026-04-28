## ADDED Requirements

### Requirement: REPL starts and displays prompt
The REPL SHALL start when milliways is invoked without arguments and display a prompt indicating the current runner and session.

#### Scenario: REPL starts successfully
- **WHEN** user runs `milliways` with no arguments
- **THEN** REPL starts and displays prompt with runner name and session name

### Requirement: REPL accepts text input
The REPL SHALL accept text input from the user via liner library with history and basic editing (Ctrl+U kill line, Ctrl+A bol, Ctrl+E eol).

#### Scenario: User types and submits prompt
- **WHEN** user types a prompt and presses Enter
- **THEN** the prompt is submitted to the current runner

### Requirement: REPL processes slash commands
The REPL SHALL recognize commands prefixed with `/` and execute the corresponding REPL command instead of sending to the runner.

#### Scenario: User types /help
- **WHEN** user types `/help` and presses Enter
- **THEN** help text is displayed and no runner invocation occurs

### Requirement: REPL processes bash commands
The REPL SHALL recognize commands prefixed with `!` and execute them as bash commands, displaying output directly.

#### Scenario: User types !pwd
- **WHEN** user types `!pwd` and presses Enter
- **THEN** the bash command runs and output is displayed

### Requirement: REPL handles Ctrl+C gracefully
The REPL SHALL handle Ctrl+C by interrupting the current runner if one is active, or exiting the REPL if no runner is active.

#### Scenario: Ctrl+C during active runner
- **WHEN** user presses Ctrl+C while a runner is producing output
- **THEN** the runner is interrupted and the REPL prompt returns

### Requirement: REPL handles Ctrl+D
The REPL SHALL exit cleanly when user presses Ctrl+D on an empty line.

#### Scenario: Ctrl+D on empty line
- **WHEN** user presses Ctrl+D with no input
- **THEN** REPL exits with status 0

### Requirement: REPL streams output in real-time
The REPL SHALL display runner output as it arrives, line by line, without buffering the entire response.

#### Scenario: Runner produces streaming output
- **WHEN** runner streams output line by line
- **THEN** each line appears in the terminal as it arrives
