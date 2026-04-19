# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- **Two-Active-Memory architecture**: orchestrator is aware of project context (git repo, CodeGraph symbols, MemPalace palace) while maintaining conversation memory. Project context is detected automatically from cwd on startup.
  - `internal/project/` package: `ProjectContext` detection for git repo root, CodeGraph index, MemPalace palace
  - `internal/bridge/` package: project memory bridge with topic extraction, palace search, citation creation, and cross-palace resolution
  - TUI project status bar: shows project name, palace drawer/room/wing counts, CodeGraph symbol count
  - TUI commands: `/project` (project info), `/repos` (accessed repos), `/palace` (palace status/search), `/codegraph` (codegraph status/search)
  - Repo context tracking: segments and turns record `repo_context`, `repos_accessed`, and `project_refs` fields
  - Cross-palace citations: `palace://<palace_id>[/<wing>]/<room>/<drawer_id>` citation syntax with read-only enforcement

- Kitchen switching commands: `/switch <kitchen>`, `/back`, `/stick`, and `/kitchens`.
- Headless kitchen switching with the `--switch-to <kitchen>` CLI flag.
- Continuous routing with sommelier re-evaluation at user-turn boundaries.
- Auto-switch visibility in the process map, including trigger, tier, and reversal hints.
- Block telemetry summaries with session metadata visibility.
- Smoke test coverage for user-switch and exhaustion scenarios.

### Changed

- Conversation state now uses the MemPalace substrate by default.
- Legacy SQLite conversation storage remains available with `--use-legacy-conversation`.

### Migration

- Existing conversations auto-migrate on first run when the new substrate is enabled.
- Set `MILLIWAYS_MEMPALACE_MCP_CMD` to enable the MemPalace substrate.
- Use `--use-legacy-conversation` to opt out of migration and stay on legacy storage.

### Dependencies

- Requires the `mempalace-milliways` fork with conversation primitives.
- See `mempalace-milliways/FORK.md` for fork-specific documentation.
