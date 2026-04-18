# Milliways Go quality review

## Findings
- **critical** — `internal/tui/dispatch.go:183-195`: `if summarizeStep != nil && stepID != "summarize" && status == pipeline.StatusDone || status == pipeline.StatusFailed` relies on `&&`/`||` precedence. Any failed step enters the block even when `summarizeStep == nil`, and line 195 then dereferences `summarizeStep.Prompt`, which can panic.
- **major** — `internal/kitchen/adapter/claude.go:408-415`, `internal/kitchen/adapter/codex.go:170-177`, `internal/kitchen/adapter/opencode.go:187-194`: `Send` starts a goroutine for the pipe write and returns on `ctx.Done()`. If the write blocks, that goroutine is left behind with no cancellation path, so interactive sends can leak goroutines under backpressure.
- **major** — `internal/orchestrator/orchestrator.go:77-78,91,115,179`: `reposAccessed` is mutable receiver state, reset and populated inside `Run`. Reusing one `Orchestrator` across concurrent runs would race and let repository tracking bleed between conversations.
- **major** — `internal/tui/dispatch.go:26,233-244` and `internal/tui/app.go:395,443-452`: block dispatch and follow-up `Send` calls are rooted in `context.Background()` instead of the caller/block context. Parent shutdown and higher-level cancellation will not propagate cleanly once those paths are taken.
- **minor** — `internal/kitchen/generic.go:97-99` vs `internal/kitchen/generic.go:15-20`: `GenericKitchen.Exec` checks `allowedCmds[k.cfg.Cmd]` directly, while the adapter layer correctly uses `IsCmdAllowed` with `filepath.Base`. Absolute-path binaries accepted elsewhere will be rejected here.

## What's good
- Error wrapping is mostly disciplined (`%w`) in the reviewed paths, especially in `internal/bridge/bridge.go` and `internal/orchestrator/orchestrator.go`.
- Nil-guarding and defensive copying are solid in several places, e.g. `internal/bridge/bridge.go:103-130`, `internal/bridge/cross_palace.go:37-44`, and `internal/conversation/model.go:187-193`.
- Test coverage is pointed at real behavior: failover and hydration in `internal/orchestrator/orchestrator_test.go`, access control in `internal/bridge/bridge_test.go`, and goroutine-leak checks in `internal/kitchen/adapter/leak_test.go`.

## Recommendations
1. Fix the summarize guard with explicit parentheses and a nil-safe path, then add a regression test for failed pipelines without a summarize step.
2. Move adapter stdin writes behind a single writer path or another ctx-aware mechanism, and add goleak coverage for cancelled `Send` calls.
3. Keep `Orchestrator` run state local to `Run`, and thread block-scoped contexts through TUI dispatch/send flows instead of creating fresh background contexts.
4. Reuse `IsCmdAllowed` everywhere command allowlisting is enforced so path-based configs behave consistently.
