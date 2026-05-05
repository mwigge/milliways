## 1. Dependency and Schema

- [ ] 1.1 Add `github.com/google/osv-scanner/v2` to `go.mod` with a `// pinned: review before upgrade` comment; run `go mod tidy`
- [ ] 1.2 Add `mw_security_findings` table DDL to `internal/pantry/schema.go` (version bump): columns `id`, `cve_id`, `package_name`, `installed_version`, `fixed_in_version`, `severity`, `ecosystem`, `summary`, `scan_source`, `status`, `first_seen`, `last_seen`; unique index on `(cve_id, package_name, installed_version, ecosystem)`
- [ ] 1.3 Add `mw_security_accepted_risks` table DDL: columns `id`, `cve_id`, `package_name`, `reason`, `accepted_at`, `expires_at`; unique index on `(cve_id, package_name)`
- [ ] 1.4 Implement `SecurityStore` in `internal/pantry/security.go`: `UpsertFinding`, `ListActive`, `ListAll`, `GetByCVE`, `MarkResolved`, `InsertAcceptedRisk`, `ListAcceptedRisks`
- [ ] 1.5 Write unit tests for `SecurityStore` in `internal/pantry/security_test.go`

## 2. OSV Scan Runner

- [ ] 2.1 Create `internal/security/` package; define `Finding`, `ScanResult` types in `internal/security/types.go`
- [ ] 2.2 Implement `DiscoverLockfiles(root string) []string` in `internal/security/scanner.go` — walk workspace root, return paths matching supported lockfile names
- [ ] 2.3 Implement `Scan(ctx context.Context, lockfiles []string) (ScanResult, error)` wrapping `osvscanner.DoScan`; map library output to `[]Finding`
- [ ] 2.4 Implement `Runner` struct in `internal/security/runner.go`: holds `SecurityStore` reference, `lastScanned map[string]time.Time`, runs initial scan in background goroutine on `Start(ctx)`
- [ ] 2.5 Implement `Runner.PollLoop`: ticks every 5 minutes, compares lockfile mtimes against `lastScanned`, triggers rescan on change
- [ ] 2.6 Implement `Runner.UpsertFindings`: calls `SecurityStore.UpsertFinding` for each result, calls `SecurityStore.MarkResolved` for packages no longer in the lockfile
- [ ] 2.7 Register `Runner.Start` in daemon startup sequence (after pantry init, in background goroutine)
- [ ] 2.8 Write unit tests for `DiscoverLockfiles`, `Runner.UpsertFindings` (mock scanner), and mtime-change detection

## 3. Security Context Injection

- [ ] 3.1 Create `internal/security/injector.go` with `BuildContextBlock(findings []Finding, tokenCap int) string` — formats the `[security context]` block, truncates to token cap, appends truncation note when needed
- [ ] 3.2 Implement severity ordering in `BuildContextBlock`: CRITICAL first, then HIGH; within tier, sort by `first_seen` descending
- [ ] 3.3 Wire injection into `agent.open` handler: query `SecurityStore.ListActive(severity: [CRITICAL, HIGH])` after `Runner` has completed at least one scan; prepend result of `BuildContextBlock` as synthetic priming message
- [ ] 3.4 Add `security_context bool` field to `agent.open` request type in `internal/rpc/types.go`; default true; skip injection when false
- [ ] 3.5 Wire `parallel.dispatch` to generate context block once and pass to all slots (not re-queried per slot)
- [ ] 3.6 Write unit tests for `BuildContextBlock` covering: empty findings, truncation at cap, severity ordering, accepted-risk exclusion

## 4. /scan Slash Command

- [ ] 4.1 Add `/scan` to slash command dispatch table in `cmd/milliways/chat.go`
- [ ] 4.2 Implement handler: call `Runner.ScanNow(ctx, 30s timeout)` (new method that runs a scan synchronously and returns `ScanResult`); deduplicate with in-progress scan if one exists
- [ ] 4.3 Implement `RenderScanSummary(result ScanResult) string` in `internal/security/render.go` — produce the formatted findings table with severity ordering and milliwaysctl hint line
- [ ] 4.4 Add `/scan` to the slash command picker help text

## 5. security_scan Agentic Tool

- [ ] 5.1 Implement `SecurityScanTool` in `internal/tools/security.go` implementing the `Tool` interface
- [ ] 5.2 Implement 60-second per-session rate limit: store `lastScanTime map[string]time.Time` in the tool, keyed by session handle; return cached result with `from_cache: true` within cooldown
- [ ] 5.3 Register `SecurityScanTool` in `internal/tools/registry.go`
- [ ] 5.4 Write unit tests for `SecurityScanTool` covering: successful scan, cooldown cache hit, no lockfiles case

## 6. milliwaysctl security Commands

- [ ] 6.1 Create `cmd/milliwaysctl/security.go` with `securityCmd` Cobra command tree
- [ ] 6.2 Implement `security list` sub-command: call `SecurityStore.ListActive` via daemon RPC, render table with columns CVE ID / Package / Version / Fixed In / Severity / First Seen; support `--include-accepted` flag
- [ ] 6.3 Implement `security show <cve-id>` sub-command: call `SecurityStore.GetByCVE`, render full detail block
- [ ] 6.4 Implement `security accept <cve-id> --reason <text> --expires <date>` sub-command: validate CVE exists, validate expiry ≤ 365 days, call `SecurityStore.InsertAcceptedRisk`
- [ ] 6.5 Add `security.list`, `security.show`, `security.accept` RPC handlers to daemon
- [ ] 6.6 Write unit tests for command flag validation (expiry cap, unknown CVE guard)

## 7. Integration Tests

- [ ] 7.1 Add integration test: daemon start with a `go.sum` fixture containing a known-CVE package → verify `mw_security_findings` populated after initial scan
- [ ] 7.2 Add integration test: `agent.open` after scan → verify priming message contains `[security context]` block
- [ ] 7.3 Add integration test: `agent.open` with `security_context: false` → verify no priming message injected
- [ ] 7.4 Add integration test: `milliwaysctl security accept` → verify finding excluded from subsequent `security list` output and context injection
