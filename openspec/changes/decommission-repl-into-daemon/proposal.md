## Why

`internal/repl/` is the legacy line-reader UI plus eight runner files (claude, codex, gemini, copilot, local, minimax, opsx, pool). The user-facing surface is being replaced by milliways-term (wezterm-based); the `--repl` flag is already labeled "Removal scheduled in v0.6.0" in `cmd/milliways/launcher.go:243`. A parallel `internal/daemon/runners/` package exists but has significant feature drift and is missing several runners entirely. Removing `internal/repl/` today would silently lose minimax tool calling (just landed), gemini/opsx/pool routes, and substantial features in claude/codex/local. This change converges all runners into the daemon path, ensures every HTTP-based runner has uniform tool execution, and then deletes the REPL package and its launcher fallback.

## What Changes

- Port REPL→daemon for `claude`, `codex`, `local` (drift sync of ~200–400 lines each).
- Port REPL→daemon for `minimax` including the agentic tool loop (`internal/tools/` Registry), system prompt, SSE think-filter, and stream-integrity checks.
- Create new daemon runners for `gemini`, `opsx`, `pool` (do not exist in `internal/daemon/runners/` today).
- Wire `tools.Registry` (built-in set: bash, file read/write/edit, grep, glob, web fetch) into all HTTP-based runners (`minimax`, `copilot`, `local`). CLI-based runners (`claude`, `codex`, `gemini`) inherit tool execution from their underlying CLI.
- Port REPL runner tests into the daemon package; preserve coverage.
- **BREAKING**: Delete `internal/repl/` package entirely.
- **BREAKING**: Remove `--repl` CLI flag, `MILLIWAYS_REPL` env var, and the deprecation notice.
- Remove "Fallback: run `milliways --repl`" messages from `cmd/milliways/launcher.go` (lines 120, 128, 136, 234, 243).
- Update `README.md` and `CLAUDE.md` (project root) references to `internal/repl/` and `--repl`.
- Default `--sandbox workspace-write --ask-for-approval never` in the codex kitchen adapter (`internal/kitchen/adapter/codex.go`) so codex actually executes its tools when invoked non-interactively via `exec --json`. User-supplied flags via `cfg.Args` continue to override.

## Capabilities

### New Capabilities

- `daemon-runners`: The contract for runner implementations under `internal/daemon/runners/` — what each runner must implement, the dispatch lifecycle, history/quota/auth surfaces, and how outputs flow back through the daemon to clients.
- `runner-tool-execution`: The agentic tool-loop contract for HTTP-based runners — how `tools.Registry` is invoked, how tool calls are streamed, how tool results are folded back into the conversation, and the maximum-turn safety bound.

### Modified Capabilities

<!-- None — the four existing specs (jobs-panel, rotation-ring, session-limit-detection, takeover-command) describe behaviour that is preserved through the migration; their requirements don't change. -->

## Impact

- **Code removed**: `internal/repl/` (entire package, ~30 files including UI: shell, pane, status bar, commands, line-reader, `!cmd` shell escape; plus 8 runner files and their tests).
- **Code added/modified**: `internal/daemon/runners/{minimax,claude,codex,local,copilot}.go` updated; `internal/daemon/runners/{gemini,opsx,pool}.go` created; matching `_test.go` files.
- **CLI surface**: `--repl` flag and `MILLIWAYS_REPL=1` env var no longer recognised. Users on the legacy fallback must switch to `milliways` (no flags) which launches milliways-term/wezterm.
- **`cmd/milliways/main.go`**: imports of `internal/repl` removed (currently the only consumer); ~100 lines of REPL setup deleted (lines ~1557-1660).
- **`cmd/milliways/launcher.go`**: REPL-mode dispatch and fallback messaging removed.
- **Docs**: `README.md`, `CLAUDE.md` (project root), and `cmd/milliwaysctl/README.wezterm.md` references audited.
- **Out of scope**: `internal/tools/` itself; the wezterm/milliways-term integration internals; kitchen adapters other than the codex sandbox/approval defaults folded in here.
- **Migration risk**: dispatch behaviour for HTTP runners changes (tool loop becomes default). Token/cost accounting must continue to work via the existing daemon usage hooks. Each runner port is independently testable.
