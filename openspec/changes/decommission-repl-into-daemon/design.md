## Context

`internal/repl/` and `internal/daemon/runners/` host two parallel runner implementations that have drifted significantly:

| Runner | REPL (lines) | Daemon (lines) | Notes |
|---|---:|---:|---|
| minimax | 1045 | 265 | Daemon lacks the agentic tool loop wired in REPL last session |
| claude | 661 | 239 | Drift in retry, rate-limit detection, image attachments |
| codex | 624 | 188 | Drift in reasoning modes, sandbox/approval, proxy detection |
| local | 482 | 147 | Drift in backend dispatch and quota handling |
| copilot | 155 | 135 | Roughly aligned |
| gemini | 272 | — | Missing entirely from daemon |
| opsx | 148 | — | Missing entirely from daemon |
| pool | 273 | — | Missing entirely from daemon |

Only `cmd/milliways/main.go` imports `internal/repl/`. The default user surface is milliways-term (wezterm subtree under `crates/milliways-term/`) launched via `cmd/milliways/launcher.go`. The `--repl` flag is a labelled-deprecated fallback (line 243: "Removal scheduled in v0.6.0").

The freshly landed minimax tool loop (in `internal/repl/runner_minimax.go`) uses `internal/tools/` Registry with the built-in tool set (bash, file read/write/edit, grep, glob, web fetch). HTTP-based runners (minimax, copilot, local) need the same loop in the daemon path; CLI-based runners (claude, codex, gemini) inherit tool execution from their underlying CLI.

## Goals / Non-Goals

**Goals:**

- Single canonical runner implementation per provider, living under `internal/daemon/runners/`.
- Every HTTP-based runner exposes the same agentic tool loop with `internal/tools/` Registry.
- `internal/repl/` package, `--repl` flag, and `MILLIWAYS_REPL` env var deleted in the same change.
- Tests preserved or improved (no coverage regression on tool-loop or stream-integrity logic).
- Build green at every commit; no silent loss of routes.

**Non-Goals:**

- Refactoring `internal/tools/` itself.
- Changes to kitchen adapters in `internal/kitchen/adapter/` beyond the codex sandbox/approval defaults folded in here (see D7).
- Rewriting milliways-term/wezterm integration internals.
- Renaming `internal/daemon/runners/` to a top-level package — kept inside `daemon` because daemon is the only consumer.
- Generalising the tool surface per-runner — every HTTP runner gets `tools.NewBuiltInRegistry()` uniformly; per-runner curation is a follow-up if needed.

## Decisions

### D1. Destination package: `internal/daemon/runners/` (not a new top-level package)

The daemon is currently the only consumer; a top-level `internal/runners/` would create an empty abstraction. If a second consumer appears later (CLI subcommands, other entry points), lift then. Keeps blast radius minimal.

**Alternatives considered:** Lift to `internal/runners/` for "neutrality" — rejected because it's speculation. Keep both packages and deprecate REPL slowly — rejected because it leaves dead code and split-brain bugs.

### D2. Port-then-delete sequencing, runner-by-runner

Each runner gets its own commit: port code + tests, run `go test ./internal/daemon/runners/`, commit. After all eight pass, delete `internal/repl/` package + flag + launcher messages in a final commit.

Order: minimax (highest drift, has the new tool loop) → gemini/opsx/pool (greenfield) → claude/codex/local (drift sync) → copilot (smallest delta) → REPL deletion.

**Alternatives considered:** Big-bang single commit — rejected because review would be impossible and rollback granularity is zero. Lift runners verbatim then refactor — rejected because it duplicates code in a third location and the daemon already has stub implementations to merge into.

### D3. Tool-loop contract is uniform across HTTP runners

All HTTP runners (`minimax`, `copilot`, `local`) implement the same loop:

1. Send messages with `tools` array from `tools.Registry.Schemas()`.
2. Stream response, accumulating tool-call deltas.
3. On `finish_reason: "tool_calls"`, execute each tool via `Registry.Invoke(name, args)`, append assistant + tool messages to history, loop.
4. On `finish_reason: "stop"`, exit loop and return.
5. Hard cap at **10 turns** to prevent runaway loops; emit a structured warning if hit.

Implemented as a shared helper `internal/daemon/runners/tooling.go::RunAgenticLoop(ctx, client, registry, opts) (Result, error)`. Each runner provides a thin client adapter.

**Alternatives considered:** Per-runner copy of the loop — rejected because it guarantees drift returns. Putting the loop in `internal/tools/` — rejected because tools shouldn't know about chat completions.

### D4. CLI runners (claude, codex, gemini) skip the internal tool loop

The CLIs (`claude`, `codex --json`, `gemini`) execute tools themselves and stream tool-use events back. The daemon runner's job is to parse those events and surface them to the client. No internal tool loop needed.

**Alternatives considered:** Pass `internal/tools/` to CLI runners as a fallback — rejected because it conflicts with the CLI's own tool execution and would double-run actions.

### D5. Drop `--repl` flag and `MILLIWAYS_REPL` env in the same change

