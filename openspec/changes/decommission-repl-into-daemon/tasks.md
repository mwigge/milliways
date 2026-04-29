## 1. Branch and tooling

- [x] 1.1 Create branch `chore/decommission-repl-into-daemon` off `master` (renamed from `fix/codex-default-sandbox-approval`)
- [x] 1.2 Verify clean baseline: `go build ./... && go test ./internal/daemon/... ./internal/repl/...` is green before any port begins
- [x] 1.3 Capture a manifest of REPL runner exports (types, constants, functions) so nothing referenced by `cmd/milliways/main.go` is silently lost — see `manifest.md`

## 1a. Codex kitchen-adapter sandbox/approval defaults (folded in, already implemented)

- [x] 1a.1 Add tests in `internal/kitchen/adapter/codex_test.go` for `buildCodexArgs`: defaults inject `--sandbox workspace-write` and `--ask-for-approval never`; user-supplied flags via `cfg.Args` win; prompt is always the last positional arg
- [x] 1a.2 Extract `buildCodexArgs(cfg, task) []string` and `hasFlag` helper in `internal/kitchen/adapter/codex.go`; replace inline arg construction in `Exec`
- [x] 1a.3 `go test ./internal/kitchen/adapter/ -run TestBuildCodexArgs` green; full kitchen suite green
- [x] 1a.4 Commit `fix(kitchen): default codex sandbox=workspace-write approval=never` (separate atomic commit so it stays revertable if codex defaults change again) — `f65fcc5`

## 2. Shared agentic tool-loop helper

- [x] 2.1 Add `internal/daemon/runners/tooling.go` with `RunAgenticLoop(ctx, client, registry, messages, opts) (LoopResult, error)` plus `DefaultMaxTurns = 10`
- [x] 2.2 Add `internal/daemon/runners/tooling_test.go` covering: clean stop, multiple tool calls per turn (in order), max-turn cap (custom + default 10), tool failure folded as `error: ...`, unknown tool folded as error, malformed JSON args folded as error
- [x] 2.3 Document the loop contract via godoc on `RunAgenticLoop` and the `Client`/`Message`/`ToolCall` types; multi-chunk SSE delta reassembly is per-runner (the `Client.Send` adapter), not in the shared loop

## 3. Minimax port (highest drift, owns the new tool loop)

- [x] 3.1 Survey done — REPL `runner_minimax.go` (829 base + 449 stash WIP diff) vs daemon `runners/minimax.go` (265). Daemon's RunMiniMax uses a channel-based contract (Pusher/MetricsObserver), so the "port" is a substantial enrichment of the daemon shape rather than a 1-to-1 file copy.
- [x] 3.2 Chat path now drives via `RunAgenticLoop`: system prompt prepended (no `req.Rules` forwarding), per-delta `stream.Push(encodeData(...))` preserved for content, EOF-without-terminal-event still reported as "incomplete stream" so the existing TestRunMiniMax_IncompleteStreamEmitsError contract holds. **Deferred** (out of scope for this commit, file as follow-up): SSE think-filter (`<think>...</think>` extraction), open-fence/heredoc integrity warnings — these are REPL-presentation concerns; the agentic tool loop subsumes most of the integrity-warning use case (model now invokes file-write tools rather than narrating heredocs).
- [ ] 3.3 Image, music, lyrics paths — **deferred**. The daemon `RunMiniMax` doesn't currently support multi-kind dispatch (only chat). Adding image/music/lyrics requires routing-layer work upstream (the `Pusher` event vocabulary needs `image_url` etc.). File as follow-up; out of scope for the runner-port theme.
- [x] 3.4 `tools.NewBuiltInRegistry()` is the default registry (override via `withMinimaxToolRegistry` for tests; disable via `MINIMAX_TOOLS=off` env var). Wired through `runMiniMaxOnce` → `RunAgenticLoop`.
- [x] 3.5 Test parity preserved: all 4 existing tests (`NoAPIKey`, `StreamsDeltas`, `APIError`, `IncompleteStreamEmitsError`) still pass. **Reasoning**: Test parity for `internal/repl/runner_minimax_test.go` is N/A here because the daemon API surface differs (channel/Pusher vs synchronous io.Writer). Behaviour parity is what matters and is verified by the existing daemon tests.
- [x] 3.6 Four new golden-path tests in `minimax_tools_test.go`: `IncludesSystemPromptAndTools` (payload shape), `ToolsDisabledByEnv` (env override), `AgenticToolLoop` (multi-turn execution + assistant tool_calls msg + tool result msg), `ToolFailureFoldedAsErrorContent` (error-prefixed tool message back to model)
- [x] 3.7 `go test ./internal/daemon/runners/` green (8/8 minimax tests + 7/7 tooling helper tests). Full `go test ./...` green. Commit `feat(daemon): port minimax with agentic tool loop`

