# kitchen-auth — Unified Login for All Kitchens

## ADDED Requirements

### Requirement: TUI /login command lists kitchens with auth status

The TUI SHALL provide a `/login` palette command that lists all configured kitchens with their current auth status and appropriate login action.

#### Scenario: /login with no arguments shows kitchen auth status list
- **WHEN** user types `/login` with no arguments in the TUI
- **THEN** TUI displays each kitchen with: name, auth status (not installed / needs auth / authenticated / no auth needed), and the login command or instruction

#### Scenario: /login <kitchen> initiates kitchen-specific login
- **WHEN** user types `/login claude` (or any specific kitchen name)
- **THEN** TUI initiates the appropriate login flow for that kitchen type

#### Scenario: /login with no kitchen args and no TTY shows usage
- **WHEN** `/login` is invoked with no arguments and stdin is not a TTY
- **THEN** TUI prints a brief usage line: "usage: /login <kitchen>" and returns

### Requirement: CLI login subcommand for headless environments

The CLI SHALL provide a `milliways login <kitchen>` subcommand that performs the same auth flow as the TUI for headless/scripted use.

#### Scenario: milliways login claude runs auth and exits
- **WHEN** user runs `milliways login claude`
- **THEN** milliways executes `claude auth login` and exits with status 0 on success

#### Scenario: milliways login with no args shows usage
- **WHEN** user runs `milliways login` with no kitchen argument
- **THEN** milliways prints usage: "usage: milliways login <kitchen>" and exits with code 1

#### Scenario: milliways login --list shows all kitchens with status
- **WHEN** user runs `milliways login --list`
- **THEN** milliways prints all kitchens with auth status and available login actions

### Requirement: MiniMax API key login prompts and updates carte.yaml

For minimax, the login flow SHALL prompt for an API key interactively and write it to `carte.yaml`.

#### Scenario: /login minimax prompts for API key
- **WHEN** user runs `/login minimax` or `milliways login minimax`
- **THEN** TUI/CLI prompts: "Enter your MiniMax API key:" (with masked input)
- **AND** on valid key entry, updates `carte.yaml`'s `kitchens.minimax.http_client.auth_key` to the provided key
- **AND** prints "✓ MiniMax authenticated"

#### Scenario: MiniMax login with non-TTY input shows instructions
- **WHEN** `/login minimax` or `milliways login minimax` is invoked and stdin is not a TTY
- **THEN** milliways prints: "MiniMax requires an API key. Set MINIMAX_API_KEY in your shell profile, or run interactively."

#### Scenario: MiniMax login writes atomically and backs up
- **WHEN** MiniMax login successfully updates `carte.yaml`
- **THEN** milliways writes to a temporary file then renames it (atomic operation)
- **AND** creates a backup at `carte.yaml.bak`

### Requirement: CLI OAuth kitchens (claude, gemini) delegate to provider CLI

For claude and gemini, the login flow SHALL delegate to the provider's own auth command.

#### Scenario: /login claude runs claude auth login
- **WHEN** user runs `/login claude`
- **THEN** milliways executes `claude auth login` (which opens browser OAuth)
- **AND** waits for the command to complete
- **AND** refreshes kitchen status after completion

#### Scenario: /login gemini runs gemini auth login
- **WHEN** user runs `/login gemini`
- **THEN** milliways executes `gemini auth login`
- **AND** waits for the command to complete
- **AND** refreshes kitchen status after completion

### Requirement: OpenCode delegates to opencode providers

For opencode, the login flow SHALL delegate to the provider's interactive auth UI.

#### Scenario: /login opencode runs opencode providers
- **WHEN** user runs `/login opencode`
- **THEN** milliways executes `opencode providers` (interactive TUI)
- **AND** waits for the command to complete
- **AND** refreshes kitchen status after completion

### Requirement: Env-var kitchens (groq, aider, goose) show setup instructions

For kitchens that use environment variables, the login flow SHALL print clear setup instructions rather than running an interactive process.

#### Scenario: /login groq shows env var instructions
- **WHEN** user runs `/login groq`
- **THEN** milliways prints:
  ```
  Groq uses the GROQ_API_KEY environment variable.
  Set it in your shell profile:
    export GROQ_API_KEY=your_key_here
  Get your key at: https://console.groq.com/keys
  ```

### Requirement: Ollama reports no authentication needed

For ollama, the login flow SHALL report that no authentication is required and optionally verify the service is running.

#### Scenario: /login ollama reports no auth needed
- **WHEN** user runs `/login ollama`
- **THEN** milliways prints: "Ollama uses no authentication. Verifying service..."
- **AND** checks if `http://localhost:11434` is reachable
- **AND** prints "✓ Ollama is running" or "✗ Ollama is not running at localhost:11434. Start it with: ollama serve"

### Requirement: Kitchen status reflects auth state

The `milliways status` output and `/login --list` SHALL show the current auth state for each kitchen.

#### Scenario: Status shows NotInstalled for uninstalled kitchens
- **WHEN** user runs `milliways status`
- **THEN** any kitchen whose binary is not found shows status "NotInstalled"

#### Scenario: Status shows NeedsAuth for installed but unauthenticated kitchens
- **WHEN** user runs `milliways status`
- **THEN** a kitchen that is installed but has no valid credentials shows status "NeedsAuth"

#### Scenario: Status shows Ready for authenticated kitchens
- **WHEN** user runs `milliways status`
- **THEN** a kitchen with valid credentials shows status "Ready"
