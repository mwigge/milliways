## ADDED Requirements

### Requirement: Security context block is injected into every agent session at open time

When `agent.open` is called, the daemon SHALL query `mw_security_findings` for CRITICAL and HIGH severity findings with `status=active` (not resolved, not accepted) relevant to the workspace root. If any findings exist, a synthetic priming message SHALL be prepended to the session's conversation before the first user turn. The block SHALL be capped at 2000 tokens; if findings exceed the cap, the most-severe are included first and a truncation note appended.

#### Scenario: Active CRITICAL findings exist — block is injected

- **WHEN** `agent.open` is called for a session in a workspace containing 2 CRITICAL findings
- **THEN** the session's opening context SHALL include a block:
  ```
  [security context — 2 active findings in this workspace]
  CRITICAL  CVE-2024-12345  github.com/foo/bar@v1.2.0  fixed in v1.2.1
            Arbitrary code execution via crafted input in Bar.Parse()
  CRITICAL  CVE-2024-67890  github.com/baz/qux@v0.9.1  no fix available
            Path traversal in Qux.ReadFile() — no upstream patch as of 2026-05-05
  ─────────────────────────────────────────────────────────────
  Run /scan to refresh · milliwaysctl security list for full report
  ```
- **AND** this block SHALL appear before the user's first prompt in the conversation history sent to the provider

#### Scenario: No CRITICAL or HIGH findings — no injection

- **WHEN** `agent.open` is called and the workspace has only MEDIUM/LOW findings (or none)
- **THEN** no `[security context]` block is prepended
- **AND** the session opens exactly as before this change

#### Scenario: Security context suppressed per session

- **WHEN** `agent.open` is called with `security_context: false` in the request body
- **THEN** no security context block is injected regardless of findings
- **AND** this is used by the `/parallel` navigator pane and other non-code sessions

#### Scenario: Initial scan not yet complete at session open

- **WHEN** a session is opened within the first 10 seconds after daemon start (scan still running)
- **THEN** security context injection is skipped for that session
- **AND** a debug-level log entry is written: `[security] skipping injection — initial scan not complete`

#### Scenario: Block truncated when findings exceed token cap

- **WHEN** the workspace has 30 CRITICAL/HIGH findings that would exceed 2000 tokens
- **THEN** findings SHALL be included in order of severity (CRITICAL first, then HIGH), filling up to 2000 tokens
- **AND** the block SHALL end with: `[truncated — showing N of 30 findings. Run /scan for full report]`

### Requirement: Security context is consistent across all providers in a parallel group

When `/parallel` dispatches N slots, all slots SHALL receive the same security context block (same findings, same truncation cutoff) so that agents are working from a consistent security baseline.

#### Scenario: Parallel dispatch injects identical security context to all slots

- **WHEN** `parallel.dispatch` opens 3 slots for a workspace with 2 CRITICAL findings
- **THEN** all 3 slots SHALL have the same `[security context]` priming message
- **AND** the block SHALL be generated once and reused (not re-queried per slot)
