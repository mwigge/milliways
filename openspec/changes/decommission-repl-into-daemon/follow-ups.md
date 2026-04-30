# Follow-ups Deferred from `decommission-repl-into-daemon`

This change shipped at v0.5.0 with the runner consolidation, agentic tool loop on HTTP runners, and review-driven security guardrails. The items below were intentionally scoped out — each warrants its own change proposal once the precursor work is in production.

## Architecture

### A1. Re-home `provider.ToolDef` into `internal/tools/`
Reviewer (architect) F2. Today `tooling.go` and `tools.Registry.List()` reach into `internal/provider` for `ToolDef`. That's a layer inversion — the agentic loop has no business knowing about the higher-level `provider.Provider` abstraction. Define `tools.ToolSchema` (or `tools.Definition`) in `internal/tools/` as the canonical shape; either remove `provider.ToolDef` or make it a thin re-export.

### A2. Extract a `Runner` interface
Reviewer (architect) F6. Every daemon runner has the same `Run<Provider>(ctx, input <-chan []byte, stream Pusher, metrics MetricsObserver)` signature. The `daemon-runners` spec already implies registry membership and a queryable `AuthStatus()` surface. Define one interface; have the runners implement it; collapse the agents.go switch into a `map[string]Runner` lookup; add a conformance test suite.

### A3. Reclassify `pool` and `opsx` between runners and ops verbs (define D9)
Reviewer (architect) F5 + F7. `pool` is a Poolside-AI CLI wrapper; semantically it's a chat runner so it stays. `opsx` was reclassified to `milliwaysctl opsx` in v0.5.0 but the apply / explore compose verbs (which need orchestration with daemon `agent.send` + a chat runner) still need a home. Proposal: `internal/orchestrator/opsx.go` for the compose verbs, exposed only via daemon RPC; bare `milliwaysctl opsx <verb>` stays for the shell-out verbs.

### A4. Refactor the streaming boundary so the loop owns SSE
Reviewer (architect) F4 + F11. Today `minimaxClient` and `localClient` smuggle a `Pusher` into the chat client and push deltas as a side-effect of `Send`. With two HTTP runners now sharing `streamOpenAITurn`, this is OK in practice — but the contract is implicit. Either have `Client.Send` return a delta channel and let `RunAgenticLoop` fan it out, or formalise the side-effect in the `Client` interface godoc.

### A5. Slash dispatcher routes through daemon RPC for in-session verbs
Reviewer (architect) F3. The wezterm `Leader+/` palette currently fork-execs `milliwaysctl <verb>` per dispatch. For in-session ops verbs (anything that should participate in session quotas, ledger, OTel parent span, cancellation) this should dispatch via the daemon's RPC channel instead. Bootstrap verbs (`local install-server`) stay fork-exec since they must work without a running daemon.

## Observability

### O1. Wire `TraceEmitter` into production tool registry
SRE S2.4. `tools.NewBuiltInRegistry()` returns a `Registry` with `emitter == nil`; the `r.emitter.Emit(...)` call inside `ExecTool` is therefore a no-op in production. Thread the daemon's existing `TraceEmitter` through `runners.RunMiniMax → minimaxRegistry()` (and equivalently for local) so tool execution emits the agent.tool trace event in production, not just in tests.

### O2. Wrap each prompt in an `agent.dispatch` parent span
SRE S2.4. `agents.go:runMiniMax` (and friends) call `runners.RunMiniMax(context.Background(), …)` — the resulting `agent.tool` spans inside `RunAgenticLoop` are root spans with no causal link back to the originating prompt. Wrap each prompt in `StartSpan(ctx, "agent.dispatch", attribute.String(AttrAgentID, sess.AgentID))` and pass the span context down so child tool spans inherit the parent.

### O3. Loop-level metrics: `tool_loop_turns`, `tool_loop_max_turns_hit`
SRE S2.4. There's no counter today for how many turns the agentic loop took or how often it hit the 10-turn cap. SLO impact: cannot alert on "X% of dispatches hit max-turns". Add the counters to `runners/metrics.go` and observe them at the end of `runMiniMaxOnce` (and `runLocalOnce`).

### O4. Smoke scenario for the agentic tool loop
SRE S2.7. `make smoke` does not exercise `RunAgenticLoop` end-to-end. Add `testdata/smoke/bin/fake-minimax-tool-loop` that emits SSE chunks containing `tool_calls` then a `stop`, plus a smoke scenario asserting the daemon executes the tool, `chunk_end` carries non-zero token counts, and the loop terminates within 10 turns.

### O5. Stderr session-limit detection at `slog.Warn`, not just stream
SRE S3.9. When `<provider>StderrSignalsLimit(lines)` returns true, the runner pushes a structured `err` event to the stream but does not log a warn-level event. SREs looking at `milliwaysd.log` see only `slog.Debug` raw stderr noise (filtered at default level). Promote to `slog.Warn` with `agent` + tail of stderr lines so default-level logs flag session-limit transitions.

## Configurability

