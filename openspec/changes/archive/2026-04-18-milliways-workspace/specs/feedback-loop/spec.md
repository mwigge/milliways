## ADDED Requirements

### Requirement: TUI feedback overlay with Ctrl+F
During Idle state (after a completed dispatch), pressing Ctrl+F SHALL open a rating overlay showing `Rate last dispatch: [g]ood  [b]ad  [s]kip`. A single keypress SHALL record the rating and close the overlay. Good SHALL call `pantry.routing.RecordOutcome()` with success=true. Bad SHALL call it with success=false. Skip SHALL close the overlay without recording. The rating SHALL be stored on the current Section as `Rated *bool`.

#### Scenario: Good rating recorded
- **WHEN** the user presses Ctrl+F in Idle state and then presses `g`
- **THEN** `pantry.routing.RecordOutcome()` SHALL be called with success=true and the Section.Rated field SHALL be set to a pointer to true

#### Scenario: Bad rating recorded
- **WHEN** the user presses Ctrl+F in Idle state and then presses `b`
- **THEN** `pantry.routing.RecordOutcome()` SHALL be called with success=false and the Section.Rated field SHALL be set to a pointer to false

#### Scenario: Skip closes overlay without recording
- **WHEN** the user presses Ctrl+F in Idle state and then presses `s`
- **THEN** the overlay SHALL close and no call to RecordOutcome() SHALL be made

### Requirement: CLI feedback commands
`milliways rate good` SHALL rate the most recent ledger entry as successful. `milliways rate bad` SHALL rate the most recent ledger entry as failed. Both commands SHALL print a confirmation line: `Rated last dispatch (kitchen: X, prompt: "Y...") as good/bad`. If no ledger entries exist, an error message SHALL be printed.

#### Scenario: milliways rate good prints confirmation
- **WHEN** the user runs `milliways rate good` and at least one ledger entry exists
- **THEN** the command SHALL call RecordOutcome() with success=true and print the confirmation line including kitchen name and truncated prompt

#### Scenario: milliways rate bad prints confirmation
- **WHEN** the user runs `milliways rate bad` and at least one ledger entry exists
- **THEN** the command SHALL call RecordOutcome() with success=false and print the confirmation line

#### Scenario: No ledger entries produces error
- **WHEN** the user runs `milliways rate good` or `milliways rate bad` with no ledger entries
- **THEN** the command SHALL print an error message and exit with a non-zero status

### Requirement: Feedback ratings flow into learned routing
Feedback ratings SHALL be consumed by the existing `BestKitchen()` learned routing query through the existing data path. Explicit bad ratings SHALL decrease the kitchen's success rate for that task type. Explicit good ratings SHALL increase the kitchen's success rate. The learned routing algorithm itself SHALL NOT be modified.

#### Scenario: Bad rating lowers kitchen success rate
- **WHEN** a bad rating is recorded for a dispatch to kitchen K with task type T
- **THEN** the next BestKitchen() query for task type T SHALL reflect a lower success rate for kitchen K

#### Scenario: Good rating raises kitchen success rate
- **WHEN** a good rating is recorded for a dispatch to kitchen K with task type T
- **THEN** the next BestKitchen() query for task type T SHALL reflect a higher success rate for kitchen K
