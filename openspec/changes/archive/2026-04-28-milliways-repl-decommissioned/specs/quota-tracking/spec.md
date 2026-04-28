## ADDED Requirements

### Requirement: /limit shows per-runner quotas
The `/limit` command SHALL display quota information for all three runners (claude, codex, minimax) with day/week/month breakdowns and reset timestamps.

#### Scenario: View quota for all runners
- **WHEN** user types `/limit`
- **THEN** quota information for claude, codex, and minimax is displayed

### Requirement: Day limit display
Each runner SHALL show current day usage, day limit, percentage used, and time until reset.

#### Scenario: Day limit display
- **WHEN** `/limit` is executed
- **THEN** for each runner: day used / day limit (percentage) [resets HH:MM]

### Requirement: Week limit display
Each runner SHALL show current week usage, week limit, percentage used, and day of week reset.

#### Scenario: Week limit display
- **WHEN** `/limit` is executed
- **THEN** for each runner: week used / week limit (percentage) [resets DayName]

### Requirement: Month limit display
Each runner SHALL show current month usage, month limit, percentage used, and date of month reset.

#### Scenario: Month limit display
- **WHEN** `/limit` is executed
- **THEN** for each runner: month used / month limit (percentage) [resets MMM DD]

### Requirement: Runner quota data source
Quota data SHALL be obtained from each runner's native quota command or API:
- claude: `claude quota` or equivalent
- codex: `codex quota` or equivalent
- minimax: `mmx quota` (already exists)

#### Scenario: Quota data retrieval
- **WHEN** `/limit` is executed
- **THEN** milliways queries each runner's quota interface

### Requirement: Fallback for unavailable quota data
If a runner's quota interface is unavailable, `/limit` SHALL show "unknown" for that runner's data.

#### Scenario: Quota unavailable
- **WHEN** a runner's quota command fails or is unavailable
- **THEN** "unknown" is displayed for that runner's quota data

### Requirement: /cost shows session cost
The `/cost` command SHALL display the current session's accumulated cost in USD.

#### Scenario: Session cost display
- **WHEN** user types `/cost`
- **THEN** the session's total cost is displayed (e.g., "$0.42")
