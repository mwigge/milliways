# Changelog

All notable changes to milliways. Follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions.

---

## [1.0.3] — 2026-05-02

### Added
- **`/upgrade` command** — upgrade milliways to the latest release from inside the chat REPL or via `milliwaysctl upgrade`.  Detects the original install tier (deb/rpm/pacman/binary/macOS) and performs the appropriate upgrade.  Flags: `--check` (print current vs latest, no install), `--yes` (skip prompt), `--version <tag>` (pin a specific version).  Support scripts (`upgrade.sh`) are refreshed as part of the upgrade.
- **`scripts/upgrade.sh`** — shell-level upgrade orchestrator covering all tiers: native packages (apt/dnf/pacman), raw binary replacement (atomic `.upgrade.tmp` dance), MilliWays.app via `open`, and support-script refresh.
- **`scripts/smoke-upgrade.sh`** — 11-scenario smoke test suite for the upgrade path.  Linux package-manager scenarios (UG-5/6/7) run inside Docker containers on amd64 hosts, matching the existing `smoke-linux-install.sh` pattern.
- **`install.sh`** now bundles `upgrade.sh` alongside the other support scripts so it is available for all fresh installs.
- **OTLP HTTP exporter support** — when `MILLIWAYS_OTEL_ENDPOINT` is set (e.g. `http://localhost:4318`), spans and metrics are forwarded to any OTLP-compatible backend (Jaeger, Tempo, Grafana Cloud) via `otlptracehttp`/`otlpmetrichttp`.  Stdout exporters remain the fallback when the env var is absent.
- **`MILLIWAYS_OTEL_ENDPOINT`** and **`MILLIWAYS_OTEL_PROTOCOL`** added to the daemon `localenv` allowlist so users can persist the observability target via `/login`.

---

## [1.0.1] — 2026-05-01

### Added
- Native Linux packages: `.deb` (Debian/Ubuntu), `.rpm` (Fedora/RHEL), `.pkg.tar.zst` (Arch) built via fpm and attached to every release. All three binaries (`milliways`, `milliwaysd`, `milliwaysctl`) plus support scripts are in each package; installs to `/usr/bin`.
- `install.sh` now tries the native package first, falls back to raw binary, then source build.
- CI `package-smoke-linux` job verifies each package format installs cleanly on its target distro on every push.

### Fixed
- Node.js 20 deprecation warning in CI — opted into Node.js 24 runtime via `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`.

---

## [1.0.0] — 2026-05-01

### Added
- **Certified Linux installation** on Ubuntu 24.04, Fedora 41, and Arch Linux (all verified in CI on a native linux/amd64 host — no emulation).
- `scripts/smoke-linux-install.sh`: full install certification harness covering binary download, source-build fallback, and a deep CLI smoke (local-server bootstrap, gemini + copilot install, session `--switch-to` handoff).
- `scripts/build-linux-amd64.sh` + `local/docker/build-linux/Dockerfile`: reproducible linux/amd64 build environment based on Debian bookworm with Go 1.25 and cross-compile toolchain.
- CI job `install-smoke-linux` on `ubuntu-latest` (native amd64): runs the full smoke harness on every push.
- **Human-friendly runner error messages** across all CLI runners (claude, codex, copilot, gemini, pool): exit errors now include the CLI's own last stderr line (`codex exited (code 2) — <actual error>`); start errors include the install command; Bearer tokens in stderr are automatically redacted.
- **`/path` command** for persistent PATH override for all runner subprocesses — useful when milliways is launched from a GUI app with a minimal PATH: `/path <value>` sets, `/path reset` clears.
- **wezterm tab title** via `format-tab-title` Lua event: tab strip shows live milliways status (`milliways · claude · $0.02 session · 1200→340 tok`) instead of the process name.
- **Terminal title/tab lifecycle**: thinking… → streaming… → stats on every response; ring rotation flash `↻ codex` in background tabs; resets to ready on error.

### Fixed
- Release CI: `gh release create` was failing when the release already existed (manually created before CI ran) — binaries were never uploaded. Now uses `gh release upload --clobber` when the release exists.
- Source-build fallback in `install.sh`: when installing via `curl | bash` with no local checkout, the script now clones the repo to a temp dir rather than looking for `./cmd/milliways` in the user's cwd (which is never there).
- `install.sh`: `GOTOOLCHAIN=auto` + `GOSUMDB=sum.golang.org` so the source fallback works on Fedora, which ships Go 1.24 and sets `GOSUMDB=off` in its system `go.env`.
- `install.sh`: architecture fallback — if the native-arch binary 404s, tries the amd64 binary (runs under Rosetta / QEMU).
- `waitForStream` in `agents.go`: replaced CPU-burning busy-wait (`for {}` spin) with a channel notification (`streamReady chan struct{}` closed by `AttachStream`).
- `persistLocalEnv`: empty value now fully removes the key from `local.env` instead of writing `KEY=`.
- `config.setenv` on the daemon: empty value now calls `os.Unsetenv` rather than `os.Setenv("KEY", "")`.
- `milliwaysctl local install-server`: fixed script-lookup path to use `os.Executable()` so the script is found regardless of working directory.
- `internal/kitchen/generic.go`: added `copilot` to the CLI allowlist.

