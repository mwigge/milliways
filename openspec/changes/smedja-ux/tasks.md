## 1. Turn Blocks

- [x] Create `bin/smedja/src/blocks.rs`: `TurnBlock` struct, `toolEntry`, `blockStatus` enum, `BlockStore` (in-memory ring, last 200 turns)
- [x] Replace flat output in turn rendering with `TurnBlock` state machine driven by RPC events
- [x] Print turn block header on `turn_start`: `"┌─ turn N ── tier · model ── … ──────────────┐"`
- [x] Print `"▸ tool_name args"` on `tool_call`; update with `outcome` on `tool_result`
- [x] Print turn block footer on `turn_end`: token count, latency, status badge
- [x] Block navigation: `b` opens block browser (arrow keys to select); `c` copies selected block text to clipboard; `r` replays turn
- [x] Persist blocks to `BlockStore`; `smj session blocks --session <id>` lists blocks
- [x] Write unit tests: `TurnBlock` state transitions (streaming → tool_call → tool_result → complete); render output matches expected string
- [x] Run `cargo test -p smedja` clean

## 2. Inline Diff Viewer

- [x] On `edit_file` tool results, extract unified diff from `tool_result.content`; store in `toolEntry.diff`
- [x] `d` key on selected tool entry: expand inline diff (max 20 lines, with `+`/`-` prefixes and syntax highlight via syntect)
- [x] `D` key: show full diff in scrollable overlay pane
- [x] Collapse back on second `d` press or `Esc`
- [x] Write unit tests: diff extraction from tool_result; inline render truncation at 20 lines; syntect highlighting applies

## 3. Modular Status Bar

- [x] Create `bin/smedja/src/statusbar.rs`: `StatusModule` trait, `ModuleCtx` struct, `render_status_bar` runner
- [x] Built-in modules: `tier`, `model`, `context_pct` (from WorkingMemory), `modes`, `smedja_task`, `git_branch`, `time`
- [x] Module tasks with configurable timeout (default 30ms per module); skip on timeout
- [x] TOML config section `[statusbar]`: `format` string, per-module `[statusbar.module_name]` sections (disabled, symbol, style, threshold)
- [x] Replace current status hint line with modular status bar; update on every turn-end event
- [x] Write unit tests: module timeout → segment omitted; disabled module → not rendered; format string reordering

## 4. Workspace Skills

- [x] `load_workspace_skills(dir: &Path) -> Vec<String>`: scans `<dir>/.smedja/skills/*.md`; returns content slices
- [x] Inject skills into WorkingMemory as system-slot message before `stable_prefix` is set at session start
- [x] `smj workspace skills list` subcommand: print file names and token counts
- [x] `smj workspace skills add <file>` subcommand: copy to `.smedja/skills/`; create dir if absent
- [x] Write unit tests: skills loaded from fixture dir; injected message appears before stable_prefix watermark; empty dir → no injection, no error

## 5. Input Staging Queue

- [x] Add `staging_queue: Vec<StagedAction>` to `ChatLoop` struct
- [x] `/stage <tool> <json-args>` command: parse, append to queue, print `"⏸ staged: <tool>"`
- [x] `/run` command: drain queue in order; dispatch each as tool_call RPC; render results as turn-block tool entries
- [x] `/unstage [N]` command: remove item N from queue (or all if no N); print remaining queue
- [x] Action log: staged items shown with `⏸ pending` prefix; executed items updated to `▸` on run
- [x] Write unit tests: stage + run → tool dispatched; unstage removes correct item; empty run → no-op

## 6. Context Rail (ratatui)

- [x] Create `bin/smedja/src/context_rail.rs`: `ContextRail` widget, `ContextSlot` struct, `SlotStyle` enum
- [x] Slot data from `WorkingMemory.slot_usage()` after each turn
- [x] Render: per-slot row with fill bar (Unicode block chars), name, percentage; colour by threshold
- [x] Ctrl-R toggles rail visibility; width: 25 columns fixed; collapses on terminal width < 100
- [x] Write unit tests: slot fill bar renders correct Unicode char count; colour thresholds applied at 60%/80%

## 7. UX Ports and Integration

- [x] Implement `TurnBlock` as `ratatui::Widget` in `bin/smedja/src/blocks.rs`
- [x] Implement modular status bar in `crates/mt-statusbar` (rayon par_iter, same module spec)
- [x] Wire workspace skills discovery into `smedja-memory` stable-prefix injection
- [x] Implement input staging in `bin/smedja/src/staging.rs`

## 8. End-to-End Validation

- [ ] Smoke test: run a 5-turn agent session; verify each turn produces a visible TurnBlock with header/footer
- [ ] Smoke test: `edit_file` turn; press `d` on tool entry; verify inline diff renders with correct +/- lines
- [ ] Smoke test: `smj workspace skills add docs/conventions.md`; start session; verify skill content injected before stable_prefix
- [ ] Smoke test: `/stage edit_file foo.rs "a" "b"` → `/run` → verify tool called with correct args
- [ ] Smoke test: status bar shows correct context% after a turn that fills 50% of context window
- [ ] Confirm `cargo test --workspace` passes with zero new failures
