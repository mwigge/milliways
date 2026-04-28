## ADDED Requirements

### Requirement: Green phosphor color palette
The REPL SHALL use the Adobe "Monochromatic CPU Terminal Green" color palette for all terminal output.

#### Scenario: Color palette applied
- **WHEN** the REPL renders output
- **THEN** colors from the phosphor green palette are used

### Requirement: Background color
The terminal background SHALL be pure black (#000000).

#### Scenario: Black background
- **WHEN** the REPL starts
- **THEN** the terminal background is #000000

### Requirement: Primary text color
Active output text SHALL be bright phosphor green (#4FB522).

#### Scenario: Primary text
- **WHEN** runner output text is displayed
- **THEN** the text color is #4FB522

### Requirement: Secondary text color
Secondary text (timestamps, metadata) SHALL be medium green (#2E6914).

#### Scenario: Secondary text
- **WHEN** timestamps or secondary metadata are displayed
- **THEN** the text color is #2E6914

### Requirement: Muted text color
Borders, inactive elements, and muted text SHALL be dark green (#466D35).

#### Scenario: Muted elements
- **WHEN** borders or inactive elements are rendered
- **THEN** the color is #466D35

### Requirement: Error color
Error messages SHALL be displayed in red (#FF4444).

#### Scenario: Error display
- **WHEN** an error occurs
- **THEN** the error text is displayed in #FF4444

### Requirement: Warning/running color
Warning messages and active/running indicators SHALL be amber (#FFAA00).

#### Scenario: Warning display
- **WHEN** a warning or running indicator is displayed
- **THEN** the color is #FFAA00

### Requirement: Kitchen accent colors
Each runner SHALL be identifiable by a consistent accent color:
- claude: #7C3AED (purple)
- codex: #059669 (green)
- minimax: #2563EB (blue)

#### Scenario: Kitchen badge colors
- **WHEN** a runner name is displayed as a badge
- **THEN** it uses the runner's accent color
