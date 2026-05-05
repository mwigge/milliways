## ADDED Requirements

### Requirement: /scan slash command triggers an on-demand scan and prints findings to chat

The `/scan` slash command SHALL trigger an immediate OSV scan of the workspace, wait for it to complete (max 30 seconds), and print a formatted findings summary to the active chat session.

#### Scenario: /scan with findings

- **WHEN** the user runs `/scan` in the chat
- **THEN** the system SHALL run a fresh OSV scan of all lockfiles in the workspace
- **AND** SHALL print output in the format:
  ```
  [scan] go.sum · Cargo.lock scanned — 3 findings
  CRITICAL  CVE-2024-12345  github.com/foo/bar@v1.2.0  → fix: v1.2.1
  HIGH      CVE-2024-55555  openssl@3.0.1              → fix: 3.0.2
  MEDIUM    CVE-2024-99999  libc@2.35                  → no fix
  ─────────────────────────────────────────────────────────────
  Run: milliwaysctl security accept <cve-id> --reason "..." --expires 2026-08-01
  ```
- **AND** findings SHALL be ordered CRITICAL → HIGH → MEDIUM → LOW

#### Scenario: /scan with no findings

- **WHEN** the user runs `/scan` and no vulnerabilities are found
- **THEN** the system SHALL print: `[scan] go.sum · Cargo.lock scanned — no findings ✓`

#### Scenario: /scan timeout

- **WHEN** the OSV API does not respond within 30 seconds
- **THEN** the system SHALL print: `[scan] timed out after 30s — OSV API may be unreachable`
- **AND** SHALL NOT update `mw_security_findings` with partial results

#### Scenario: /scan while scan already in progress

- **WHEN** the user runs `/scan` and a background scan is already running
- **THEN** the system SHALL print: `[scan] scan in progress — will print results when complete`
- **AND** SHALL attach to the in-progress scan rather than starting a second one

### Requirement: security_scan agentic tool available to HTTP runners

The `security_scan` tool SHALL be registered in `internal/tools/registry.go` alongside Bash, Read, and WebFetch. When called by an agent in an agentic loop, it returns the current list of active findings as structured JSON. The tool is rate-limited to one fresh scan per 60 seconds per session; within the cooldown, cached results are returned.

#### Scenario: Agent calls security_scan tool

- **WHEN** an HTTP runner's agentic loop calls `security_scan {}` (no arguments)
- **THEN** the tool SHALL return a JSON result:
  ```json
  {
    "scanned_at": "2026-05-05T12:00:00Z",
    "findings": [
      {"cve_id": "CVE-2024-12345", "package": "github.com/foo/bar", "installed": "v1.2.0", "fixed_in": "v1.2.1", "severity": "CRITICAL", "summary": "..."}
    ],
    "accepted_risks": 0,
    "from_cache": false
  }
  ```

#### Scenario: security_scan called within cooldown period

- **WHEN** `security_scan` is called twice within 60 seconds in the same session
- **THEN** the second call SHALL return the cached result with `"from_cache": true`
- **AND** SHALL NOT trigger a new OSV API call

#### Scenario: security_scan in workspace with no lockfiles

- **WHEN** `security_scan` is called in a workspace with no supported lockfiles
- **THEN** the tool SHALL return `{"findings": [], "scanned_at": "...", "from_cache": false}` and exit cleanly

### Requirement: milliwaysctl security sub-commands provide CLI access to findings

The `milliwaysctl security` sub-command tree SHALL provide `list`, `show`, and `accept` commands for managing security findings outside the chat session.

#### Scenario: milliwaysctl security list

- **WHEN** `milliwaysctl security list` is run
- **THEN** it SHALL print a table of all active (non-resolved, non-accepted) findings ordered by severity descending, with columns: CVE ID, Package, Version, Fixed In, Severity, First Seen
- **AND** SHALL exit with status 0 even when no findings exist (prints "no active findings")
- **AND** `--include-accepted` flag SHALL include accepted-risk rows with an `[accepted]` marker

#### Scenario: milliwaysctl security show <cve-id>

- **WHEN** `milliwaysctl security show CVE-2024-12345` is run
- **THEN** it SHALL print the full finding detail: CVE description, affected package, installed version, fixed-in version, severity, CVSS score if available, OSV link, and first/last seen timestamps

#### Scenario: milliwaysctl security accept <cve-id> --reason <text> --expires <date>

- **WHEN** `milliwaysctl security accept CVE-2024-12345 --reason "false positive — not reachable in our usage" --expires 2026-08-01` is run
- **THEN** a row SHALL be inserted into `mw_security_accepted_risks` with the CVE ID, package, reason, and expiry date
- **AND** the finding SHALL no longer appear in context injection or `milliwaysctl security list` (unless `--include-accepted`)
- **AND** the command SHALL print: `[accepted] CVE-2024-12345 suppressed until 2026-08-01`

#### Scenario: milliwaysctl security accept with expiry beyond 1 year

- **WHEN** `--expires` is set to a date more than 365 days from today
- **THEN** the command SHALL print: `expiry cannot exceed 1 year from today (max: <date>)` and exit with status 1

#### Scenario: milliwaysctl security accept for unknown CVE

- **WHEN** `milliwaysctl security accept CVE-9999-00000 ...` is run for a CVE not in `mw_security_findings`
- **THEN** the command SHALL print: `CVE-9999-00000 not found in findings — run /scan first` and exit with status 1