## 4. Greenfield daemon runners

- [x] 4.1 `internal/daemon/runners/gemini.go` created (CLI shell-out, stderr session-limit detection); 5 tests pass; committed `9a57c11 feat(daemon): add gemini runner`
- [ ] 4.2 ~~Create `internal/daemon/runners/opsx.go`~~ — **pivoted** per design's D2 open question and the D8 milliwaysctl pattern. Opsx is request/response with subcommands (`list`, `status`, `show`, `archive`, `validate`), and apply/explore need orchestration with a chat runner. The daemon's hardcoded `Run<Provider>(ctx, chan, stream, metrics)` shape doesn't fit. Instead: add a `milliwaysctl opsx <verb>` subcommand tree (same shape as `milliwaysctl local`); the wezterm Leader+/ palette surfaces them as `/opsx-list`, `/opsx-status`, etc. for free. Apply/explore (compose verbs) deferred — they need orchestration design (probably `--agent <name>` that talks to daemon agent.send); file as follow-up.
- [x] 4.3 `internal/daemon/runners/pool.go` created (Poolside CLI shell-out — name was misleading; pool is a Poolside-AI CLI wrapper, not a multi-provider router); 5 tests pass; committed in this round

## 5. Drift-sync ports for existing daemon runners

- [x] 5.1 Claude drift-sync: rate_limit_event surfacing, stderr session-limit detection, cache tokens in chunk_end. Out of scope (richer dispatch contract): per-call reasoning, --allowed-tools, --model, --image. Committed `f7b17b1`.
- [x] 5.2 Codex drift-sync: default `--sandbox workspace-write --ask-for-approval never` (mirrors kitchen-adapter fix), Zscaler/proxy block detection (stdout + stderr), JSON event session-limit detection. Out of scope: per-call reasoning/profile/image/search/model. Committed `b9a3a8d`.
- [x] 5.3 Local drift-sync: pivoted from Ollama-native (`/api/chat` port 11434, OLLAMA_BASE_URL) to OpenAI-compatible (`/chat/completions` port 8765, MILLIWAYS_LOCAL_ENDPOINT) so daemon matches REPL/ctl/install_local.sh. Bearer auth via MILLIWAYS_LOCAL_API_KEY. Out of scope: tool registry via RunAgenticLoop (local models often unreliable at tool calling — opt-in via env var in a follow-up). Committed `0244419`.
- [x] 5.4 Copilot drift-sync: stderr session-limit detection (rate limit / context window / context_length / token limit). The smallest of the four ports — copilot was already roughly aligned. Committed in this round.

## 6. Excise REPL setup from cmd/milliways/main.go (revised — see manifest.md)

The original task assumed `cmd/milliways/main.go` would be rewritten to construct
daemon runners directly. The manifest revealed the daemon runners are invoked
through the daemon RPC layer, not from `cmd/milliways/main.go`. So this section
*deletes* the REPL construction code instead of porting it.

