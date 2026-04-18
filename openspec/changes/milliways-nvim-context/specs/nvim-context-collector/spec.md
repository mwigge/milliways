## ADDED Requirements

### Requirement: Plugin module refactor
The nvim plugin SHALL be structured as a multi-file Lua module under `nvim-plugin/lua/milliways/` with files `init.lua`, `context.lua`, `commands.lua`, `float.lua`, and `kitchens.lua`, each with a single well-defined responsibility.

#### Scenario: Plugin loads cleanly after refactor
- **WHEN** a user calls `require('milliways').setup({})` in their nvim config
- **THEN** the plugin SHALL register all existing user commands and keybindings with no regression

#### Scenario: Module boundaries respected
- **WHEN** any module is required independently
- **THEN** it SHALL not error on load and SHALL expose only its documented public API

### Requirement: Buffer and cursor collectors
The plugin SHALL implement `Context.collect_buffer()` returning path, filetype, modified flag, total lines, and visible range; and `Context.collect_cursor()` returning line, column, and treesitter scope when a parser is available.

#### Scenario: Buffer collector on a normal file
- **WHEN** `Context.collect_buffer()` is called with a normal file buffer active
- **THEN** it SHALL return a table with non-nil `path`, `filetype`, `modified`, `total_lines`, and `visible_range.top` / `visible_range.bottom`

#### Scenario: Cursor collector with treesitter available
- **WHEN** `Context.collect_cursor()` is called and a treesitter parser is installed for the current filetype
- **THEN** it SHALL return `line`, `column`, and `scope.kind` / `scope.name` matching the nearest named node

#### Scenario: Cursor collector without treesitter
- **WHEN** `Context.collect_cursor()` is called and no treesitter parser is available
- **THEN** it SHALL return `line` and `column` only, with `scope` as nil, no error raised

### Requirement: LSP and git collectors
The plugin SHALL implement `Context.collect_lsp(scope)` returning LSP diagnostics filtered by severity within the visible range (default) or file-wide when `scope = "file"`; and `Context.collect_git()` returning branch, dirty flag, files changed, and ahead/behind counts.

#### Scenario: LSP collector with active language server
- **WHEN** `Context.collect_lsp("visible")` is called and an LSP client is attached
- **THEN** it SHALL return only diagnostics whose line falls within the current visible range

#### Scenario: LSP collector without language server
- **WHEN** `Context.collect_lsp("visible")` is called and no LSP client is attached
- **THEN** it SHALL return nil without raising an error or emitting a user-visible warning

#### Scenario: Git collector inside a repo
- **WHEN** `Context.collect_git()` is called from a buffer inside a git repository
- **THEN** it SHALL return branch, dirty, files_changed, ahead, and behind values

#### Scenario: Git collector outside a repo
- **WHEN** `Context.collect_git()` is called from a buffer not inside a git repository
- **THEN** it SHALL return nil without raising an error

### Requirement: Bundle builder performance budget
`Context.build(opts)` SHALL assemble a complete bundle in under 50ms wall-clock time on typical projects (< 10k LOC, LSP warm), with a per-collector timeout of 15ms and a total budget cap.

#### Scenario: Build completes within budget
- **WHEN** `Context.build()` is called on a real buffer with an active LSP
- **THEN** the returned bundle SHALL be produced within 50ms

#### Scenario: Slow collector times out gracefully
- **WHEN** a single collector exceeds its 15ms per-collector timeout
- **THEN** that collector SHALL return nil and the build SHALL complete using the remaining collectors' data

#### Scenario: Visual selection included on opt-in
- **WHEN** `Context.build({ include_selection = true })` is called while a visual selection is active
- **THEN** the bundle SHALL include a `selection` field with `start_line`, `end_line`, and `text`

#### Scenario: Quickfix included on opt-in
- **WHEN** `Context.build({ include_quickfix = true })` is called with entries in the quickfix list
- **THEN** the bundle SHALL include a `quickfix` array of entries
