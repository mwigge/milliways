# Tasks — milliways-nvim-context

## Service 1 — Lua Context Collection (4 SP)

### Course NC-1: Plugin module refactor [1 SP]

- [x] NC-1.1 Split `nvim-plugin/lua/milliways/init.lua` into `init.lua`, `context.lua`, `commands.lua`, `float.lua`, `kitchens.lua`
- [x] NC-1.2 Preserve all existing public commands and default keybindings with no regression
- [x] NC-1.3 plenary.nvim smoke test: plugin loads cleanly, commands are registered

### Course NC-2: Core collectors [1.5 SP]

- [x] NC-2.1 `Context.collect_buffer()` — path, filetype, modified flag, total lines, visible range
- [x] NC-2.2 `Context.collect_cursor()` — line, column, treesitter scope (function/class/block) when parser available
- [x] NC-2.3 `Context.collect_selection()` — start/end lines + text, only when called in visual mode
- [x] NC-2.4 `Context.collect_project()` — git-aware root detection, primary language, open buffers, recent files
- [x] NC-2.5 plenary.nvim specs: each collector covered with a fixture buffer

### Course NC-3: LSP and git collectors [1 SP]

- [x] NC-3.1 `Context.collect_lsp(scope)` — diagnostics filtered by severity; scope = "visible" (default) or "file"
- [x] NC-3.2 `Context.collect_git()` — branch, dirty flag, files_changed, ahead/behind counts; shells out to git
- [x] NC-3.3 Graceful degradation: absent LSP or non-git dir returns `nil`, never errors
- [x] NC-3.4 plenary.nvim specs for both, including absence cases

### Course NC-4: Bundle builder [0.5 SP]

- [x] NC-4.1 `Context.build(opts)` assembles the full bundle with `schema_version="1"`
- [x] NC-4.2 Opt-in collectors: selection (auto in visual mode), quickfix, loclist
- [x] NC-4.3 Per-collector timeout (default 15ms) and total-budget cap (default 64kb, 50ms wall clock)
- [x] NC-4.4 Unit tests: budget overflow truncates cleanly, degraded collectors return `nil`

- [ ] 🍋 **Palate Cleanser 1** — Running `:lua print(vim.inspect(require('milliways.context').build()))` on a real buffer produces a well-formed bundle in under 50ms.

---

## Service 2 — Go-Side Structured Context Ingestion (3 SP)

### Course NC-5: `editorcontext` package [1 SP]

- [x] NC-5.1 Create `internal/editorcontext/` with `Bundle`, `BufferState`, `CursorState`, `Selection`, `Diagnostic`, `GitState`, `ProjectMetadata`, `QuickfixEntry`, `LoclistEntry` types
- [x] NC-5.2 JSON codec with `schema_version` validation; reject unknown major versions with typed error
- [x] NC-5.3 Unit tests: round-trip, missing-fields handling, unknown-version rejection

### Course NC-6: CLI flags [0.5 SP]

- [x] NC-6.1 Add `--context-json` and `--context-stdin` to `cmd/milliways/`
- [x] NC-6.2 Preserve existing `--context-file` (reconstruct into minimal `Bundle`)
- [x] NC-6.3 Integration test: invoking milliways with `--context-stdin` pipes a bundle and dispatch completes normally

### Course NC-7: Sommelier pantry signals from editor context [1 SP]

- [x] NC-7.1 Add editor-context signal extraction to the pantry-signals tier: `editor.lsp_error_count`, `editor.in_test_file`, `editor.dirty_churn`, `editor.language`
- [x] NC-7.2 `carte.yaml` schema extension: per-kitchen `weight_on` map honours editor-context keys
- [x] NC-7.3 Unit tests: each signal derivation, weight composition, fallback when bundle absent

### Course NC-8: Continuation payload integration [0.5 SP]

- [x] NC-8.1 `internal/conversation/continue.go` accepts an optional `editorcontext.Bundle`
- [x] NC-8.2 Condensed editor-context section rendered, capped at 500 tokens
- [x] NC-8.3 Unit tests: section renders cleanly, truncation preserves highest-signal entries

- [ ] 🍋 **Palate Cleanser 2** — `milliways --context-stdin` accepts a real nvim-generated bundle and routes based on it. Continuation payloads include a condensed editor-context section.

---

## Service 3 — Nvim Command Parity with TUI (2 SP)

