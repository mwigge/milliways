## ADDED Requirements

### Requirement: Turn-boundary routing evaluation
The sommelier SHALL evaluate routing at initial dispatch, on each new user turn appended to a conversation, and on explicit `/reroute` requests. It SHALL NOT evaluate during assistant streaming or on every assistant turn.

#### Scenario: Evaluate on new user turn
- **WHEN** a new user turn is appended to an active conversation
- **THEN** the sommelier SHALL run its routing evaluation before dispatching the next kitchen turn

#### Scenario: No evaluation during streaming
- **WHEN** a kitchen is currently streaming assistant output within a segment
- **THEN** the sommelier SHALL NOT trigger a routing evaluation until the next turn boundary

#### Scenario: Explicit reroute request
- **WHEN** the user issues `/reroute`
- **THEN** the sommelier SHALL evaluate routing immediately using the current conversation state

### Requirement: Stickiness threshold and hard signals
The sommelier SHALL only auto-switch when either a hard signal is present or the candidate kitchen score exceeds the current kitchen score by at least the configured stickiness delta. Sticky mode SHALL disable auto-switching entirely.

#### Scenario: Auto-switch on hard signal
- **WHEN** the active turn includes an explicit hard signal such as "search the web"
- **THEN** the sommelier SHALL allow an auto-switch even if the score delta is below the default stickiness threshold

#### Scenario: Auto-switch on score delta
- **WHEN** the candidate kitchen score exceeds the current kitchen score by at least `routing.stickiness_delta`
- **THEN** the sommelier SHALL auto-switch to the candidate kitchen

#### Scenario: No auto-switch below threshold
- **WHEN** the candidate kitchen score exceeds the current kitchen score by less than the configured stickiness delta and no hard signal is present
- **THEN** the sommelier SHALL remain on the current kitchen

#### Scenario: Sticky mode blocks auto-switch
- **WHEN** sticky mode is active for the conversation
- **THEN** the sommelier SHALL not auto-switch regardless of score delta or hard signal

### Requirement: Auto-switch visibility and reversibility
Every automatic switch SHALL emit a visible TUI system line stating the origin kitchen, destination kitchen, and reason, and SHALL remain reversible through `/back`.

#### Scenario: Visible auto-switch reason line
- **WHEN** the sommelier auto-switches from claude to gemini because the turn mentions "search the web"
- **THEN** the TUI SHALL display `[milliways] switched claude → gemini — task mentioned "search the web" (hard signal). /back to reverse, /stick to disable.`

#### Scenario: Auto-switch reversible
- **WHEN** an automatic switch has just occurred
- **THEN** the user SHALL be able to issue `/back` and return to the prior kitchen
