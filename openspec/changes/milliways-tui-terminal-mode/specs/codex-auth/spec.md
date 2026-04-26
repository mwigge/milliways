## ADDED Requirements

### Requirement: codex added to LoginKitchen switch

The `LoginKitchen` function in `internal/maitre/onboard.go` SHALL include a `case "codex"` that routes to the correct auth method for the codex CLI.

#### Scenario: codex login via CLI OAuth

- **WHEN** `LoginKitchen("codex")` is called
- **AND** a PTY is available
- **THEN** milliways SHALL run `exec.Command("codex", "auth")` with stdin/stdout/stderr connected to the PTY
- **AND** the codex CLI OAuth flow SHALL proceed in the terminal

#### Scenario: codex login without PTY

- **WHEN** `LoginKitchen("codex")` is called without PTY availability
- **THEN** milliways SHALL show the OAuth URL for codex and a manual code-entry form in the login overlay
- **AND** the auth status SHALL be refreshed after successful manual entry

### Requirement: codex kitchen registered

The `carte.yaml` default kitchens or the kitchen registry SHALL include a `codex` entry so that `milliways status` and `/kitchens` show codex alongside other providers.

#### Scenario: codex appears in kitchen list

- **WHEN** user runs `milliways status` or `/kitchens`
- **THEN** `codex` SHALL appear in the kitchen list with its auth status and install instructions if not installed