---

## [0.9.9] — 2026-05-01

### Fixed
- `writeOSCTitle` read `$TMUX` directly from the process environment, making it impure and unsafe under `t.Parallel()`. Now accepts `tmuxEnv string` as a parameter; `setTermTitle` passes the value at the call site.
- Terminal tab/window title was not reset after a runner error event — tab could show "streaming…" indefinitely. Now resets to the ready state on every `err` event.
- `rpc/client.rs`: `ClientError::io` and `ClientError::protocol` constructors were private, preventing downstream crates from building synthetic errors. Now `pub`.
- Subscription reader task log level downgraded `warn` → `debug` — disconnect errors are expected on normal agent shutdown.

### Tests
- tmux DCS passthrough test uses direct parameter instead of `t.Setenv` (safe for `t.Parallel()`).
- tmux DCS frame structure now fully asserted (doubled ESC, correct BEL/ST terminator placement).
- Added `"\033\\"` (DCS string terminator) case to `TestSanitiseOSC_StripsControlChars` — documents that the DCS terminator injection vector is neutralised.

---

## [0.9.8] — 2026-05-01

### Added
- **Live terminal tab and window titles.** The tab and title bar update as you work:

  | State | Tab | Window |
  |---|---|---|
  | Switch to runner | `● claude · sonnet-4-6` | `milliways · claude` |
  | Prompt sent | unchanged | `milliways · claude · thinking…` |
  | First token | unchanged | `milliways · claude · streaming…` |
  | Response done | unchanged | `milliways · claude · $0.0218 session · 1200→340 tok` |
  | Ring rotation | `↻ codex` | `milliways · rotating → codex` |
  | Exit | `milliways` | `milliways` |

- Window title shows **cumulative session cost** so total spend is visible at a glance without adding up per-response amounts.
- Model name in tab title (`● claude · sonnet-4-6`) so adjacent tabs are visually distinct at different model tiers.
- `sanitiseOSC()` strips `\033`, `\007`, `\r`, `\n` before any title interpolation — defence-in-depth against control character injection.
- tmux DCS passthrough wrapping when `$TMUX` is set.

### Fixed
- `rpc/client.rs subscribe()`: `sidecar.flush().await.ok()` silently discarded flush errors, causing subscriptions to produce zero events with no error surfaced. Now propagated with `?`.

---

## [0.9.7] — 2026-05-01

### Added
- **`/local-endpoint <url>`** — point the local runner at any OpenAI-compatible backend at runtime; persists across daemon restarts.
- **`/local-temp <0.0–2.0|default>`** — sampling temperature for the local runner, injected into the OpenAI payload per-request.
- **`/local-max-tokens <N|off>`** — cap reply length for the local runner.
- **`/local-hot on|off`** — toggle llama-swap hot mode (models always resident) vs standby (TTL eviction).
- All four commands shown in `/help` under "Local-model tuning" and wired into tab completion.
- Current temp and max_tokens values shown in `/model local` settings dump.
- `MILLIWAYS_LOCAL_TEMP` and `MILLIWAYS_LOCAL_MAX_TOKENS` added to the daemon `allowedSetenvKeys`.

### Fixed
- `artifacts.go` python3 subprocess: added 30-second execution timeout and 10-second AST validation timeout (was unbounded — goroutine leak on hang).
- `artifacts.go` python3 subprocess: stripped ambient environment — API keys and cloud credentials no longer accessible to generated scripts.
- `handleReview`: git diff wrapped in `<tool_result>` tags to prevent prompt injection via committed content.
- `/help`: fixed duplicate "Session:" heading; added `/metrics`, `/opsx-archive`, `/opsx-validate`.
- README: fixed `/takeover-ring` → `/ring`; corrected `MILLIWAYS_LOCAL_TEMPERATURE` → `MILLIWAYS_LOCAL_TEMP`.
- Rust `rpc/client.rs`: added `#[must_use]` to all public `Result`-returning functions.
- Archived completed OpenSpec changes: `milliways-http-kitchen`, `milliways-kitchen-parity`, `milliways-nvim-context`, `decommission-repl-into-daemon`.

---

## [0.9.6] — 2026-04-30

