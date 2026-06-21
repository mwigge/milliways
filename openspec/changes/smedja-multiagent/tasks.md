## 1. Role-Based Sommelier Dispatch

- [x] Add `role` dimension to `SommelierRouter`: `role × complexity → (runner, tier, model)` mapping
- [x] Load `.smedja/agents.toml` at session start; parse `[roles.*]` sections; fall back to built-in defaults if absent
- [x] `smj workspace agents` subcommand: print resolved role→runner→tier→model table for current workspace
- [x] TOML schema: `[roles.<name>]` with `runner`, `tier`, `model`, `tools` fields; `[parallel.dependencies]` for role ordering
- [x] Write unit tests: role=review → deep tier; role=impl → local tier; missing agents.toml → defaults applied; tool whitelist enforced

## 2. Worktree Pool and Parallel Tasks

- [x] Create `crates/smedja-assayer/orchestrator/parallel.rs`: `WorktreePool`, `Task`, `TaskStatus` structs
- [x] `WorktreePool.Start(goal, roles)`: create git worktrees via `git worktree add`; start smdjad sessions per role; respect `[parallel.dependencies]` ordering
- [ ] `WorktreePool.Merge(task, strategy)`: open PRs or merge branches on task completion; clean up worktrees
- [x] `task.parallel` RPC method: parse goal + roles from request; delegate to WorktreePool; stream task events back to client
- [x] `task.cancel` RPC method: terminate in-flight role sessions; remove worktrees; mark task failed
- [x] `smj task parallel "<goal>" --roles impl,test,review` subcommand
- [x] `smj task status <task-id>` subcommand: print per-role status and current turn count
- [x] `smj task cancel <task-id>` subcommand
- [ ] Write unit tests: WorktreePool.Start creates correct number of worktrees; dependency ordering respected; cancel cleans up directories

## 3. State Checkpointing

- [ ] Database migration: add `checkpoints` table (id, session_id, turn_n, messages JSON, tool_state JSON, ts)
- [ ] Save checkpoint after every `turn_end` in daemon: serialise `session.Messages` → JSON → upsert row
- [ ] On daemon start: detect sessions with `status = 'in_flight'`; load latest checkpoint; resume session
- [ ] `session.rollback` RPC method: restore session to checkpoint at turn N; discard turns > N from history
- [x] `smj session checkpoint list <session-id>` subcommand: table of turn_n, ts, message count
- [x] `smj session rollback <session-id> <turn-n>` subcommand: calls session.rollback RPC
- [x] `smj session fork <session-id> [--turn <N>]` subcommand: clone checkpoint → new session; print new session ID
- [ ] Write unit tests: save/load round-trip (messages JSON unchanged); rollback to turn 5 discards turns 6+; fork produces independent session

## 4. ACP Endpoint

- [x] Implement `crates/smdjad/acp/server.rs`: HTTP server on `SMEDJA_ACP_PORT` (default 7730)
- [x] `POST /acp/v1/session/new`: create session, return session_id
- [x] `POST /acp/v1/session/:id/prompt`: map PromptRequest → smedja turn; stream response as SSE
- [x] `POST /acp/v1/session/:id/model`: call runner-switch logic (same as `/runner` command)
- [x] `POST /acp/v1/session/:id/mode`: call agent-mode switch (same as `/agent` command)
- [x] `DELETE /acp/v1/session/:id`: close session cleanly
- [x] ACP endpoint disabled by default; activated by `SMEDJA_ACP_PORT` env var
- [ ] Write integration test: create session via ACP; send a prompt; verify streamed response chunks arrive; close session

## 5. Cost Ledger

- [x] Database migration: add `cost_ledger` table (id, session_id, turn_id, runner, model, in_tok, out_tok, cost_usd, ts)
- [x] Bundle `prices.toml` in smdjad binary (embed via `include_bytes!`)
- [x] Insert cost_ledger row on every `turn_end`; compute `cost_usd` from prices table (NULL if model not in table)
- [x] `smj session cost [--session <id>] [--since <duration>]` subcommand: aggregate and print cost table
- [ ] `smj session export <session-id>` subcommand: write JSON cost lineage to stdout
- [ ] `smj prices update [--file <path>]` subcommand: replace prices.toml from file or configured endpoint
- [ ] Write unit tests: cost_usd computed correctly for known model; NULL for unknown model; aggregate across multiple turns

## 6. Per-Tool Permission Rules (BashArity)

- [x] Create `crates/smdjad/tools/arity.rs`: `BashArity(cmd: &str) -> ToolArity` with read/write classification
- [x] Read patterns: `cat`, `ls`, `grep`, `find`, `git log`, `git diff`, `git status`, `wc`, `head`, `tail`, `echo`
- [ ] `ToolGate`: when session role has `bash = ["read"]` in agents.toml, call `BashArity` before bash execution; block write-arity commands with `ToolResult{error: "bash:write blocked for role review"}`
- [ ] `review` role default: `bash = ["read"]` (can be overridden in agents.toml)
- [x] Write unit tests: `BashArity("cat src/foo.rs")` = read; `BashArity("rm foo.rs")` = write; `BashArity("git commit -m ...")` = write; gate blocks write in review role

## 7. Memory Stratification

- [x] Add `Stratum` type to `crates/smedja-memory/src/working.rs`: `Hot | Warm | Cold`; `StrataConfig { hot_depth: usize, warm_depth: usize }`
- [x] `SetBudget(tier: &str, context_window: usize)` method: configure hot/warm/cold boundaries based on tier
- [ ] `BuildPrompt`: assemble hot (always) + warm (structured compact summaries) + cold retrieval (top-K from smedja-vault)
- [ ] Cold retrieval: when current turn references symbols/files that appear only in cold strata, call `smedja-vault.retrieve(query, k=3)`; promote results to bottom of warm before BuildPrompt
- [ ] Context budget table: fast=hot+top5warm, deep=hot+allwarm+top3cold, local=hot+top10warm
- [ ] Write unit tests: hot turns never compacted; warm turns appear as compact summaries; cold retrieval returns expected turns by cosine similarity

## 8. AGENTS.md Compatibility

- [x] Detect `AGENTS.md` in workspace root; if present, inject as skill text into system prompt (Verdent/Warp convention)
- [x] When both `AGENTS.md` and `.smedja/agents.toml` are present: use agents.toml for structured config; AGENTS.md as supplementary skill text
- [ ] `smj workspace agents init` generates a starter `.smedja/agents.toml` from built-in defaults
- [x] Write unit test: AGENTS.md text injected into system prompt; agents.toml role config takes precedence for runner selection

## 9. End-to-End Validation

- [ ] Parallel task smoke test: `/parallel "Add a flag parser" --roles impl,test` → two sessions in two worktrees → both complete → two branches exist in git
- [ ] Checkpoint smoke test: kill smdjad mid-session → restart → session resumes from last checkpoint
- [ ] Rollback smoke test: `smj session rollback <id> 3` → session has only 3 turns → turn 4+ gone from history
- [ ] ACP smoke test: `curl -X POST localhost:7730/acp/v1/session/new` → session_id returned → prompt → streamed response
- [ ] Cost smoke test: 10-turn session with claude-sonnet-4-6 → `smj session cost` shows non-zero cost_usd
- [ ] BashArity smoke test: role=review + `/bash rm foo.rs` → blocked; `/bash git log` → permitted
- [ ] Confirm `cargo test --workspace` passes with zero new failures
