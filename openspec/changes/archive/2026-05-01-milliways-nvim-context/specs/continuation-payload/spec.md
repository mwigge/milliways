## MODIFIED Requirements

### Requirement: Continuation payload includes condensed editor context
The continuation-payload builder SHALL accept an optional `editorcontext.Bundle` and, when present, append a condensed editor-context section to the payload rendered to the incoming kitchen. The section SHALL be capped at 500 tokens and raw LSP messages truncated to their first line.

#### Scenario: Editor context section rendered in payload
- **WHEN** a continuation payload is built with an editor context bundle containing buffer, cursor, LSP diagnostics, and git state
- **THEN** the payload SHALL include a section with file path and filetype, cursor position and scope, truncated LSP error lines, and git branch and dirty status

#### Scenario: Condensed section stays within token cap
- **WHEN** the editor context bundle contains a large number of LSP diagnostics
- **THEN** the condensed section SHALL be truncated to remain within 500 tokens, preserving the most important signals first

#### Scenario: Payload without editor context unchanged
- **WHEN** a continuation payload is built with no editor context bundle
- **THEN** the payload format SHALL be identical to the pre-L2 baseline with no placeholder or empty section added

#### Scenario: Long LSP message truncated
- **WHEN** an LSP diagnostic message spans multiple lines
- **THEN** only the first line of the message SHALL appear in the condensed section
