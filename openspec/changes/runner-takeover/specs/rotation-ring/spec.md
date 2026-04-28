## ADDED Requirements

### Requirement: Rotation ring configuration

The system SHALL provide a `/takeover-ring <r1,r2,...>` command to configure an ordered runner rotation ring for the current session. The ring SHALL persist across session saves and restores.

#### Scenario: Ring configured with valid runners

- **WHEN** user runs `/takeover-ring claude,codex,minimax`
- **AND** all three runners are registered
- **THEN** system SHALL set ring to `[claude, codex, minimax]` with position 0 (claude)
- **AND** system SHALL print `Rotation ring set: claude → codex → minimax → claude`

#### Scenario: Ring configured with unknown runner

- **WHEN** user runs `/takeover-ring claude,unknown`
- **THEN** system SHALL print `Unknown runner: unknown` and reject the ring

#### Scenario: Ring cleared

- **WHEN** user runs `/takeover-ring off` or `/takeover-ring clear`
- **THEN** system SHALL remove the ring configuration
- **AND** system SHALL print `Rotation ring cleared`

#### Scenario: Ring status shown

- **WHEN** user runs `/takeover-ring` with no arguments
- **THEN** system SHALL print the current ring or `No rotation ring configured`

### Requirement: Ring position tracks active runner

The system SHALL advance the ring position whenever a runner switch occurs (manual or automatic). The position always points to the currently active runner in the ring.

#### Scenario: Position advances on switch

- **WHEN** ring is `[claude, codex, minimax]` at position 0 (claude)
- **AND** `/takeover` is issued
- **THEN** active runner becomes codex and ring position advances to 1

#### Scenario: Ring wraps at end

- **WHEN** ring is `[claude, codex, minimax]` at position 2 (minimax)
- **AND** `/takeover` is issued
- **THEN** active runner becomes claude and ring position wraps to 0

#### Scenario: Ring persists across session restore

- **WHEN** ring is configured and session is saved
- **AND** milliways is restarted and session is restored
- **THEN** ring SHALL be restored with the same runners and last known position

### Requirement: Status bar ring indicator

When a rotation ring is active, the status bar SHALL show the current runner's position in the ring.

#### Scenario: Ring indicator displayed

- **WHEN** ring `[claude, codex, minimax]` is active and current runner is codex (position 2)
- **THEN** status bar runner segment SHALL read `●codex 2/3`

#### Scenario: No ring indicator when ring inactive

- **WHEN** no rotation ring is configured
- **THEN** status bar runner segment SHALL show only `●<runner>` with no position suffix

### Requirement: Ring skips quota-exhausted runners

When advancing the ring, the system SHALL skip any runner whose daily quota is zero according to the pantry store.

#### Scenario: Exhausted runner skipped

- **WHEN** ring is `[claude, codex, minimax]` at position 0 (claude)
- **AND** codex has zero daily quota remaining
- **AND** auto-rotate triggers
- **THEN** system SHALL skip codex and rotate to minimax
- **AND** system SHALL print `[ring] codex exhausted — skipping to minimax`

#### Scenario: All ring runners exhausted

- **WHEN** all runners in the ring have zero daily quota
- **THEN** system SHALL not rotate
- **AND** system SHALL print `[ring] all runners exhausted — cannot continue`
- **AND** the original limit error SHALL be surfaced to the user
