# Changelog

## v1.0.63

Security control-plane hardening, documentation honesty, TUI polish, and dead-code
removal (from a full review).

### Security & correctness
- Brokered shells now restore the shim dir on the child PATH, so a subshell can't
  run its subtree unprotected (S1).
- Fixed subscribe-stream / statusSubscribers / observability-loop leaks on
  disconnect (S4/S5); RPC methods reject malformed params (S10); `security scan
  --layers` enumerates real workspace files instead of hardcoded paths (S3); status
  conveys the in-process-firewall vs PATH-shim enforcement mechanism (S2); workflow
  save + audit-row errors are logged (S7/S9/S11).

### Docs
- Reworded quota-rotation to match the code (rotate + `/retry`, not
  auto-re-dispatch — that's `/takeover`); qualified the skill-rules keyword-injection
  claim (the consuming CLIs do the matching); added the "when MemPalace is
  configured" caveat to project memory.

### TUI
- Unified to one brand palette (navy #0b1220 + brand hexes) across chat chrome, a
  custom brand chroma style for code panels, and the shipped terminal theme;
  animated braille streaming spinner; inline markdown (bold/code/italic); a
  persistent role gutter and turn separators.

### Removed
- Deleted dead/unwired packages that duplicated live behavior: internal/memory,
  internal/plugins, internal/rules (skill-rules loader), session/store.go
  (test-only), and a dead cmd/milliways/run.go. (The live paths — event-history
  NDJSON, the toolkit-bundle injection, same-process briefing — are unchanged.)


All notable changes to milliways. Follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions.

---

## [1.0.62] — 2026-06-16

### Changed
- **rs-llmctl 1.6.2** — `install-gpu-server` now installs rs-llmctl 1.6.2, which adds Gemma4 GGUF tokenizer support. Gemma4 models previously failed at startup with `unsupported tokenizer model 'gemma4'`; the engine now builds the tokenizer directly from GGUF vocab and BPE merges using Metaspace (▁) whitespace escaping.

---

## [1.0.58] — 2026-06-10

### Fixed
- **Pipe-drain ordering in claude/gemini/pool runners** — `stderrWg.Wait()` is now called before `cmd.Wait()` so buffered stderr lines (session-limit/error detection) are never dropped.
- **Stdout stream errors surfaced** — `scanner.Err()` / read errors after the stdout loop are now pushed as error events in all four runners (claude, gemini, copilot, pool) instead of silently truncating output.
- **OTel CVEs GO-2026-4985 and GO-2026-4394** — bumped `otlpmetrichttp`, `otlptracehttp`, and `otel/sdk` to v1.43.0; govulncheck reports no reachable vulnerabilities.
- **`ErrInvalidParams` misuse in JSON-RPC handlers** — backend failures ("pantry not available", store errors, scan failures) in `security_methods.go` now return `ErrInternal` (-32603) instead of `ErrInvalidParams` (-32602), letting clients distinguish bad requests from daemon faults.
- **Prompt enrichment E2BIG guard** — `enrichPrompt` enforces a 64 KB total cap, dropping codegraph then palace layers before truncating the toolkit bundle; the user's prompt is always delivered intact.
- **TUI data race on `l.sess`** — `drainStream`'s goroutine now reads `l.sess` via `isActiveSess` (RLock) while the main goroutine writes under `sessMu.Lock()`, eliminating the race. Also adds a `default: slog.Debug(...)` case to the event-type switch for unknown event types.
- **Tool-gate fail-safe on daemon death mid-approval** — `milliwaysctl tool-gate` now returns `deny` when the daemon connection drops while a tool call is pending human review; the pre-existing fail-open behavior on initial dial failure is preserved.

### Changed
- **`*.bak` files excluded from git** — added `*.bak` to `.gitignore`; committed stale `.bak` files removed.

### Docs
- **CHANGELOG backfill** — added entries for v1.0.53–v1.0.57.

---

## [1.0.57] — 2026-06-10

### Added
- **Interactive tool-permission approval** — Claude runs in headless `--print` mode, so every `Bash`/`Edit`/file tool call is now routed through a `PreToolUse` hook (`milliwaysctl tool-gate`) that blocks on the user's approval before allowing execution; the approval prompt appears in the milliways TUI. Infrastructure failures fail open; mid-decision daemon death fails safe (deny).
- **CodeGraph auto-init** — `milliwaysctl codegraph init` / `/repoinit` bootstraps a CodeGraph index for the current project; the TUI also auto-inits on `/repoindex` if no index is detected.

### Changed
- **Claude default timeout raised to 6 h** — `CLAUDE_TIMEOUT` now defaults to 6 hours; agentic coding sessions no longer expire on long-running tasks. Timeout and SIGKILL events produce a legible `"claude: timed out after Xm"` / `"claude: killed"` message instead of a raw exit-code error.

### Fixed
- **CI proxy flake hardening** — Go module fetches in release CI now use `|`-separated `GOPROXY` fallback list so transient proxy errors retry through `goproxy.io` and `direct` instead of failing the build.

---

## [1.0.56] — 2026-06-09

### Fixed
- **E2BIG "claude not installed" on large toolkit bundles** — prompts are now delivered to CLI agents (claude, codex, gemini) via stdin (`--stdin` / `--input -`) rather than as an inline argv argument; toolkit bundle + user prompt combinations that exceeded `MAX_ARG_STRLEN` (128 KiB) would silently fail with a confusing "not installed" error.
- **Honest runner start errors** — `runnerStartHint` now surfaces the real underlying error (binary not found, permission denied, etc.) alongside install hints, replacing the generic "command not found" message.

---

## [1.0.55] — 2026-06-09

### Added
- **`milliwaysctl security install-scanner`** — installs all supported security scanners (`osv-scanner`, `gitleaks`, `govulncheck`, `semgrep`) automatically via a single command; each installer is platform-aware and skips already-installed tools.

> **Note:** release CI assets for v1.0.55 were not published due to a transient CI failure; the v1.0.56 binary includes these changes.

---

## [1.0.54] — 2026-06-09

### Added
- **AgentOps observability panel** — real-time per-provider metrics (request count, token rates, cost, latency histograms) rendered in the observe panel; forwarded via OpenTelemetry to SigNoz or any OTLP-compatible backend.
- **SigNoz integration guide** — `docs/signoz.md` documents how to run a local SigNoz instance and point milliways at it with `OTEL_EXPORTER_OTLP_ENDPOINT`.

### Fixed
- **Go PATH fallback for scanner installs** — when `go` is not on `PATH` at hook-invocation time, the scanner install logic now probes `~/go/bin`, `~/.local/bin`, and `/usr/local/go/bin` before giving up, fixing "go: command not found" on fresh macOS installs.
- **Always-visible observe panel sections** — performance and provider sections are now shown even when metrics are zero, eliminating the confusing "empty panel" state on first launch.

---

## [1.0.53] — 2026-06-09

### Added
- **`milliwaysctl security install-scanner`** *(preview)* — groundwork for one-command scanner installation; included in this release as a preview (superseded by the full implementation in v1.0.55).

### Fixed
- **Go PATH fallback for scanners** *(landed in v1.0.53)* — see v1.0.54 note; the fix shipped across two patch releases.

---

## [1.0.52] — 2026-06-07

### Changed
- **`/install-local-gpu-server` model replacement** — now detects the currently configured local model (`MILLIWAYS_LOCAL_MODEL`) and compares it against the hardware-recommended pick, printing clear before/after messaging: "currently running X, replacing with Y" or "already running the recommended model".

### Fixed
- **Model swap on GPU server install** — `install_local.sh`'s `reuse_existing_or_pick_port` previously reused any reachable local server regardless of which model it served, silently discarding the requested `MODEL_ALIAS` and keeping the old model running forever. It now only reuses when the live server already serves the requested model; otherwise it picks a fresh port and installs the recommended model, replacing the active `local.env` configuration.

---

## [1.0.51] — 2026-06-06

### Added
- **Toolkit bundle injection** — on startup, milliways scans `CLAUDE.md` and `.claude/{skills,rules,agents,commands}/*.md` and injects the bundle as `<toolkit_context>` into every Claude prompt, giving the agent access to project context and skills automatically.
- **Claude local-coder MCP bridge** — when `MILLIWAYS_LOCAL_ENDPOINT` is reachable, Claude receives `--mcp-config` and `--append-system-prompt` wiring in a `local_code` MCP tool backed by the on-device LLM; Claude delegates implementation tasks to the local model and falls back if it's too slow or low quality.
- **`milliwaysctl mcp-localcoder`** — minimal JSON-RPC 2.0 MCP server (stdio) that proxies `local_code` tool calls to the local OpenAI-compatible endpoint.
- **Apple Silicon / mlx-lm install** — `/install-local-mlx` (`milliwaysctl local install-mlx`) installs `mlx-lm`, sets the endpoint, and prints Gemma 4 12B startup commands; recommended over llama.cpp on Apple Silicon.
- **`/install-local-gpu-server --accel metal`** — Metal acceleration flag supported; Apple Silicon VRAM budget raised to 70% (vs 45% for discrete GPU) to account for unified memory.
- **Gemma 4 model catalog** — three Gemma 4 entries added: `Gemma-4-E4B` (9.7 GB, 128K context), `Gemma-4-12B` (7.7 GB, 256K), `Gemma-4-26B-A4B` (17.5 GB MoE, 256K). All include Ollama pull tags and MLX community repo hints.
- **Ollama integration hints** — Gemma 3/4 catalog entries carry an `Ollama` field; `/install-gpu-server` and `/install-local-mlx` now print `ollama pull <tag>` as a zero-config Metal-accelerated alternative.

### Changed
- **K/V cache quantization** — llama-server launcher now includes `--cache-type-k q8_0 --cache-type-v q4_0`, reducing KV cache VRAM by ~60% and enabling significantly longer context windows.
- **Model-aware context sizing** — `contextSizeForVRAMAndModel` computes actual KV cache headroom (VRAM − model size − 1.5 GB overhead) using calibrated per-token cost with cache quant applied; 12 GB Apple gets 65 K (was 16 K), 32 GB Apple gets 131 K (was 65 K), 48 GB+ gets 262 K.
- **Context window scaling** — `contextSizeForVRAM` was replaced; context is now selected from actual residual VRAM rather than VRAM tiers alone.

### Fixed
- **Arrow key handling** — ESC + CSI sequences (↑↓←→) now detected before `waitReadable` poll via `br.Buffered() > 0`; buffered bytes were already in userspace but the kernel fd showed empty, causing arrow keys to abort the stream.
- **Observability frame compactness** — performance section hidden when `RequestCount == 0`; idle frames stay within the 19-line limit.
- **Quota time-to-limit window** — `formatTimeToLimit` now falls back to `parseQuotaWindow(q.Window)` when `UsedDaily` is nil, fixing incorrect 24 h display for 1 h quota windows.
- **Metrics failure rate cap** — `observeRequest` + `observeOperationDuration` added to claude, codex, and gemini runners so failure rate can never exceed 100%.
- **Scanner installs** — gitleaks updated to v8 module path, osv-scanner to v2 binary name, pool corrected to `npm install` (not `go install`).

---

## [1.0.9] — 2026-05-11

### Added
- **Panel deck** — on WezTerm startup, milliways now opens a home-hero-dashboard layout: navigator pane on the left, per-provider content panes alongside; falls back to single-pane chat when not in WezTerm.
- **Side panels** — TUI side-panel framework (SPS-1 through SPS-9): cost, routing, system, snippets, OpenSpec, diff/change-set, multi-model compare panels; toggled with `Ctrl+O`, cycled with `h`/`l` or `←`/`→`.
- **/upgrade command** — `milliwaysctl upgrade` and `/upgrade` slash command; shell-level upgrade for deb, rpm, pacman, raw binary, and MilliWays.app tiers.
- **OTLP HTTP exporter** — when `MILLIWAYS_OTEL_ENDPOINT` is set, traces and metrics are exported via `otlptracehttp`/`otlpmetrichttp` instead of stdout.
- **Local model catalog** — `milliwaysctl local setup-model`, `download-model`, `list-models`, `install-server`, `install-swap`; Devstral-Small-2505 as default; HuggingFace mirror and HF_TOKEN support.
- **Code review pipeline** — `ReviewRunner`: Go-orchestrated multi-model code review with git integration, CodeGraph injection, MemPalace transport, edit format, and lint.
- **Parallel panels** — `/parallel` dispatches a prompt to multiple providers simultaneously; live comparison view with consensus rendering.
- **Security scanner** — OSV-scanner integration behind `/security` toggle; schema V7 `SecurityStore`.
- **Briefing block** — handed-off context rendered inline on runner switch.

### Changed
- **Version fallback** — local (non-release) builds now report `dev` instead of a stale hardcoded version number.
- **WezTerm config** — updated provider abbreviations (Copilot → `Cp`, Gemini → `G`, Pool → `P`), refreshed per-client colour themes, `gui-startup` maximises the initial window.

### Fixed
- **Linux package version stamp** — the v1.0.8 Linux package incorrectly shipped a v1.0.6 binary; the build pipeline is now verified to stamp `$VERSION` via ldflags on every release.
- Various chat stream, terminal editing, pool, daemon, and local-model fixes (see commits v1.0.5–v1.0.8).

---

## [1.0.4] — 2026-05-09

### Added
- **Markdown heading hierarchy** — `#` through `####` headings now render with bold ANSI styling; H1 gets a `═` underline rule and H2 gets a `─` rule so document structure is immediately visible in the chat stream.
- **Heading prefix** — `⏺` bullet used for all thinking status lines and action lines (Edited / Ran), matching Claude Code's visual idiom.

### Changed
- **Agent badge** — removed bracket wrapping; badges now render as filled colour pills (` minimax ▶`) using lighter, more vibrant 256-colour backgrounds against catppuccin-mocha.
- **Thinking lines** — prefix changed from `[agent] … reasoning` to `⏺ reasoning`; one shared glyph with tool-call action lines for visual consistency.
- **Code panel borders** — bumped from `238m` to `243m` so the box is visible against dark terminal backgrounds; panel label brightened from `2;250m` (dim) to `252m`.
- **Response wrapping** — wrap width changed from `termWidth - 2` to `termWidth - 4`, adding a two-character breathing margin on each side.
- **Quote prefix** — blockquote `>` markers now rendered in dim `244m` colour to distinguish them from body text.
- **Action line glyphs** — `✎ Edited` and `▶ Ran` unified to `⏺ Edited` / `⏺ Ran`.

### Fixed
- **Thinking text display** — switched from in-place `\r` overwrite (which placed thinking on the wrong line after cursor advanced during agentic tool loops) to sequential `\n`-terminated status lines with a codeHighlighter flush before each write, ensuring thinking always starts at column 0.
- **Secondary thinking clearing** — `thinkingActive` flag now checked before every data write, not only on `firstData`; thinking from subsequent tool-call turns no longer bleeds into the response stream.
- **`chunk_end` state reset** — `firstData` and `thinkingActive` now reset on every `chunk_end` so multi-turn agentic sessions start each turn with a clean display state.
- **Ghost prompt rows** — `clearPromptLocked` now uses `max(stored_rows, content_rows)` when deciding how many rows to clear, fixing double-display of the input prompt when the buffer grew between redraws.
- **Post-submit prompt echo** — on Enter, the wrapped readline input area is cleared and reprinted as a single `\n`-terminated line, making the full submitted text selectable as one string in the scrollback.
- **Deck card label truncation** — `padPlain` now uses `displayWidth()` + rune-aware truncation; multi-byte glyphs (`▶`, `…`) no longer cause overflow past the right card border.
- **Table column overflow** — table renderer iteratively shrinks the widest column until the total table width fits `termWidth`; cells are truncated via `truncateANSIVisible` to match.
- **Cost/token hint removed from chat stream** — `($0.0003 · ...)` inline hint removed; stats are in the observability panel and window title.
- **Pool tool approval** — `--unsafe-auto-allow` added to `pool exec` args so tool calls proceed without interactive confirmation in headless mode.
- **Truecolor syntax highlighting** — `terminal16m` formatter selected automatically when `COLORTERM=truecolor` or `TERM=xterm-kitty`; falls back to `terminal256`.
- **Default highlight theme** — changed from `monokai` to `catppuccin-mocha`.
- **`truncateANSIVisible` CSI parser** — fixed CSI sequence parsing that left `38;2;R;G;B` truecolor codes as visible text when the `[` introducer was mistaken for a final byte.

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