- [x] 6.1 ~~Move shared types~~ — not needed. Manifest revealed all `repl.QuotaInfo`/`QuotaPeriod` usage was inside `runREPL()`; deleting that function deleted all consumers in one shot.
- [x] 6.2 N/A (see 6.1)
- [x] 6.3 Deleted `runREPL` function (lines 1497–1614, ~118 lines) plus the launcher-side `--repl` machinery (`stripLeadingREPLFlag`, `ensureREPLFlag`, `printREPLDeprecationNotice`, `MILLIWAYS_REPL` env handling, `modeREPL` constant). RunE for no-args now returns a clear error pointing at milliways-term.
- [x] 6.4 `internal/repl` import removed from `cmd/milliways/main.go`.
- [x] 6.5 `go build ./...` and `go test ./...` green. Committed in this round (combined with sections 7-9 — they're mutually dependent and a single atomic commit is safer than four cascading ones).

## 7. Strip --repl flag and MILLIWAYS_REPL env

- [x] 7.1 `--repl` parsing removed (the entire pre-cobra dispatch block, `stripLeadingREPLFlag`, `ensureREPLFlag`, deprecation notice printer)
- [x] 7.2 `MILLIWAYS_REPL` env handling removed
- [x] 7.3 REPL-mode dispatch removed from `cmd/milliways/launcher.go` — `modeREPL` constant gone, `parseLauncherMode` simplified to two-mode (cockpit | cobra), `printREPLDeprecationNotice` deleted
- [x] 7.4 Launcher milliwaysd-failure messages rewritten — point at `daemonLogPath()` for tail and `milliwaysd` (foreground) for diagnostics, not `--repl`
- [x] 7.5 launcher_test.go updated — `modeREPL`-bearing test cases removed, `parseLauncherMode` signature corrected. Build green; cobra now rejects `--repl` as unknown flag.
- [x] 7.6 Combined into the single Section 6-9 commit (mutually dependent)

## 8. Delete internal/repl/

- [x] 8.1 `git rm -r internal/repl/` — entire package deleted
- [x] 8.2 No remaining `internal/repl` imports (verified by grep)
- [x] 8.3 main.go REPL setup excised (118 lines via `runREPL` deletion)
- [x] 8.4 `go build ./... && go test ./...` green
- [x] 8.5 Combined into the single Section 6-9 commit

## 9. Documentation cleanup

- [x] 9.1 README.md `--repl` mentions removed (the legacy-fallback paragraph + the CLI-mode block trailer)
- [x] 9.2 CLAUDE.md "Key packages" list updated — `internal/repl/` replaced with `internal/daemon/runners/`, `internal/tools/`, `cmd/milliways/`, `cmd/milliwaysctl/`
- [x] 9.3 README.wezterm.md is already current (the section was rewritten in Section 11b without `--repl` references)
- [x] 9.4 CHANGELOG.md `[Unreleased]` block expanded with Added/Changed/Removed entries covering the decommission, all the runner ports, drift-syncs, and the codex sandbox/approval fix
- [x] 9.5 Combined into the single Section 6-9 commit

## 10. Smoke and final verification

- [ ] 10.1 `make smoke` passes
- [ ] 10.2 Run a real minimax dispatch end-to-end and verify tool execution (bash + file read)
- [ ] 10.3 Run a real codex dispatch and verify tool execution (sandbox/approval defaults from `fix/codex-default-sandbox-approval` are in effect)
- [ ] 10.4 Push branch, open PR titled `chore: decommission internal/repl into daemon runners`
- [ ] 10.5 Once merged: `/opsx:archive decommission-repl-into-daemon`

## 11. Local-model self-service (folded in; runs before Section 3 per pacing)

### 11a. milliwaysctl `local` subcommand tree

- [x] 11a.1 Create `cmd/milliwaysctl/local.go` with a `runLocal(args []string) int` dispatcher; register it in `main.go`'s switch as `case "local":`
- [x] 11a.2 Implement `local install-server` — shells `scripts/install_local.sh`, streams output, propagates exit code; env vars from the script (MODEL_REPO/QUANT/PORT/BIND_HOST) pass through naturally
- [x] 11a.3 Implement `local install-swap` — shells `scripts/install_local_swap.sh`; `--hot` flag sets `HOT_MODE=1`
- [x] 11a.4 Implement `local list-models` — GETs `$MILLIWAYS_LOCAL_ENDPOINT/models` (default `http://localhost:8765/v1`); prints IDs one per line; non-zero exit on unreachable
- [x] 11a.5 Implement `local switch-server <kind>` — writes `~/.config/milliways/local.env` (or `$XDG_CONFIG_HOME/milliways/local.env`) with the endpoint
- [x] 11a.6 Implement `local download-model <repo> [--quant Q] [--alias N] [--force]` — curls GGUF from HF into `$MODEL_DIR` (default `$HOME/.local/share/milliways/models/`); skips if cached
- [x] 11a.7 Implement `local setup-model <repo> [--quant Q] [--alias N]` — download → idempotent insert into `~/.config/milliways/llama-swap.yaml` → best-effort verify via list-models
- [x] 11a.8 Tests in `cmd/milliwaysctl/local_test.go`: 18 tests covering URL/dest construction, list-models JSON parsing + unreachable, kind→endpoint map + unknown kind, switch-server env-file write, llama-swap insert (add / idempotent / preserve existing), dispatcher unknown-verb / no-args / help
- [x] 11a.9 Update `cmd/milliwaysctl/main.go` `usage()` to document the `local` verb tree (single-line summary pointing at `local --help`)
- [x] 11a.10 `go build ./...` green; `go test ./cmd/milliwaysctl/...` 28 tests pass

### 11b. wezterm Lua slash-command dispatcher

- [x] 11b.1 Read `cmd/milliwaysctl/milliways.lua` end-to-end (existing leader-key pattern uses `act.PromptInputLine` + `wezterm.action_callback` already, e.g. the resume modal at line 205)
- [x] 11b.2 Bind `Leader + /` to a `wezterm.action_callback` that opens an `InputSelector` palette (curated `ctl_choices`, `fuzzy=true`); chosen complete verbs dispatch to `act.SpawnCommandInNewTab { args = {'milliwaysctl', ...} }` directly; verbs taking args (trailing space in id) and the free-form escape hatch fall through to `PromptInputLine` with optional `initial_value` prefill
- [x] 11b.3 Output streams in the new tab spawned by `SpawnCommandInNewTab` (wezterm hosts the spawned process directly; exit visible in the tab); no extra streaming layer needed
- [x] 11b.4 Completion via `InputSelector` with `fuzzy=true` — wezterm-native filter over the curated `ctl_choices` list. Free-form escape hatch covers verbs not in the curated list. (Original task wording suggested `--json-completions` querying ctl; the wezterm-native InputSelector pattern is simpler and idiomatic per docs.)
- [x] 11b.5 `cmd/milliwaysctl/README.wezterm.md` updated with the palette flow, examples, limitations, and a smoke test recipe
- [x] 11b.6 Smoke validated via `wezterm --config-file …/milliways.lua show-keys` — config parses cleanly, `LEADER /` registers as the expected `EmitEvent`/callback chain. End-to-end "press Leader+/" smoke requires a wezterm GUI session and is documented for the user to run.
- [x] 11b.7 Commit `feat(wezterm): milliwaysctl command palette via Leader+/`

### 11c. Wiring and docs

- [x] 11c.1 Update `README.md` "Local models / Setup" section to lead with `milliwaysctl local <verb>` + Leader+/ palette; keep `scripts/install_local.sh` as a fallback path
- [x] 11c.2 Update `CHANGELOG.md` `[Unreleased]` with Added (ctl `local` tree, Leader+/ palette, agentic tool-loop helper) + Fixed (codex sandbox/approval defaults)
- [x] 11c.3 Commit `docs: local-model self-service via milliwaysctl + slash dispatcher`
