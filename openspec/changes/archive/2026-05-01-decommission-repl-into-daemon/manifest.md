# REPL Export Manifest

Captured for task 1.3. This is the surface area the migration must preserve (or replace) so `cmd/milliways/main.go` continues to compile and behave correctly.

## Symbols referenced from `cmd/milliways/main.go`

```
repl.NewClaudeRunner    (1×)
repl.NewCodexRunner     (1×)
repl.NewCopilotRunner   (1×)
repl.NewGeminiRunner    (1×)
repl.NewLocalRunner     (1×)
repl.NewMinimaxRunner   (1×)
repl.NewPoolRunner      (1×)
repl.NewREPL            (1×)
repl.NewREPLPane        (1×)
repl.NewREPLWithSubstrate (1×)
repl.NewReplLogHandler  (1×)
repl.NewShell           (1×)
repl.QuotaInfo          (2×)
repl.QuotaPeriod        (4×)
repl.REPL               (1×)
```

## REPL exported types (per file)

| File | Type | Notes |
|---|---|---|
| `runner.go` | `RunResult`, `Runner` (interface), `SessionUsage`, `QuotaInfo`, `QuotaPeriod`, `NullRunner` | Shared runner contract — needs to land somewhere outside `repl/` |
| `repl.go` | `InputKind`, `Input`, `RunnerState`, `REPL` + `NewREPL`, `NewREPLWithSubstrate`, `NewREPLWithQuotaFunc` | Pure UI layer — all goes away |
| `shell.go` | `Shell` + `NewShell` | UI layer — all goes away |
| `runner_claude.go` | `ClaudeReasoningMode`, `ClaudeSettings`, `ClaudeRunner` + `NewClaudeRunner` | Port to daemon |
| `runner_codex.go` | `CodexRunner`, `CodexReasoningMode`, `CodexSettings` + `NewCodexRunner` | Port to daemon |
| `runner_gemini.go` | `GeminiRunner`, `GeminiSettings` + `NewGeminiRunner` | New in daemon (greenfield) |
| `runner_minimax.go` | `MinimaxReasoningMode`, `MinimaxModelKind`, `MinimaxSettings`, `MinimaxRunner` + `NewMinimaxRunner` | Port to daemon (incl. tool loop) |
| `runner_copilot.go` | `CopilotRunner` + `NewCopilotRunner` | Port to daemon (drift-sync) |
| `runner_local.go` | `LocalRunner`, `LocalSettings` + `NewLocalRunner` | Port to daemon |
| `runner_pool.go` | `PoolRunner` + `NewPoolRunner`, `PoolSettings` | New in daemon (greenfield) |
| `runner_opsx.go` | (no `New*Runner` constructor exposed at this scan; verify in port) | New in daemon (greenfield) |

## Daemon runners' current exports

```
internal/daemon/runners/claude.go      → only `Pusher` interface
internal/daemon/runners/metrics.go     → `MetricsObserver` interface
internal/daemon/runners/probe.go       → `AgentInfo` struct
```

**Conclusion**: daemon runners have no `New<X>Runner` constructors and no `<X>Runner` types. They are not API-compatible drop-in replacements for the REPL runners. The wiring in `cmd/milliways/main.go` (lines ~1557–1660) constructs REPL-only `Runner` instances and registers them on the REPL `r.Register(...)`. The replacement path runs through `milliwaysd` (the daemon binary) — which is the wezterm/default flow already.

## Implication for the migration

`cmd/milliways/main.go` will lose ~100 lines of REPL setup entirely (the `runREPL` function and its callers), not be rewritten to construct daemon runners directly. The eight providers stay reachable via the daemon path that already exists. Section 6 of the task list ("Wire daemon runners into cmd/milliways") may need re-scoping: the daemon already has its own wiring; the cmd/milliways change is mostly *removing* REPL setup, not adding daemon wiring.

## Shared types that need a new home

These types live in `internal/repl/runner.go` but are general-purpose and need to survive:

- `Runner` (interface) — currently the contract every REPL runner implements
- `RunResult` — return type
- `SessionUsage`, `QuotaInfo`, `QuotaPeriod` — quota accounting; `repl.QuotaInfo`/`QuotaPeriod` are referenced from `main.go`
- `NullRunner` — test stub

**Decision needed during port**: do these types move into `internal/daemon/runners/` (canonical location), into a neutral `internal/runners/` package (as design D1 explicitly rejected), or into a small `internal/runtypes/` shim? Given D1 keeps everything in `internal/daemon/runners/`, these types move there.

Updates to `cmd/milliways/main.go`:
- `repl.QuotaInfo` → `runners.QuotaInfo` (where `runners "github.com/mwigge/milliways/internal/daemon/runners"`)
- `repl.QuotaPeriod` → `runners.QuotaPeriod`
- All `repl.New<X>Runner()` calls → removed (the daemon path constructs them itself)
- `repl.NewREPL`, `repl.NewShell`, `repl.NewREPLPane`, `repl.NewReplLogHandler`, `repl.REPL` → all deleted along with the REPL package.