### Course NC-9: `:MilliwaysSwitch` / `:MilliwaysStick` / `:MilliwaysBack` [1 SP]

- [x] NC-9.1 Implement commands in `commands.lua`; each invokes milliways headless with the appropriate flag and updates the float header
- [x] NC-9.2 Tab-completion on kitchen names via `complete=customlist`
- [x] NC-9.3 `:MilliwaysSwitch` without arg opens `vim.ui.select` of available kitchens
- [x] NC-9.4 plenary.nvim specs covering happy path + error cases (unknown kitchen, no prior switch)

### Course NC-10: `:MilliwaysKitchens` with Telescope support [0.5 SP]

- [x] NC-10.1 Detect Telescope presence; use it if available, fall back to `vim.ui.select`
- [x] NC-10.2 Show kitchen status, capabilities, and current sticky mode in the picker
- [x] NC-10.3 Selection dispatches `:MilliwaysSwitch <chosen>`

### Course NC-11: `:MilliwaysReroute` and default keybindings [0.5 SP]

- [x] NC-11.1 `:MilliwaysReroute` forces sommelier re-evaluation on the current conversation
- [x] NC-11.2 Default keybindings: `<leader>ms`, `<leader>m.`, `<leader>m,`, `<leader>mK` — non-conflicting with existing
- [x] NC-11.3 `which-key.nvim` descriptions registered when which-key is present

- [ ] 🍋 **Palate Cleanser 3** — `:MilliwaysSwitch codex` mid-conversation in nvim produces the same substrate-level switch as `/switch codex` in the TUI. A second milliways instance sees it.

---

## Service 4 — UX Polish (1.5 SP)

### Course NC-12: Line-by-line streaming [0.5 SP]

- [x] NC-12.1 Switch `jobstart` from `stdout_buffered=true` to per-line streaming
- [x] NC-12.2 Append each line to the float buffer as it arrives; autoscroll unless user cursor has moved
- [x] NC-12.3 plenary.nvim spec with a fake binary emitting one line per 100ms

### Course NC-13: Lineage header [0.5 SP]

- [x] NC-13.1 First line of float shows `claude → codex | sticky | <Tab> recent <leader>mK kitchens` (dynamic)
- [x] NC-13.2 Updated in place on segment change
- [x] NC-13.3 plenary.nvim spec: header reflects segment-change events from the substrate

### Course NC-14: `<Tab>` recent conversations [0.5 SP]

- [x] NC-14.1 `<Tab>` inside float cycles through 3 most recent conversations from MemPalace
- [x] NC-14.2 Preview shown above the input line; `<CR>` resumes in place
- [x] NC-14.3 Fallback behaviour when MemPalace substrate is unavailable — `<Tab>` is a no-op with a notice

- [ ] 🍋 **Palate Cleanser 4** — The floating window streams as work happens, shows current provider lineage, and can cycle through recent conversations without leaving the buffer.

---

## Service 5 — Verification (1.5 SP)

### Course NC-15: plenary.nvim test suite [0.5 SP]

- [x] NC-15.1 `nvim-plugin/tests/` directory with spec files per module
- [x] NC-15.2 `make plugin-test` target runs the specs against a pinned headless nvim
- [x] NC-15.3 CI step runs `make plugin-test` after `make smoke`

### Course NC-16: End-to-end smoke scenario [0.5 SP]

- [x] NC-16.1 `testdata/smoke/scenarios/nvim-context.sh` — invokes milliways binary with a representative JSON bundle and asserts routing picks up editor-context signals
- [x] NC-16.2 Integrated into the existing `scripts/smoke.sh` matrix
- [x] NC-16.3 Scenario is deterministic — no real nvim required, just a pre-built JSON bundle fixture

### Course NC-17: Documentation [0.5 SP]

- [x] NC-17.1 Update `nvim-plugin/README.md` with L2 context hydration, new commands, Telescope support, keybindings
- [x] NC-17.2 Add section on privacy: what's collected, what's sent, how to opt out per-collector
- [x] NC-17.3 Add troubleshooting section: LSP not installed, git not in repo, Telescope not installed

- [ ] 🍽️ **Grand Service** — The nvim plugin is a first-class milliways surface. Editor context reaches the sommelier automatically. Switch commands behave identically to their TUI counterparts. Collection stays under the 50ms budget. Existing plugin users see no regression.
