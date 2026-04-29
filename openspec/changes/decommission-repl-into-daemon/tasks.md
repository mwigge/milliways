## 1. Branch and tooling

- [x] 1.1 Create branch `chore/decommission-repl-into-daemon` off `master` (renamed from `fix/codex-default-sandbox-approval`)
- [ ] 1.2 Verify clean baseline: `go build ./... && go test ./internal/daemon/... ./internal/repl/...` is green before any port begins
- [ ] 1.3 Capture a manifest of REPL runner exports (types, constants, functions) so nothing referenced by `cmd/milliways/main.go` is silently lost

## 1a. Codex kitchen-adapter sandbox/approval defaults (folded in, already implemented)

- [x] 1a.1 Add tests in `internal/kitchen/adapter/codex_test.go` for `buildCodexArgs`: defaults inject `--sandbox workspace-write` and `--ask-for-approval never`; user-supplied flags via `cfg.Args` win; prompt is always the last positional arg
- [x] 1a.2 Extract `buildCodexArgs(cfg, task) []string` and `hasFlag` helper in `internal/kitchen/adapter/codex.go`; replace inline arg construction in `Exec`
- [x] 1a.3 `go test ./internal/kitchen/adapter/ -run TestBuildCodexArgs` green; full kitchen suite green
- [ ] 1a.4 Commit `fix(kitchen): default codex sandbox=workspace-write approval=never` (separate atomic commit so it stays revertable if codex defaults change again)

## 2. Shared agentic tool-loop helper

- [ ] 2.1 Add `internal/daemon/runners/tooling.go` with `RunAgenticLoop(ctx, client, registry, opts) (Result, error)` plus `LoopOptions{MaxTurns: 10}` default
- [ ] 2.2 Add `internal/daemon/runners/tooling_test.go` covering: multi-chunk argument reassembly, multiple tool calls per turn, max-turn cap, clean stop, tool failure folded as `error: ...`
- [ ] 2.3 Document the loop contract referenced by `runner-tool-execution` spec

## 3. Minimax port (highest drift, owns the new tool loop)

- [ ] 3.1 Read `internal/repl/runner_minimax.go` end-to-end; list every exported symbol and behaviour to preserve
- [ ] 3.2 Port chat path: messages, system prompt, `req.Rules` exclusion, SSE think filter, integrity checks (open fence + heredoc), session usage accounting
- [ ] 3.3 Port image, music, lyrics paths (preserve current behaviour; no tool loop on these)
- [ ] 3.4 Wire `tools.NewBuiltInRegistry()` into the chat path via `RunAgenticLoop`
- [ ] 3.5 Port `internal/repl/runner_minimax_test.go` (rename references; keep coverage)
- [ ] 3.6 Add a golden-path test: mock chat endpoint emits a tool-call → runner invokes registry tool → assistant→tool→assistant turn sequence is correct
- [ ] 3.7 `go test ./internal/daemon/runners/ -run TestMinimax` green; commit `feat(daemon): port minimax runner with agentic tool loop`

## 4. Greenfield daemon runners

- [ ] 4.1 Create `internal/daemon/runners/gemini.go` mirroring REPL's `runner_gemini.go` structure; add tests; commit `feat(daemon): add gemini runner`
- [ ] 4.2 Create `internal/daemon/runners/opsx.go` (shells out to `openspec` CLI); add tests; commit `feat(daemon): add opsx runner`
- [ ] 4.3 Create `internal/daemon/runners/pool.go` (multi-provider routing); add tests; commit `feat(daemon): add pool runner`

## 5. Drift-sync ports for existing daemon runners