### C1. Per-runner `MAX_TURNS` override env var
SRE S2.6 + Reviewer F10. The 10-turn cap is hardcoded. Expose `MILLIWAYS_<PROVIDER>_MAX_TURNS` (parse with sane bounds 1..50) so users with long planning workloads can raise the cap and constrained / cheap models can lower it. Test injection via `LoopOptions.MaxTurns` is already supported.

### C2. Per-runner CWD threading via `agent.send`
SRE S4.11. Subprocess CLI runners (claude/codex/copilot/gemini/pool) inherit the daemon's CWD via `os.Getwd()`. Two concurrent sessions in different project dirs both see the daemon's CWD, not the user's. Thread `project_root` through the `agent.send` RPC and use it as `cmd.Dir`.

### C3. Per-call reasoning / attachments / model override / `--allowed-tools`
Spec amendment in `daemon-runners`. The REPL runners exposed `Settings()` for per-call config. The daemon's channel-of-bytes `agent.send` does not currently carry any of this. Extend the RPC surface (likely via a control-channel JSON header) so the per-call config survives.

## Code quality

### Q1. Extract `runCLIOnce` helper
Reviewer MEDIUM 16 + Code-quality B9. claude/codex/copilot/gemini/pool repeat 25-line stderr-capture goroutine bodies and 14-line stdout-streamer functions verbatim. The four `<runner>StderrSignalsLimit` functions share the same lowercase-substring-match pattern. Extract `runCLIOnce(ctx, agentID, cmd, stream, metrics, opts)` and `stderrSignalsLimit(lines, keywords []string)`. A CVE-style fix today would need to be applied 3+ times.

### Q2. Lift `safeRunnerEnv` into a shared `internal/sandbox` package
Today the same allowlist exists in `internal/kitchen/adapter/adapter.go` (`safeEnv` + `safeEnvKeys`) and `internal/daemon/runners/subprocess_env.go` (`safeRunnerEnv` + `safeRunnerEnvKeys`). Consolidate.

### Q3. Audit `args` builder pattern across CLI runners
Code-quality B7 + B22. `geminiArgsBuilder` / `poolArgsBuilder` are package-level mutable function vars used as test seams; `claudeBinary` / `codexBinary` / `copilotBinary` are the same pattern for the binary path. claude/codex/copilot/gemini/pool are inconsistent about which seams exist. Pick one pattern (struct-with-fields preferred over package-level mutable vars), apply uniformly.

## Behaviours

### B1. Lexical stream-integrity checks for minimax
Spec `runner-tool-execution`. The REPL runner detected unclosed code fences and unclosed shell heredocs in assistant content, warning when a model described a file-write but never invoked the tool. With the agentic tool loop now wiring file/bash tools, this is less load-bearing — the model is expected to invoke tools rather than narrate. But the safety net is gone. Decide whether to port the integrity checks (in `streamOpenAITurn`?) or formally drop the requirement.

### B2. Image / music / lyrics dispatch paths for minimax
Tasks 3.3. The daemon's `Pusher` event vocabulary doesn't carry `image_url` / `audio_url` today; routing-layer work upstream is required before the multi-kind paths can return. Filed as separate change `minimax-multimodal-dispatch`.

### B3. opsx apply / explore compose verbs
Tasks 4.2. Need orchestration with daemon `agent.send` (forward openspec output to a chat runner). Probably belongs in `internal/orchestrator/opsx.go` per A3.

### B4. Real interactive approval gate for `Bash`
Security review #1. Today the workspace-root containment + cwd pin + log-redaction are the safety net, but there's no per-invocation user approval. A real approval gate needs a UX surface (the wezterm overlay or a dedicated milliways-term modal) that does not exist yet. When the UX lands, wire it in.

### B5. Real diff parser for `handleEdit`
Security review #9. `applyUnifiedDiff` collapses all `-`/`+` lines across all hunks ignoring `@@` boundaries, then does a single `strings.Replace`. Replace with a strict hunk-by-hunk parser; verify file's pre-image SHA256 matches a model-supplied hash; timestamp `.bak` files so they don't clobber across edits in the same session.

### B6. WebFetch content-type filter / HTML stripping
Security review #8. The fetched body is returned as a raw Go string. Binary blobs become part of the conversation; HTML scripts and data: URIs become high-bandwidth prompt-injection vectors. Reject or strip non-text content-types; goquery-style strip for `text/html`.

## Documentation

### D1. SECURITY.md rewrite
Security review #6. Document the codex sandbox default change, the workspace-containment / SSRF guardrails, the `MINIMAX_TOOLS=off` / `MILLIWAYS_LOCAL_TOOLS=off` opt-outs, and the threat model the agentic loop assumes. The README has a `Tool security` table; SECURITY.md should formalise it.

### D2. ADR for the runner-vs-ops-verb split
Companion to A3. Once D9 is decided, write an ADR explaining "runner = streaming chat dispatch loop, ops verb = one-shot operational verb" so the next contributor can answer "should this go in `runners/` or `ctl`?" without searching for precedent.