The decommission isn't done until users can't trip back to the dying path. Keeping the flag alongside the deletion creates a "feature exists but does nothing" footgun. Removal in this change; the launcher's "Fallback: run `milliways --repl`" messages get rewritten to point at troubleshooting `milliwaysd` startup.

**Alternatives considered:** Keep flag as a no-op printing a "removed" message — rejected because it's just a slower deprecation; the flag was already labelled deprecated. Soft-delete (gut UI, keep flag) — rejected because the flag's only function was launching the UI we're deleting.

### D6. Test parity: port REPL runner tests, not rewrite

The REPL runners have substantial test coverage (esp. minimax stream-integrity, codex JSON parsing, claude rate-limit detection). Each port lifts the tests alongside the code. Where tests reference REPL-only types (`teeWriter`, scheme), substitute the daemon equivalent or stub.

**Alternatives considered:** Rewrite tests cleanly — rejected because it loses captured edge-case bug history.

### D7. Codex kitchen-adapter sandbox/approval defaults folded in

`internal/kitchen/adapter/codex.go` invokes `codex exec --json <prompt>` without `--sandbox` or `--ask-for-approval`. Recent codex defaults to `read-only`/`on-request` in non-interactive mode and silently refuses tool execution — exactly the "codex can't run tools" symptom that motivated this whole thread. The fix extracts arg-building into `buildCodexArgs(cfg, task)` which injects `--sandbox workspace-write --ask-for-approval never` when the user hasn't supplied them via `cfg.Args`. Already implemented and tested on this branch; folded in because it's part of the same "every runner can actually run tools" theme.

**Alternatives considered:** Ship as a separate PR — rejected per direction; the codex kitchen path and the daemon-runner-port path are conceptually one body of work ("tools work everywhere").

## Risks / Trade-offs

- **[Risk]** Minimax tool-loop port introduces a regression on the agentic execution that just landed → **Mitigation**: golden-path test that mocks the chat endpoint, asserts tool calls are invoked, and the assistant→tool→assistant turn sequence is correct. Keep the existing REPL implementation in `git log` for diffing if needed.
- **[Risk]** Drift sync misses an edge case (rate-limit shape, proxy detection, exhaustion text) that production traffic hits → **Mitigation**: each runner port is its own commit; `git diff REPL_RUNNER DAEMON_RUNNER` is mandatory pre-commit. Tests for known edge cases (Zscaler block, rate-limit JSON shapes) ported alongside.
- **[Risk]** `pool` and `opsx` are routing/orchestration constructs that may not belong as "runners" — **Mitigation**: port them as runners for now to preserve behaviour; flag for follow-up reclassification (out of scope for this change).
- **[Risk]** Users on `--repl` lose access immediately on this release → **Mitigation**: this is the intended outcome; the flag was already deprecated. Launcher startup error messages get refreshed to point at `milliwaysd` troubleshooting (logs, lock files) instead of the removed fallback.
- **[Trade-off]** ~30 files of REPL UI code (shell, pane, status bar, commands, line-reader) deleted in one shot. Loses any recent unmerged WIP that touched them; verify `git stash list` and feature branches before final delete commit.

## Migration Plan

1. **Branch**: `chore/decommission-repl-into-daemon` (off `master`).
2. **Per runner**, in this order: minimax → gemini → opsx → pool → claude → codex → local → copilot. Each runner = one commit `feat(daemon): port <name> runner from REPL` (or `feat(daemon): create <name> runner` for greenfield). Tests included.
3. **Tool-loop helper** added before minimax port: `feat(daemon): add agentic tool-loop helper for HTTP runners`.
4. **Verify** after every commit: `go build ./... && go test ./internal/daemon/...`.
5. **Final commit** `chore: remove internal/repl package and --repl flag`:
   - Delete `internal/repl/` directory.
   - Strip `--repl` parsing from `cmd/milliways/main.go` (lines ~80–129) and `cmd/milliways/launcher.go` (lines 18, 53–58, 103, 108, 120, 128, 136, 228, 234, 243).
   - Strip `MILLIWAYS_REPL` env handling.
   - Strip `import "github.com/mwigge/milliways/internal/repl"` and the ~100 lines of REPL setup in `main.go` (~1557–1660).
   - Update `README.md` (line 43, 955) and `cmd/milliwaysctl/README.wezterm.md` references.
   - Update project root `CLAUDE.md` mention of `internal/repl/`.
6. **Smoke**: `make smoke` (uses the smoke harness from PC-21 closeout) plus a minimax-tool-call golden test.
7. **Rollback**: revert the final commit alone if REPL is needed back; the runner ports are independently usable.

## Open Questions

- Should `pool` runner remain as a "runner" or be reclassified into `internal/orchestrator/`? (Defaulting to: keep as runner for this change, file follow-up.)
- Should `opsx` "runner" stay at all, or be invoked directly via the openspec CLI from somewhere else? (Defaulting to: keep as runner for this change.)
- `local` runner backend: ollama? llama.cpp? Spec needs to identify the API shape so the tool loop is wired correctly. (Resolve during the local port commit by reading current implementation.)
- Tool surface curation per runner: uniform `NewBuiltInRegistry()` for all HTTP runners in this change; revisit per-runner curation if a model misuses a tool category.
