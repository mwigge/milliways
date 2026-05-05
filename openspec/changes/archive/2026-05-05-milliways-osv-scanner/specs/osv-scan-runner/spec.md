## ADDED Requirements

### Requirement: Daemon discovers and scans lockfiles in the workspace root on startup

On daemon start, the security scanner SHALL discover all supported lockfiles under `MILLIWAYS_WORKSPACE_ROOT` and initiate an OSV scan in a background goroutine. Supported lockfiles: `go.sum`, `Cargo.lock`, `pnpm-lock.yaml`, `package-lock.json`, `requirements.txt`, `pdm.lock`. Session opens SHALL NOT be blocked waiting for the initial scan to complete.

#### Scenario: Lockfiles found and scanned on startup

- **WHEN** the daemon starts and `MILLIWAYS_WORKSPACE_ROOT` contains a `go.sum` and a `Cargo.lock`
- **THEN** the scanner SHALL call the osv-scanner library for each lockfile within 10 seconds of daemon start
- **AND** SHALL write resulting findings to `mw_security_findings` with `scan_source` set to the lockfile path
- **AND** SHALL log at slog INFO: `[security] initial scan complete: N findings (C critical, H high)`

#### Scenario: No lockfiles in workspace

- **WHEN** `MILLIWAYS_WORKSPACE_ROOT` contains no supported lockfiles
- **THEN** the scanner SHALL log at slog DEBUG: `[security] no lockfiles found in workspace — skipping scan`
- **AND** SHALL NOT error or affect daemon startup

#### Scenario: OSV API unreachable during scan

- **WHEN** the scanner calls the osv-scanner library and `api.osv.dev` is unreachable
- **THEN** the scanner SHALL log at slog WARN: `[security] OSV scan failed: <error> — context injection disabled until next successful scan`
- **AND** SHALL NOT write partial findings
- **AND** SHALL NOT block session opens

### Requirement: Scanner re-runs when a lockfile's mtime changes

The daemon SHALL poll lockfiles in the workspace every 5 minutes. If any lockfile's mtime is newer than its last-scanned timestamp in `mw_security_findings`, the scanner SHALL trigger a rescan for that lockfile.

#### Scenario: Lockfile updated between polls

- **WHEN** `go.sum` is updated (e.g., `go get` was run) between two poll cycles
- **THEN** the scanner SHALL detect the mtime change on the next poll
- **AND** SHALL rescan `go.sum` within 5 minutes of the change
- **AND** SHALL upsert findings (insert new CVEs, update `last_seen` for existing ones)

#### Scenario: No lockfile changes between polls

- **WHEN** no lockfile mtimes have changed since the last scan
- **THEN** the scanner SHALL not initiate a rescan
- **AND** SHALL not write to `mw_security_findings`

### Requirement: Findings are deduplicated and stored in pantry

The `mw_security_findings` table SHALL use `(cve_id, package_name, installed_version, ecosystem)` as a composite unique key. On upsert, `last_seen` is updated and severity/fixed_in are refreshed. Findings are never deleted automatically — they are soft-expired when a lockfile no longer references the package (status set to `resolved`).

#### Scenario: Same CVE found in two consecutive scans

- **WHEN** a scan finds CVE-2024-12345 in `github.com/foo/bar@v1.2.0`
- **AND** a subsequent scan finds the same CVE
- **THEN** `mw_security_findings` SHALL contain exactly one row for that (cve_id, package, version)
- **AND** `last_seen` SHALL be updated to the latest scan timestamp

#### Scenario: Package updated to fixed version

- **WHEN** `go.sum` is updated and `github.com/foo/bar` is now at `v1.2.1` (the fixed version)
- **AND** the rescan runs
- **THEN** the finding for `v1.2.0` SHALL have its `status` set to `resolved`
- **AND** the resolved finding SHALL NOT appear in context injection

#### Scenario: Accepted risk suppresses a finding

- **WHEN** a row exists in `mw_security_accepted_risks` for (cve_id, package_name) with a non-expired `expires_at`
- **THEN** that finding SHALL be excluded from context injection and from `milliwaysctl security list` output unless `--include-accepted` flag is passed
