## ADDED Requirements

### Requirement: Login overlay replaces subprocess login

When `/login <kitchen>` is invoked in the TUI, it SHALL render a login overlay instead of spawning a subprocess with redirected stdin. The overlay replaces `LoginKitchen` in `maitre/onboard.go` for TTY-mode operation.

#### Scenario: Show login overlay with kitchen list

- **WHEN** user types `/login` without a kitchen argument
- **THEN** milliways SHALL show an overlay listing all kitchens with auth status
- **AND** each kitchen SHALL show: name, auth method, status badge (ready/needs-auth/not-installed), and an action button
- **AND** the overlay SHALL use the same `maitre.Diagnose` output already used in the jobs panel for consistency

#### Scenario: Login via inline API key input (MiniMax, Groq)

- **WHEN** user selects `minimax` or `groq` from the login overlay
- **THEN** milliways SHALL show an inline input field within the overlay for the API key
- **AND** the input SHALL be masked (dots, not raw characters) in the TUI
- **AND** on submit, `UpdateKitchenAuth` SHALL be called to persist the key to `carte.yaml`
- **AND** the overlay SHALL close and show a success/failure message in the command feedback area

#### Scenario: OAuth login via browser (claude, gemini, codex)

- **WHEN** user selects `claude`, `gemini`, or `codex` from the login overlay
- **THEN** milliways SHALL attempt to open the system's default browser to the provider's OAuth URL
- **AND** if a callback URL with an auth code is received (via localhost server on a high port), the code SHALL be exchanged for a session token
- **AND** the session token SHALL be stored in the provider's native storage location
- **AND** if the system cannot open a browser (headless), milliways SHALL show the OAuth URL and a manual code-entry form

#### Scenario: Interactive TUI login (opencode)

- **WHEN** user selects `opencode` from the login overlay
- **AND** the PTY is available
- **THEN** milliways SHALL spawn `opencode providers` in the PTY shell and let the opencode TUI handle its own flow
- **AND** on completion, re-run `Diagnose` to refresh auth status

#### Scenario: Env-var instruction mode (aider, goose, cline, groq fallback)

- **WHEN** user selects a kitchen that uses env var auth
- **THEN** milliways SHALL show instructions in the overlay: which env var to set, what value format, and a link to the provider's docs
- **AND** milliways SHALL NOT attempt to read/write the env var directly — it SHALL guide the user to set it in their shell profile

### Requirement: Non-TTY fallback

When milliways is run without a PTY and a login is attempted, the login overlay SHALL detect this and show a clear message:

> "Interactive login requires a PTY. Set `MINIMAX_API_KEY` (for minimax) or `ANTHROPIC_API_KEY` (for claude/aider) in your environment, then restart milliways."

#### Scenario: Login attempted without PTY

- **WHEN** user runs `/login minimax` in a non-PTY milliways session
- **THEN** the overlay SHALL show the env-var instruction message
- **AND** the overlay SHALL provide a "Copy command to clipboard" action for setting the relevant env var