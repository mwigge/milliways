## ADDED Requirements

### Requirement: Bundle schema definition
The system SHALL define an `EditorContext` bundle as a JSON document with a mandatory `schema_version` field and optional typed sections for buffer state, cursor, selection, LSP diagnostics, git state, project metadata, quickfix, and loclist.

#### Scenario: Well-formed minimal bundle accepted
- **WHEN** a bundle is received containing only `schema_version` and `buffer.path`
- **THEN** milliways SHALL accept it and treat all missing sections as absent signals

#### Scenario: Bundle with all sections
- **WHEN** a bundle contains schema_version, buffer, cursor, selection, lsp_diagnostics, git, project, quickfix, and loclist fields
- **THEN** milliways SHALL parse all sections into typed structs with no data loss

#### Scenario: Unknown major schema version rejected
- **WHEN** a bundle arrives with a `schema_version` whose major component does not match a supported version
- **THEN** milliways SHALL return a typed error containing the received version and a list of supported versions

#### Scenario: Additive minor-version fields accepted
- **WHEN** a bundle carries unknown fields alongside a known major version
- **THEN** milliways SHALL accept it, ignoring unknown fields without error

### Requirement: Transport flags
The system SHALL accept the editor-context bundle via two CLI flags: `--context-json` for inline JSON payloads and `--context-stdin` for piped JSON input.

#### Scenario: --context-json flag
- **WHEN** milliways is invoked with `--context-json '<valid-json>'`
- **THEN** the bundle SHALL be parsed before dispatch begins and made available to the sommelier and continuation builder

#### Scenario: --context-stdin flag
- **WHEN** milliways is invoked with `--context-stdin` and JSON is supplied on stdin
- **THEN** milliways SHALL read stdin to EOF, parse the bundle, and proceed identically to --context-json

#### Scenario: --context-file legacy compat
- **WHEN** milliways is invoked with the existing `--context-file <path>` flag
- **THEN** the file path SHALL be reconstructed into a minimal bundle with only `buffer.path` set, preserving existing caller behaviour

### Requirement: Bundle size cap
The system SHALL reject or truncate context bundles exceeding 64 KB of JSON before parsing.

#### Scenario: Bundle within size limit
- **WHEN** the bundle JSON is ≤ 64 KB
- **THEN** it SHALL be accepted and parsed in full

#### Scenario: Bundle exceeds size limit
- **WHEN** the bundle JSON is > 64 KB
- **THEN** milliways SHALL log a warning and truncate to the first 64 KB before parsing, never returning a hard error that blocks the dispatch
