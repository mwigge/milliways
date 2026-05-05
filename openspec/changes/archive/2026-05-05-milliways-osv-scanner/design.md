## Context

milliways is a Go 1.25 binary with pantry-managed SQLite, an `internal/tools/` registry that HTTP runners invoke in agentic loops, and a MemPalace MCP client for shared knowledge graph access. The workspace is jailed to `MILLIWAYS_WORKSPACE_ROOT` via `tools/safety.go`. The `milliways-parallel-panels` change introduces a MemPalace baseline injection mechanism (prime each session with prior findings before the user prompt). The OSV scanner reuses the same injection point to add a security context block alongside the knowledge-graph context.

`github.com/google/osv-scanner` is a Go library as well as a CLI. Using it as a library avoids a subprocess, gives structured `[]model.Vulnerability` output directly, and means the scan runs in-process in the daemon without shelling out. The OSV database is queried over HTTPS against `api.osv.dev` by the library; results are cached in pantry.

## Goals / Non-Goals

**Goals:**
- Daemon manages the scan lifecycle; no user setup beyond having a lockfile in the workspace.
- CRITICAL and HIGH findings automatically reach every agent session as security context (opt-out, not opt-in).
- Findings survive daemon restarts and accumulate over time — the store is the source of truth, not the last scan.
- Accepted risks are first-class: a `milliwaysctl security accept` marks a CVE as accepted with a reason and expiry, suppressing it from context injection and summaries.
- Zero subprocess — scanner runs as a Go library call in the daemon.

**Non-Goals:**
- Auto-patching or dependency upgrades.
- SAST / static analysis (osv-scanner is SCA only — manifest + lockfile vulnerability matching).
- Scanning arbitrary network URLs or container images.
- Per-provider or per-session scan isolation — the scan result is shared across all sessions in the workspace.
- Running inside the sandboxed tool environment (the scan is daemon-level, not model-invoked by default; the `security_scan` tool is an explicit opt-in surface for agentic runners).

## Decisions

### D1: Use google/osv-scanner as a Go library, not a CLI subprocess

**Decision**: Import `github.com/google/osv-scanner/v2/pkg/osvscanner` and call `osvscanner.DoScan` directly from the daemon.

**Alternatives considered**:
- *exec osv-scanner binary*: Requires users to install a separate binary; breaks the zero-setup model. Also harder to test.
- *Call OSV API directly*: Would require reimplementing lockfile parsing for 5+ ecosystems. The library does this already.

**Rationale**: The library is Apache-2.0, actively maintained by Google, and provides structured `[]models.Vulnerability` output. One new `require` line in go.mod.

### D2: Findings stored in pantry SQLite, not MemPalace

**Decision**: New `mw_security_findings` table in pantry (CVE ID, package, installed version, fixed-in version, severity, ecosystem, first seen, last seen, scan source lockfile). MemPalace is NOT used for finding storage.

**Alternatives considered**:
- *Store in MemPalace KG*: MemPalace is a semantic knowledge graph optimised for fuzzy retrieval; the security findings need exact CVE-ID lookups, deduplication by (cve_id, package, version) primary key, and accepted-risk joins. SQLite is the right store.

**Rationale**: pantry owns all structured state; MemPalace owns cross-session narrative context. The injection layer reads from pantry and formats findings as text for the context block — same pattern as the MemPalace injector in parallel-panels.

### D3: Context injection at session-open time, not at prompt time

**Decision**: The security context block is injected as a synthetic system-adjacent priming message when `agent.open` is called, before any user turn. It is not re-injected on every subsequent prompt in the session.

**Rationale**: Injecting on every prompt wastes tokens and creates noise. The security posture of a workspace doesn't change turn-to-turn. A single priming message at session start is consistent with how MemPalace baseline injection works and how system prompts function.

**Suppression**: `agent.open` accepts an optional `security_context: false` field to suppress injection for sessions where it's irrelevant (e.g., a pure chat session with no code context).

### D4: Lockfile change detection via polling, not inotify/kqueue

**Decision**: The daemon polls lockfiles in the workspace root every 5 minutes by comparing mtime. If a lockfile's mtime is newer than the last scan timestamp for that file, a rescan is triggered.

**Alternatives considered**:
- *inotify/kqueue*: More responsive, but adds OS-specific code paths and a file-watch goroutine. The 5-minute polling window is acceptable — users doing active dependency changes will also have `/scan` for immediate on-demand rescan.

**Rationale**: Simpler, portable, and the 5-minute window matches typical developer workflows. `/scan` covers the impatient case.

### D5: Severity filter for context injection — CRITICAL and HIGH only

**Decision**: Only CRITICAL and HIGH severity findings are injected into the security context block. MEDIUM and LOW are available via `/scan` and `milliwaysctl security list` but do not reach agents automatically.

**Rationale**: Injecting all findings for a large workspace (e.g., a Node project with hundreds of MEDIUM vulns) would overwhelm the context window. CRITICAL + HIGH is the signal that actually affects code review decisions.

### D6: `security_scan` tool is rate-limited to one scan per 60 seconds per session

**Decision**: When an agent calls the `security_scan` tool, the daemon enforces a 60-second cooldown per session handle. Calls within the cooldown return the cached result rather than triggering a fresh OSV API call.

**Rationale**: The OSV API has no published rate limit but is a free public service. Agentic loops can call tools in tight loops; the cooldown prevents accidental abuse without blocking the feature.

## Risks / Trade-offs

- **OSV library size**: The osv-scanner library pulls in its own transitive deps (lockfile parsers for each ecosystem). The milliways binary will grow. Acceptable — milliways already bundles a WezTerm fork; a few MB for security coverage is a reasonable trade.
- **OSV API availability**: Scan results depend on `api.osv.dev`. Offline / air-gapped environments get no findings. The daemon logs a WARN and skips injection rather than blocking session open. Document this limitation.
- **False positives / accepted-risk UX**: Users may accept risks they shouldn't. The `accept` command requires a reason string and an expiry date (max 1 year) to prevent indefinite suppression.
- **Context window cost**: Injecting a `[security context]` block adds tokens to every session open in a vulnerable workspace. Mitigated by CRITICAL/HIGH filter and a 2000-token cap on the injected block.

## Migration Plan

1. Schema version bump in `pantry/schema.go` (new tables only, additive).
2. First daemon start after upgrade: initial scan runs in background goroutine; session opens are not blocked if scan is still running (context injection is skipped until first scan completes).
3. Roll back: remove `internal/security/`, revert schema bump, drop two tables manually. No existing behavior changes.

## Open Questions

- **OSV library version**: The library's v2 API is still evolving. Pin to a specific tag and add a `// pinned: review before upgrade` comment in go.mod.
- **Token cap on context block**: 2000 tokens covers ~20 CRITICAL findings with descriptions. If a workspace has more, truncate with a note. Is 2000 the right cap or should it be configurable?
- **Parallel-panels interaction**: When `/parallel` dispatches 3 slots, all 3 receive the same security context block. This is correct (same workspace) but triples the token cost for security context. Acceptable since it's a priming message, not repeated per-turn.
