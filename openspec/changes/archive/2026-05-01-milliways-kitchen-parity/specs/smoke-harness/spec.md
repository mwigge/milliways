## ADDED Requirements

### Requirement: Deterministic fake kitchens
The system SHALL provide deterministic fake-kitchen binaries under `testdata/smoke/bin/` covering normal completion, exhaustion via text, exhaustion via structured event, crash, hang, malformed output, refusal, and at least one successful continuation target.

#### Scenario: Exhaustion-text fake kitchen
- **WHEN** the exhaustion-text fake kitchen is invoked
- **THEN** it SHALL emit a stable exhaustion message containing a reset time and exit in a way that triggers milliways exhaustion handling

#### Scenario: Crash fake kitchen
- **WHEN** the crash fake kitchen is invoked
- **THEN** it SHALL terminate abnormally in a deterministic way suitable for smoke assertions

### Requirement: Smoke scenarios cover critical routing flows
The smoke harness SHALL include scenarios for normal completion, exhaustion-text, exhaustion-structured, crash, hang, malformed output, explicit user switch, and continuous-routing hard-signal switch.

#### Scenario: User-switch smoke
- **WHEN** the `user-switch.sh` smoke scenario is run
- **THEN** it SHALL assert that a mid-conversation `/switch` creates a new segment and the next kitchen turn completes successfully

#### Scenario: Continuous-route smoke
- **WHEN** the `continuous-route.sh` smoke scenario is run
- **THEN** it SHALL assert that a hard-signal turn triggers an automatic switch and records the reason line

### Requirement: Smoke target integrated into CI
The repository SHALL expose a `make smoke` target that runs the smoke scenarios against the built milliways binary, and CI SHALL fail the build if any smoke scenario fails.

#### Scenario: Local smoke target
- **WHEN** a developer runs `make smoke`
- **THEN** all smoke scenarios under `testdata/smoke/scenarios/` SHALL execute against the current built binary

#### Scenario: CI blocks on smoke regression
- **WHEN** any smoke scenario fails in CI
- **THEN** the CI job SHALL fail and block merge
