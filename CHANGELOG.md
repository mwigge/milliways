# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.5.0] - 2026-04-30

This release decommissions the legacy `internal/repl/` line-reader and consolidates all runners into `internal/daemon/runners/`. Tool execution becomes a core capability of every HTTP-based runner. Multi-review pass added security guardrails around the agentic tool loop (workspace containment, SSRF blocking, prompt-injection mitigation) before merge.

### Added
- `internal/tools/safety.go` — workspace-root containment + dotfile credential denylist for file Read/Write/Edit/Grep/Glob; SSRF blocking for WebFetch (loopback / RFC1918 / link-local / cloud-metadata IPs rejected pre-resolve and on every redirect). Defaults: workspace = process cwd; loopback blocked. `MILLIWAYS_WORKSPACE_ROOT` and `MILLIWAYS_TOOLS_ALLOW_LOOPBACK=1` opt-overrides.
- `internal/daemon/runners/openai_stream.go` — shared OpenAI-compatible chat-completions streaming helper used by minimax + local. Reassembles tool-call argument fragments by index, surfaces stream truncation as `ErrIncompleteStream`, surfaces oversized SSE lines as `ErrSSELineTooLarge`, folds empty-tool-name calls back to the model as recoverable errors, synthesises tool_call_id when missing.
- `internal/daemon/runners/subprocess_env.go` — `safeRunnerEnv()` filters the env passed to claude/codex/copilot/gemini/pool subprocesses to a safelist (PATH/HOME/USER/SHELL/TERM/LANG/LC_*/TMPDIR/XDG_* + per-CLI auth keys). Closes the codex env-leak path that exposed MINIMAX_API_KEY / MILLIWAYS_LOCAL_API_KEY / AWS_* / GH_TOKEN to a prompt-injected codex session.
- Tool result wrapping: `<tool_result tool="...">...</tool_result>` markers around all tool output (32KB cap), system prompts in HTTP runners declare them untrusted data — mitigates prompt-injection via tool fold-back.
- `classifyDispatchError` differentiates user cancel (-32008), timeout (-32009), incomplete stream (-32011), oversized SSE (-32012), and generic backend errors (-32010).
- `scrubBearer` redacts `Bearer xxx` tokens from upstream proxy error bodies before they reach the user-facing stream / logs.
- Local runner agentic tool loop: `internal/daemon/runners/local.go` now wires `RunAgenticLoop` + `tools.NewBuiltInRegistry()` by default. System prompt prepended. `MILLIWAYS_LOCAL_TOOLS=off` opts out (chat-only mode for debugging / model comparison). Per-runner `http.Client` (no `http.DefaultClient` leak).
- `milliwaysctl local` subcommand tree — install-server, install-swap, list-models, switch-server, download-model, setup-model. Wraps the existing `scripts/install_local.sh` and `scripts/install_local_swap.sh` and adds new logic for HuggingFace GGUF download and llama-swap config registration. Lets users complete the full local-model bootstrap without leaving the milliways terminal.
- `milliwaysctl opsx` subcommand tree — list, status, show, archive, validate. Thin wrapper around the openspec CLI; surfaces as `/opsx-list`, `/opsx-status`, etc. via the milliways-term palette. (apply / explore deferred — they need orchestration with daemon agent.send.)
- `Leader + /` palette in milliways-term — opens a `wezterm` `InputSelector` (fuzzy filter) populated with curated `milliwaysctl` invocations. Picking a complete verb dispatches in a new tab; verbs that take args fall through to a prefilled `PromptInputLine`; a free-form escape hatch covers any ctl call. Adding a new ctl subcommand keeps it callable via the free-form path; the curated list is edited to surface it in the picker.
- `internal/daemon/runners/tooling.go` — shared agentic tool-loop helper (`RunAgenticLoop`) for HTTP-based runners. Drives assistant→tool→assistant cycles with `internal/tools/` Registry, a 10-turn safety cap, and `error: …` fold-back for tool failures, unknown tools, and malformed args.
- Daemon `gemini` runner — CLI shell-out (`gemini -p <prompt> -y`), stderr session-limit detection.
- Daemon `pool` runner — Poolside AI CLI shell-out (`pool exec --unsafe-auto-allow`), stderr session-limit detection.
- Daemon `minimax` agentic tool loop — first user of `RunAgenticLoop`. System prompt, multi-turn tool execution, `MINIMAX_TOOLS=off` env var to disable.
- Daemon `claude` `rate_limit_event` surfacing — claude CLI's in-band rate-limit signals now flow through the daemon stream as structured events.
- Daemon `claude` / `codex` / `local` / `copilot` stderr session-limit detection — surfaces as structured err events before chunk_end so takeover-ring can react.
- Daemon `codex` Zscaler / corporate-proxy block detection — guides the user to open ChatGPT in a browser to approve the security prompt.
- Daemon `claude` cache_read_tokens / cache_write_tokens — now surfaced in chunk_end (parsed previously but never emitted).

