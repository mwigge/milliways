# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- `milliwaysctl local` subcommand tree — install-server, install-swap, list-models, switch-server, download-model, setup-model. Wraps the existing `scripts/install_local.sh` and `scripts/install_local_swap.sh` and adds new logic for HuggingFace GGUF download and llama-swap config registration. Lets users complete the full local-model bootstrap without leaving the milliways terminal.
- `Leader + /` palette in milliways-term — opens a `wezterm` `InputSelector` (fuzzy filter) populated with curated `milliwaysctl` invocations. Picking a complete verb dispatches in a new tab; verbs that take args fall through to a prefilled `PromptInputLine`; a free-form escape hatch covers any ctl call. Adding a new ctl subcommand keeps it callable via the free-form path; the curated list is edited to surface it in the picker.
- `internal/daemon/runners/tooling.go` — shared agentic tool-loop helper (`RunAgenticLoop`) for HTTP-based runners. Drives assistant→tool→assistant cycles with `internal/tools/` Registry, a 10-turn safety cap, and `error: …` fold-back for tool failures, unknown tools, and malformed args.

### Fixed
- `internal/kitchen/adapter/codex.go` defaults `--sandbox workspace-write --ask-for-approval never` when the user hasn't set them via `cfg.Args`. Recent codex defaults to `read-only`/`on-request` in `exec --json` mode and silently refused tool execution; this restores tool execution by default while preserving user overrides.

## [0.4.14] - 2026-04-28

### Added
- `pool` runner — Poolside AI CLI (`pool exec`) integrated as a first-class milliways runner; supports `--model` and `--mode` flags, session-limit detection, and `pool login` / `pool logout`
- `gemini` runner — Google Gemini CLI (`gemini -p`) integrated as a first-class milliways runner; supports `--model` flag and session-limit detection via `resource_exhausted` / quota patterns
- `/pool`, `/gemini` shorthands — equivalent to `/switch pool` and `/switch gemini`
- `/pool-model <m>`, `/pool-mode <m>` — set pool model/mode
- `/gemini-model <m>` — set Gemini model
- `?` shortcut — typing `?` at the milliways prompt shows the milliways shortcuts reference (runners, key commands, takeover, shell)

## [0.4.13] - 2026-04-28

### Added
- `/takeover [runner]` command — generates a structured handoff briefing from the current session and switches runners; the new runner receives current task, progress summary, files changed, key decisions, and next step
- `/takeover-ring <r1,r2,...>` command — configures a priority rotation ring that persists across session saves; milliways auto-rotates to the next runner when any runner signals a session limit
- TTY transcript sidecar — every session now writes a full ANSI-stripped plain-text log (`.log` alongside `.json`); briefing generator reads the complete session history, not just the 20-turn ring buffer
- Session limit detection — all four runners (claude, codex, minimax, copilot) emit a sentinel signal when they hit a context window, quota, or rate-limit; the REPL intercepts and auto-rotates when a ring is configured
- Status bar ring indicator — shows runner position in ring (`●claude 1/3`) when rotation ring is active
- MemPalace snapshot on takeover — when MemPalace is configured, the handoff briefing is written asynchronously to `handoff/<timestamp>` in the active palace
- Rotation cap — auto-rotation halts when all ring members hit their limits on the same turn, surfacing a clear error instead of looping

## [0.4.12] - 2026-04-28

### Added
- Rich `● ToolName  detail` display for Codex tool events, matching Claude's format (`● Shell  cmd`, `● Edit  ~/path`, `● Thinking  summary`)
- Home dir paths abbreviated to `~/...` in Codex tool output

### Changed
- Banner labels ("no session", "runners:") now render in pearl white instead of dim grey

## [0.4.11] - 2026-04-28

### Fixed
- Double status bar / cursor corruption: removed scroll-region status bar that was fighting with readline; status now renders inline only
- Runner shorthands (`/claude`, `/codex`, `/minimax`, `/copilot`, `/local`) now switch immediately

### Changed
- MiniMax accent color → purple
- Codex accent color → amber/orange
- Codex and Copilot print a settings summary when switched to
- Removed all REPL/TUI language from docs, comments, and user-visible strings

## [0.4.10] - 2026-04-28

### Added
- Interactive arrow-key model picker: `/model` with no args opens an inline picker
- Tab completion for model IDs

### Fixed
- MiniMax image API JSON decode error (`failed_count` / `success_count` are strings, not ints)
- Status bar version was hardcoded as `0.1.0`

### Changed
- Phosphor green color scheme (`#4db51f` on black) replacing Gruvbox

## [0.4.9] - 2026-04-27

### Added
- MilliWays.app: native macOS terminal on a patched wezterm
- Sleep/wake badge (⚡) in status bar; resume modal via `Ctrl+Space r`
- `/help` lists all runner shorthand aliases
- curl one-liner install with remote binary download and local source fallback
- `wezterm-milliways` patch repo with macOS 26 crash fix (`catch_unwind` in `SpawnQueue`)

### Fixed
- Window closing immediately when set as `default_prog` — fixed via `MILLIWAYS_REPL=1` env var
- Missing title bar / resize buttons — `window_decorations = 'TITLE | RESIZE'`

## [0.4.8] - 2026-04-26

### Added
- Wezterm terminal integration (`cmd/milliwaysctl/milliways.lua`)
- Status bar with runner name, quota bars, session cost, wake badge
- Leader keybindings (`Ctrl+Space`): open pane, switch runner, resume, context overlay

### Added

- **Two-Active-Memory architecture**: orchestrator is aware of project context (git repo, CodeGraph symbols, MemPalace palace) while maintaining conversation memory. Project context is detected automatically from cwd on startup.
  - `internal/project/` package: `ProjectContext` detection for git repo root, CodeGraph index, MemPalace palace
  - `internal/bridge/` package: project memory bridge with topic extraction, palace search, citation creation, and cross-palace resolution
  - Terminal status bar: shows project name, palace drawer/room/wing counts, CodeGraph symbol count
  - Terminal commands: `/project` (project info), `/repos` (accessed repos), `/palace` (palace status/search), `/codegraph` (codegraph status/search)
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
