# Spec: quota-gated-routing

## ADDED Requirements

### Requirement: carte.yaml supports per-kitchen quota configuration
Each kitchen entry in `carte.yaml` SHALL support three optional quota fields: `daily_limit` (integer dispatches per day; 0 = unlimited), `daily_minutes` (float total dispatch minutes per day; 0 = unlimited), and `warn_threshold` (float 0.0–1.0, default 0.8).

#### Scenario: daily_limit configured
- **GIVEN** a kitchen entry with `daily_limit: 50`
- **WHEN** milliways loads the configuration
- **THEN** the kitchen SHALL be subject to a quota of 50 dispatches per calendar day

#### Scenario: daily_minutes configured
- **GIVEN** a kitchen entry with `daily_minutes: 60.0`
- **WHEN** milliways loads the configuration
- **THEN** the kitchen SHALL be subject to a quota of 60 cumulative minutes of dispatch per calendar day

#### Scenario: warn_threshold defaults to 0.8
- **GIVEN** a kitchen entry with no `warn_threshold` field
- **WHEN** milliways loads the configuration
- **THEN** the kitchen SHALL use a warn threshold of 0.8

---

### Requirement: Sommelier checks quota before selecting a kitchen
Before selecting a candidate kitchen, the sommelier SHALL call `QuotaStore.IsExhausted()` for every candidate and skip any kitchen that is exhausted.

#### Scenario: Exhausted kitchen is skipped
- **WHEN** the sommelier evaluates a candidate kitchen
- **AND** `QuotaStore.IsExhausted(kitchen)` returns true
- **THEN** the sommelier SHALL skip that kitchen and evaluate the next candidate
- **AND** `Decision.Reason` SHALL contain the skip explanation: `"kitchen exhausted (N/N today, resets HH:MM) → fallback other_kitchen"`

#### Scenario: All kitchens exhausted
- **WHEN** every candidate kitchen is exhausted
- **THEN** the sommelier SHALL return a Decision with an empty Kitchen field
- **AND** `Decision.Reason` SHALL explain that all kitchens are exhausted

---

### Requirement: Rate-limit events from adapters update quota state
When the TUI receives an `EventRateLimit` with `Status` set to `"exhausted"`, it SHALL call `QuotaStore.MarkExhausted(kitchen, resetsAt)`.

#### Scenario: Rate-limit event marks kitchen exhausted
- **WHEN** the TUI receives `EventRateLimit` with `Status: "exhausted"` for a kitchen
- **THEN** milliways SHALL call `QuotaStore.MarkExhausted(kitchen, resetsAt)`
- **AND** that kitchen SHALL be treated as exhausted for all subsequent routing decisions

#### Scenario: Exhaustion expires after reset time
- **GIVEN** a kitchen has been marked exhausted with a `resetsAt` timestamp
- **WHEN** the current time is past `resetsAt`
- **THEN** the kitchen SHALL be considered available again

---

### Requirement: TUI status bar shows kitchen quota states
The TUI SHALL display a status bar that shows the current quota state of every installed kitchen.

#### Scenario: Ready kitchen displayed
- **WHEN** a kitchen is installed and not exhausted or in warning state
- **THEN** the status bar SHALL display the kitchen name in green with a ✓ indicator

#### Scenario: Exhausted kitchen displayed
- **WHEN** a kitchen is exhausted
- **THEN** the status bar SHALL display the kitchen name in red with a ✗ indicator and `(resets HH:MM)`

#### Scenario: Warning kitchen displayed
- **WHEN** a kitchen's usage ratio exceeds its `warn_threshold`
- **THEN** the status bar SHALL display the kitchen name in yellow with a ⚠ indicator and the current usage fraction (e.g., `claude ⚠ 42/50`)

#### Scenario: Not-installed kitchen omitted
- **WHEN** a kitchen is not installed on the host
- **THEN** the status bar SHALL omit that kitchen entirely

---

### Requirement: Mid-dispatch exhaustion is handled safely
When a kitchen is rate-limited mid-dispatch, the current dispatch SHALL run to completion or failure before quota state is updated.

#### Scenario: Quota updated after dispatch completes
- **WHEN** a rate-limit event is received during an active dispatch
- **THEN** the current dispatch SHALL complete or fail before the quota store is updated
- **AND** the NEXT dispatch SHALL route around the exhausted kitchen
