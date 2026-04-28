## ADDED Requirements

### Requirement: /switch command
The `/switch <runner>` command SHALL switch the active runner to the specified kitchen (claude, codex, or minimax).

#### Scenario: Switch to codex
- **WHEN** user types `/switch codex`
- **THEN** codex becomes the active runner

### Requirement: /stick command
The `/stick` command SHALL keep the current runner sticky until `/stick` is used again to release.

#### Scenario: Stick to current runner
- **WHEN** user types `/stick`
- **THEN** the current runner remains active until `/stick` is used to release

### Requirement: /back command
The `/back` command SHALL reverse the most recent `/switch`.

#### Scenario: Reverse last switch
- **WHEN** user types `/back`
- **THEN** the previous runner becomes active

### Requirement: /session command
The `/session` command SHALL show the current session name. `/session <name>` SHALL name or rename the session.

#### Scenario: Name session
- **WHEN** user types `/session auth-refactor`
- **THEN** the session is named "auth-refactor"

### Requirement: /history command
The `/history` command SHALL display recent dispatches in the current session with timestamp, runner, prompt preview, status, and duration.

#### Scenario: View history
- **WHEN** user types `/history`
- **THEN** a list of recent dispatches is shown

### Requirement: /summary command
The `/summary` command SHALL display session statistics: session name, duration, number of switches, dispatch count per runner, and total estimated cost.

#### Scenario: View summary
- **WHEN** user types `/summary`
- **THEN** session statistics are displayed

### Requirement: /cost command
The `/cost` command SHALL display the current session's accumulated cost.

#### Scenario: View cost
- **WHEN** user types `/cost`
- **THEN** the session's total cost is displayed

### Requirement: /limit command
The `/limit` command SHALL display per-runner quotas: day/week/month usage and reset timestamps.

#### Scenario: View limits
- **WHEN** user types `/limit`
- **THEN** usage and limits for all three runners are shown

### Requirement: /openspec command
The `/openspec` command SHALL display the current OpenSpec change being worked on and its task progress.

#### Scenario: View current change
- **WHEN** user types `/openspec`
- **THEN** the active change and task status are shown

### Requirement: /repo command
The `/repo` command SHALL display the current git repository: repo name, branch, last commit, and git status (added/modified/deleted).

#### Scenario: View repo info
- **WHEN** user types `/repo`
- **THEN** git repository information is displayed

### Requirement: /login command
The `/login` command SHALL trigger the authentication flow for the current runner.

#### Scenario: Login to claude
- **WHEN** user types `/login` while claude is active
- **THEN** the claude auth flow is initiated

### Requirement: /logout command
The `/logout` command SHALL clear authentication credentials for the current runner.

#### Scenario: Logout from codex
- **WHEN** user types `/logout` while codex is active
- **THEN** codex credentials are cleared

### Requirement: /auth command
The `/auth` command SHALL display the authentication status for all runners.

#### Scenario: Check auth status
- **WHEN** user types `/auth`
- **THEN** authentication status for claude, codex, and minimax is shown

### Requirement: /help command
The `/help` command SHALL display available commands.

#### Scenario: View help
- **WHEN** user types `/help`
- **THEN** list of all commands is displayed

### Requirement: /exit command
The `/exit` command SHALL exit the REPL cleanly.

#### Scenario: Exit REPL
- **WHEN** user types `/exit`
- **THEN** REPL exits with status 0
