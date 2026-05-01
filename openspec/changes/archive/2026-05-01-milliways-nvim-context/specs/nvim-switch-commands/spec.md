## ADDED Requirements

### Requirement: MilliwaysSwitch command
The plugin SHALL expose a `:MilliwaysSwitch <kitchen>` command that switches the active conversation to the named kitchen via the MemPalace substrate, with tab-completion over available kitchen names.

#### Scenario: Switch to a valid kitchen
- **WHEN** the user executes `:MilliwaysSwitch codex` with an active conversation
- **THEN** the current segment SHALL be ended and a new codex segment SHALL be started via the substrate, matching the semantics of `/switch codex` in the TUI

#### Scenario: Switch reflected across surfaces
- **WHEN** `:MilliwaysSwitch codex` completes
- **THEN** another milliways instance reading the same MemPalace drawer SHALL observe the updated active segment provider

#### Scenario: Tab-completion on kitchen names
- **WHEN** the user types `:MilliwaysSwitch <Tab>`
- **THEN** nvim SHALL present a completion list of configured kitchen names

#### Scenario: Switch to unknown kitchen
- **WHEN** the user executes `:MilliwaysSwitch unknown-kitchen`
- **THEN** the float SHALL display an error line and no switch SHALL occur

### Requirement: MilliwaysStick command
The plugin SHALL expose a `:MilliwaysStick` command that pins the current kitchen, disabling automatic routing, and displays `[stuck: <kitchen>]` in the float header while active. A second invocation SHALL release the pin.

#### Scenario: Stick enables sticky mode
- **WHEN** the user executes `:MilliwaysStick` with no active sticky mode
- **THEN** the float header SHALL display `[stuck: <current-kitchen>]` and the sommelier SHALL not auto-switch

#### Scenario: Stick toggles off
- **WHEN** the user executes `:MilliwaysStick` while sticky mode is active
- **THEN** sticky mode SHALL be released, the header indicator SHALL be removed, and auto-routing SHALL resume

### Requirement: MilliwaysBack command
The plugin SHALL expose a `:MilliwaysBack` command that reverses the most recent kitchen switch. If no prior switch exists, it SHALL display a notice and take no action.

#### Scenario: Back reverses last switch
- **WHEN** the user executes `:MilliwaysBack` after a prior switch
- **THEN** the conversation SHALL re-switch to the previous kitchen via the substrate

#### Scenario: Back with no prior switch
- **WHEN** the user executes `:MilliwaysBack` with no prior switch in this conversation
- **THEN** the float SHALL display `[milliways] no prior switch to reverse` and no state change SHALL occur

### Requirement: MilliwaysKitchens picker
The plugin SHALL expose a `:MilliwaysKitchens` command that opens a picker (Telescope when installed, `vim.ui.select` otherwise) listing available kitchens. Selecting a kitchen SHALL invoke `:MilliwaysSwitch <kitchen>`.

#### Scenario: Kitchens picker with Telescope
- **WHEN** Telescope is installed and the user executes `:MilliwaysKitchens`
- **THEN** a Telescope picker SHALL open listing configured kitchen names and their current status

#### Scenario: Kitchens picker without Telescope
- **WHEN** Telescope is not installed and the user executes `:MilliwaysKitchens`
- **THEN** `vim.ui.select` SHALL be used as the fallback picker

#### Scenario: Selection triggers switch
- **WHEN** the user selects a kitchen from the picker
- **THEN** `:MilliwaysSwitch <kitchen>` SHALL be invoked automatically
