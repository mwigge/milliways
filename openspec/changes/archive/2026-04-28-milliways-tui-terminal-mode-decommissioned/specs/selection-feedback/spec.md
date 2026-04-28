## ADDED Requirements

### Requirement: Visual selection highlight in viewport

When the user drags the mouse to select text in the TUI's main viewport area, milliways SHALL render the selected range with a visual highlight (lipgloss reverse attribute or background color change) so the selection is clearly visible before yanking.

#### Scenario: Mouse selection shows highlight

- **WHEN** user clicks and drags in the viewport area to select text
- **THEN** the selected text range SHALL be rendered with a distinct highlight (e.g., reversed foreground/background or a colored background such as `#2563EB` at 30% opacity)
- **AND** the highlight SHALL persist until the mouse button is released
- **AND** on release, the selected text SHALL be copied to the system clipboard

#### Scenario: Selection cleared on click

- **WHEN** user clicks elsewhere in the viewport without dragging
- **THEN** any existing selection SHALL be cleared and the highlight removed

### Requirement: Selection works in block output

The block output area (the left panel showing streamed provider output) SHALL also support mouse selection with visual feedback. This is the primary area where users want to copy code snippets.

#### Scenario: Block text selection with highlight

- **WHEN** user selects text within a block's rendered output
- **THEN** the selection SHALL be visually highlighted
- **AND** on release, the selected text SHALL be copied to the clipboard
- **AND** the block SHALL NOT be scrolled or collapsed as a result of the selection action