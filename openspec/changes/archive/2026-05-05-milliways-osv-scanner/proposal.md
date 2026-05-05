## Why

milliways operates across multiple dependency ecosystems (Go modules, Rust crates, pnpm packages) and works inside user workspaces that may contain any of these. Today, neither the agents nor the user have any shared, up-to-date view of known vulnerabilities in the active workspace's dependencies. Adding `google/osv-scanner` as a daemon-managed shared security layer — parallel to how MemPalace provides shared memory — means every provider automatically receives relevant CVE context before reviewing code, and the user can query or act on findings without leaving the chat.

## What Changes

- New daemon background service runs `osv-scanner` via its Go library on workspace startup, on git lock-file change detection, and on-demand via `/scan`.
- New pantry tables (`mw_security_findings`, `mw_security_accepted_risks`) store scan results, dedup by CVE+package+version, and track user-accepted risks.
- New security context injection layer: before each agent session starts on a workspace path, high- and critical-severity findings for that path's manifest are prepended as a `[security context]` block — identical in mechanism to the MemPalace baseline injection in `milliways-parallel-panels`.
- New `security_scan` built-in tool available in the agentic tool loop so any HTTP runner can trigger a fresh scan mid-session.
- New `/scan` slash command for on-demand scanning with output to the chat session.
- New `milliwaysctl security` sub-commands: `list`, `show <cve-id>`, `accept <cve-id>`.

## Capabilities

### New Capabilities

- `osv-scan-runner`: Daemon-managed scan engine. Wraps the `github.com/google/osv-scanner` Go library (no subprocess). Discovers lockfiles in the workspace root (go.sum, Cargo.lock, pnpm-lock.yaml, requirements.txt, pdm.lock). Stores structured findings in pantry. Deduplicates by CVE ID + package + installed version. Runs on daemon start, on lockfile change, and on-demand.
- `security-context-injection`: Shared context layer across all providers. On session open, queries pantry for CRITICAL/HIGH findings relevant to the session's workspace path. Injects a `[security context]` block as a synthetic priming message. Mirrors the MemPalace baseline injection mechanism from `milliways-parallel-panels`. Users can suppress injection per-session with `--no-security-context`.
- `security-commands`: User-facing surface: `/scan` slash command, `security_scan` agentic tool, and `milliwaysctl security list/show/accept` CLI commands.

### Modified Capabilities

_(none — no existing spec-level requirements change)_

## Impact

- **New dependency**: `github.com/google/osv-scanner` Go library (library-mode, no subprocess, no network call at scan time — uses bundled OSV database snapshots or local cache).
- **New package**: `internal/security/` — scanner, store, injector
- **Pantry / SQLite**: two new tables (`mw_security_findings`, `mw_security_accepted_risks`)
- **Daemon startup**: lockfile discovery + initial scan on first start; subsequent scans triggered by inotify/kqueue file watch on lockfiles (using existing OS file-watch pattern if present, otherwise poll every 5 minutes)
- **Tools registry**: `security_scan` tool added to `internal/tools/registry.go` alongside Bash, Read, WebFetch
- **Chat commands**: `/scan` added to the slash command dispatch table
- **milliwaysctl**: new `security` sub-command tree
- **MILLIWAYS_WORKSPACE_ROOT**: the scan is jailed to the workspace root, consistent with all other tool security guardrails
