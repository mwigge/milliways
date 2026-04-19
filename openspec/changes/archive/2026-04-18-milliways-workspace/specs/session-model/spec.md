# Spec: session-model

## ADDED Requirements

### Requirement: Session viewport is continuous across dispatches
The output viewport SHALL never be cleared between dispatches. Each dispatch SHALL append a new Section to the session, and the viewport SHALL support scrolling back through all previous sections.

#### Scenario: Viewport persists between dispatches
- **WHEN** a second dispatch completes
- **THEN** the output from the first dispatch SHALL remain visible above the new dispatch output
- **AND** the user SHALL be able to scroll up to view all prior sections

#### Scenario: Section stores dispatch metadata
- **WHEN** a dispatch completes
- **THEN** the resulting Section SHALL store: prompt, kitchen name, routing decision, output lines, result, cost, duration, and rating

---

### Requirement: Every output line is prefixed with its kitchen name
Each output line emitted by a kitchen SHALL be prefixed with `[kitchen_name]` colored with that kitchen's assigned color. System messages SHALL use a distinct `[milliways]` prefix in muted style.

#### Scenario: Kitchen output prefixed
- **WHEN** a kitchen emits an output line
- **THEN** the TUI SHALL prepend `[kitchen_name]` using the kitchen's assigned color from `kitchenColors`

#### Scenario: System message prefixed
- **WHEN** milliways emits a routing info or quota warning message
- **THEN** the TUI SHALL prepend `[milliways]` in muted style

---

### Requirement: Code blocks are syntax-highlighted
Events of type `EventCodeBlock` SHALL be rendered with syntax highlighting using chroma. An empty `Language` field SHALL trigger chroma auto-detection. If highlighting fails, the code SHALL be displayed without highlighting.

#### Scenario: Code block with language rendered with highlighting
- **WHEN** an `EventCodeBlock` with a non-empty `Language` field is received
- **THEN** the TUI SHALL render the code with chroma syntax highlighting using the specified language
- **AND** a terminal256-compatible theme (monokai or dracula) SHALL be used

#### Scenario: Code block without language auto-detected
- **WHEN** an `EventCodeBlock` with an empty `Language` field is received
- **THEN** the TUI SHALL pass the code to chroma for language auto-detection

#### Scenario: Highlighting failure falls back gracefully
- **WHEN** chroma raises an error while highlighting a code block
- **THEN** the TUI SHALL display the code as plain text without returning an error

---

### Requirement: Ctrl+G toggles between raw and glamour-rendered markdown
The default render mode SHALL be raw markdown (plain text with syntax-highlighted code blocks). Pressing Ctrl+G SHALL toggle between raw mode and glamour-rendered mode without losing scroll position.

#### Scenario: Default render mode is raw
- **GIVEN** milliways has just started
- **WHEN** the viewport displays output
- **THEN** markdown SHALL be shown as plain text with syntax-highlighted code blocks

#### Scenario: Ctrl+G switches to glamour mode
- **WHEN** the user presses Ctrl+G in raw mode
- **THEN** the viewport SHALL re-render section content through glamour with headings, lists, and tables styled
- **AND** kitchen prefixes SHALL remain visible

#### Scenario: Ctrl+G returns to raw mode
- **WHEN** the user presses Ctrl+G in glamour mode
- **THEN** the viewport SHALL return to raw rendering without losing the current scroll position

---

### Requirement: Submitted prompt is echoed immediately
When the user submits a prompt, the prompt text SHALL appear immediately in the viewport as `▶ prompt_text` in muted style, followed by a separator line, before any kitchen output.

#### Scenario: Prompt echo appears before kitchen output
- **WHEN** the user submits a prompt
- **THEN** `▶ prompt_text` SHALL appear in the viewport immediately in muted style
- **AND** a separator line SHALL follow
- **AND** kitchen output SHALL appear after the separator

---

### Requirement: Ctrl+S shows a session summary overlay
Pressing Ctrl+S SHALL display a session summary overlay. The overlay SHALL be dismissable with `q`.

#### Scenario: Summary overlay displayed
- **WHEN** the user presses Ctrl+S
- **THEN** an overlay SHALL appear showing: total dispatches, kitchens used with counts, total duration, total cost where available, and success rate

#### Scenario: Recent dispatches listed
- **WHEN** the session summary overlay is open
- **THEN** it SHALL list recent dispatches each showing: kitchen, prompt (truncated), duration, status, and cost

#### Scenario: Overlay dismissed with q
- **WHEN** the session summary overlay is open and the user presses `q`
- **THEN** the overlay SHALL close and the normal viewport SHALL be restored