- [ ] 5.1 Diff `internal/repl/runner_claude.go` vs `internal/daemon/runners/claude.go`; port retry, rate-limit detection, image attachments, reasoning modes; sync tests; commit `feat(daemon): sync claude runner with REPL feature parity`
- [ ] 5.2 Diff `internal/repl/runner_codex.go` vs `internal/daemon/runners/codex.go`; port reasoning modes, sandbox/approval, proxy detection, JSON event parsing; sync tests; commit `feat(daemon): sync codex runner with REPL feature parity`
- [ ] 5.3 Diff `internal/repl/runner_local.go` vs `internal/daemon/runners/local.go`; identify backend (ollama/llama.cpp); wire `RunAgenticLoop` if HTTP-based; sync tests; commit `feat(daemon): sync local runner with REPL feature parity`
- [ ] 5.4 Diff `internal/repl/runner_copilot.go` vs `internal/daemon/runners/copilot.go`; minor sync; wire `RunAgenticLoop`; sync tests; commit `feat(daemon): sync copilot runner with REPL feature parity`

## 6. Wire daemon runners into cmd/milliways

- [ ] 6.1 Update `cmd/milliways/main.go` to construct daemon runners (not REPL runners) for the routes that currently use REPL
- [ ] 6.2 Verify `cmd/milliways/main.go` compiles with all eight daemon runners registered and no `internal/repl` import in the runner-construction path
- [ ] 6.3 Run `go build ./...` and `go test ./...` (all passing); commit `refactor(cmd): construct runners from internal/daemon/runners`

## 7. Strip --repl flag and MILLIWAYS_REPL env

- [ ] 7.1 Remove `--repl` parsing from `cmd/milliways/main.go` (lines ~80–129) — `stripLeadingREPLFlag`, `ensureREPLFlag`, deprecation print path
- [ ] 7.2 Remove `MILLIWAYS_REPL` env handling from `cmd/milliways/main.go`
- [ ] 7.3 Remove REPL-mode dispatch from `cmd/milliways/launcher.go` (lines 18, 53–58, 103, 108, 228, 234, 243)
- [ ] 7.4 Rewrite the launcher's milliwaysd-failure messages (lines 120, 128, 136) to point at troubleshooting `milliwaysd` (logs/lock files), not `--repl`
- [ ] 7.5 Verify cobra rejects `milliways --repl` with an unknown-flag error (manual run) and `MILLIWAYS_REPL=1 milliways` ignores the env var
- [ ] 7.6 Commit `feat(cli)!: remove --repl flag and MILLIWAYS_REPL env`

## 8. Delete internal/repl/

- [ ] 8.1 `git rm -r internal/repl/`
- [ ] 8.2 Verify no remaining references: `grep -r "internal/repl" --include="*.go" .` returns nothing outside `.claude/worktrees/`
- [ ] 8.3 Verify no remaining references in main.go: ~100 lines of REPL setup (`main.go:1557–1660`) are gone
- [ ] 8.4 `go build ./... && go test ./...` green
- [ ] 8.5 Commit `chore(repl)!: remove internal/repl package`

## 9. Documentation cleanup

- [ ] 9.1 Update `README.md` (lines 43, 955) to drop `--repl` mentions
- [ ] 9.2 Update project root `CLAUDE.md` "Key packages" list to remove `internal/repl/` entry
- [ ] 9.3 Update `cmd/milliwaysctl/README.wezterm.md` if it references `--repl`
- [ ] 9.4 Update `CHANGELOG.md` with a `BREAKING` entry under the next version
- [ ] 9.5 Commit `docs: drop --repl and internal/repl references`

## 10. Smoke and final verification

- [ ] 10.1 `make smoke` passes
- [ ] 10.2 Run a real minimax dispatch end-to-end and verify tool execution (bash + file read)
- [ ] 10.3 Run a real codex dispatch and verify tool execution (sandbox/approval defaults from `fix/codex-default-sandbox-approval` are in effect)
- [ ] 10.4 Push branch, open PR titled `chore: decommission internal/repl into daemon runners`
- [ ] 10.5 Once merged: `/opsx:archive decommission-repl-into-daemon`