### Changed
- Daemon `local` runner pivoted from Ollama-native (`/api/chat` at port 11434, `OLLAMA_BASE_URL`/`OLLAMA_MODEL`) to OpenAI-compatible (`/chat/completions` at port 8765, `MILLIWAYS_LOCAL_ENDPOINT`/`MILLIWAYS_LOCAL_MODEL`). The daemon was the outlier — every other piece of the local-model stack (REPL runner, milliwaysctl local, install scripts) targets the OpenAI-compatible path. Bearer auth via `MILLIWAYS_LOCAL_API_KEY`.

### Fixed
- **SECURITY** Bash tool no longer logs the raw command string at INFO (only length + sha256 prefix); cwd pinned to workspace root. Closes a credential-leak vector where model-generated commands containing env-var-interpolated secrets would land in the daemon log.
- **SECURITY** File `read`/`write`/`edit` refuse paths outside `MILLIWAYS_WORKSPACE_ROOT` (default = process cwd) and refuse credential-bearing paths even inside the workspace (`~/.ssh/`, `~/.aws/`, `~/.gnupg/`, `~/.kube/`, `~/.netrc`, `~/.docker/config.json`, `~/.config/milliways/local.env`, `~/.config/anthropic/auth.json`, `~/.config/gh/hosts.yml`).
- **SECURITY** WebFetch refuses non-`http(s)` schemes, loopback / RFC1918 / link-local hosts, and cloud-metadata endpoints (`169.254.169.254`, `metadata.google.internal`); CheckRedirect re-validates every redirect target so `200 → 302 → IMDS` escapes are closed. Cloud-metadata blocking is unconditional even when `MILLIWAYS_TOOLS_ALLOW_LOOPBACK=1`.
- Daemon `gemini` and `pool` runners now actually register in `internal/daemon/agents.go` (they shipped with full test suites in earlier commits but the dispatch table missed them).
- `probe.go` now probes all 7 chat runners (was 4); `probeCopilot` fixed to test the actual `copilot` binary `RunCopilot` invokes (was testing `gh copilot` — probe/runtime mismatch).
- `cmd/milliwaysctl/milliways.lua`: removed `MILLIWAYS_REPL=1` env var and `default_prog = milliways` (would have recursively syscall-execed milliways-term inside every new wezterm tab once `--repl` removal landed). `default_prog` now `$SHELL`; agent panes open via `milliwaysctl open --agent <name>`.
- `version` bumped 0.4.13 → 0.5.0 to reflect the BREAKING `--repl` removal. Migration interceptor catches `milliways --repl` and `MILLIWAYS_REPL=1` with a curated migration message before cobra emits a raw `unknown flag`.
- All HTTP runner err paths now push `chunk_end` before returning so clients waiting on a terminal frame per `agent.send` do not hang.
- Codex `sawProxyBlock` `sync.Mutex` + `bool` → `sync/atomic.Bool` (single field; no need for separate mutex).
- `withMinimaxToolRegistry` moved out of production code into `minimax_export_test.go` (was a textbook `testing` import in prod anti-pattern).
- `internal/kitchen/adapter/codex.go` defaults `--sandbox workspace-write --ask-for-approval never` when the user hasn't set them via `cfg.Args`. Recent codex defaults to `read-only`/`on-request` in `exec --json` mode and silently refused tool execution; this restores tool execution by default while preserving user overrides.
- Daemon `codex` runner gets the same `--sandbox workspace-write --ask-for-approval never` defaults via a new `buildCodexCmdArgs` helper.

### Removed
- **BREAKING**: `internal/repl/` package deleted (~30 files: legacy line-reader UI, shell, pane, status bar, commands, plus 8 runner files now living in `internal/daemon/runners/`).
- **BREAKING**: `--repl` CLI flag removed; `MILLIWAYS_REPL` env var no longer recognised. `milliways` (no flags) launches milliways-term/wezterm; for one-shot prompts use `milliways "<prompt>"`. The legacy line-reader was already labelled deprecated for removal in v0.6.0.
- The launcher's "Fallback: run `milliways --repl`" startup error messages have been replaced with pointers to `milliwaysd` log troubleshooting.

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