### Added
- **Dynamic model lists** — `/model` fetches live model lists from provider APIs where an API key is available; falls back to a curated `knownModels` list for OAuth-authenticated CLIs where the token is scoped for the CLI, not the developer API.
- `modelCache` with 1-hour TTL and background refresh on startup (`RefreshAsync`).
- Per-provider fetchers: Anthropic, OpenAI, Gemini (`X-Goog-Api-Key` header), MiniMax, GitHub Copilot (OAuth token from `~/.copilot/` or `~/.config/github-copilot/`).
- TOCTOU fix in `modelCache.Models`: re-checks inside write lock before setting `fetching: true`.

---

## [0.9.4] — 2026-04-30

### Added
- **Gen AI OpenTelemetry spans** following semantic conventions: `gen_ai.client.operation` parent spans and `gen_ai.execute_tool` child spans for all tool calls across all runners.
- **Live metrics dashboard** — `/metrics` or `milliwaysctl metrics --watch`: 5-column table showing token usage and cost across 1 min / 1 hour / 24 h / 7 d / 30 d windows, auto-refreshes every 5 seconds.
- `/ring` command: configure auto-rotation ring, show current ring and exhausted runners.

### Fixed
- `renderTurnsWithBudget`: last user turn identified by position (`lastUserIdx`) not content comparison.
- `switchAgent` data race on `ring`/`exhausted` — protected by `ringMu` mutex.
- `autoRotate` called `switchAgent` from drainStream's goroutine — moved to main goroutine via `rotateCh` channel.

---

## [0.9.3] — 2026-04-30

### Added
- **Artifact commands** available in all runners: `/pptx`, `/drawio`, `/review`, `/compact`, `/clear`.
- `/pptx <topic>`: asks the active runner to write a python-pptx script, AST-validates it against an import allowlist, executes it, saves `.pptx` in cwd.
- `/drawio <topic>`: generates draw.io XML, saves `.drawio` in cwd.
- `/review [focus]`: sends `git diff HEAD` to the active runner for code review.
- `/compact` / `/clear`: summarise or wipe the session turn log.
- Python AST validator via subprocess — blocks `eval`, `exec`, `open`, `__import__` and all non-allowlisted imports.

### Fixed
- `enrichWithPalace`: palace content XML-escaped before injection into `<project_memory>` tags.
- Workspace jail in `handleGrep`/`handleGlob`: symlink traversal blocked via `EvalSymlinks` + re-validate.

---

## [0.9.2] — 2026-04-30

### Added
- **API key persistence** via `~/.config/milliways/local.env` (mode `0600`). Keys set via `/login` survive daemon restarts.
- `/login <runner>` prompts interactively for API keys or prints CLI auth steps.

---

## [0.9.0] — 2026-04-30

### Added
- **Tab completion** for all slash commands and runner names.
- Client-native slash command pass-through: runner-specific commands (copilot `/diff`, pool `/mode`, etc.) forwarded directly to the CLI.
- `chatCtlAliases` map connecting `/opsx-*`, `/install-*`, `/local-*` to `milliwaysctl` subcommands.

---

## [0.8.x] — 2026-04-30

### Added
- **Project memory (MemPalace)**: `enrichWithPalace` injects relevant memories as a `<project_memory>` XML block before each user prompt.
- **Session takeover**: structured briefing carried across runner switches, 4096-byte budget, last user turn always included.
- Shared `turnLog` across all runners — `/switch` passes context to the new runner automatically.

---

## [0.6.x – 0.7.x] — 2026-04-19 – 2026-04-26

### Added
- Interactive chat loop replacing the removed internal REPL, backed by daemon RPC over Unix domain socket.
- Landing zone with numbered runner shortcuts (`/1`–`/7`), auth status marks, daemon connectivity probe.
- `milliwaysctl` ops verbs: `metrics`, `local`, `opsx`, `install`, `bridge`, `context-render`, `observe-render`.
- wezterm `AgentDomain` with per-pane reconnect watcher (FSM: Connected → Disconnected → Reconnecting → GaveUp), banner injection via `Pane::perform_actions`.
- `milliways-term` wezterm fork: `Leader+/` palette, status bar, sleep/wake badge, per-runner colour coding.

---

## [0.4.x] — 2026-04-26 – 2026-04-28

### Added
- Initial public releases: Go daemon (`milliwaysd`), CLI client (`milliways`), ops tool (`milliwaysctl`).
- Runners: claude, codex, copilot, gemini, local (llama.cpp), minimax, pool (Poolside ACP).
- Agentic tool loop (`RunAgenticLoop`) with Bash, Read/Write/Edit, Grep/Glob, WebFetch for HTTP runners.
- SQLite metrics store with raw/hourly/daily/weekly/monthly rollup tiers.
- `install.sh` one-liner for macOS and Linux; `scripts/install_local.sh` and `scripts/install_local_swap.sh` for local model setup.
- CI: build, test, release matrix for `linux_amd64`, `linux_arm64`, `darwin_amd64`, `darwin_arm64`.
