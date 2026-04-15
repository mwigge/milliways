# Spec: feedback-loop

## Overview

Users MUST be able to rate dispatch outcomes to improve learned routing. Feedback feeds the existing `pantry.routing.RecordOutcome()` model.

## Requirements

### TUI feedback

- Ctrl+F during Idle state (after a completed dispatch) MUST open a rating overlay
- The overlay MUST show: `Rate last dispatch: [g]ood  [b]ad  [s]kip`
- Single keypress MUST record the rating and close the overlay
- The rating MUST be stored on the current Section as `Rated *bool`
- Good ratings MUST call `pantry.routing.RecordOutcome()` with success=true
- Bad ratings MUST call `pantry.routing.RecordOutcome()` with success=false
- Skip MUST close the overlay without recording

### CLI feedback

- `milliways rate good` MUST rate the most recent ledger entry as successful
- `milliways rate bad` MUST rate the most recent ledger entry as failed
- Both commands MUST print confirmation: `Rated last dispatch (kitchen: X, prompt: "Y...") as good/bad`
- If no ledger entries exist, MUST print an error message

### Impact on routing

- Feedback ratings MUST be used by the existing `BestKitchen()` learned routing query
- Explicit bad ratings MUST decrease the kitchen's success rate for that task type
- Explicit good ratings MUST increase the kitchen's success rate for that task type
- No changes to the learned routing algorithm itself — feedback flows through the existing data path
