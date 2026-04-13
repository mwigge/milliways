# Service Plan — Milliways

**Type**: tasting-menu
**Maitre d'**: Morgan
**Repo**: `pprojects/milliways` (private, GitHub)
**Language**: Go 1.22+ (Bubble Tea, Lipgloss, Cobra, go-sqlite3)
**Kitchens available**: claude, opencode, gemini, aider, goose, cline
**Services**: 6 (core -> pantry -> kitchens -> TUI -> full pantry -> neovim)
**Palate cleansers**: 6 (one per service)

---

## Operational Context

**Go toolchain**: `go install`, `go build`, `go test`
**Binary output**: `~/.local/bin/milliways` (or Homebrew tap later)
**Config**: `~/.config/milliways/carte.yaml`
**Data**: `~/.config/milliways/milliways.db` (unified PantryDB with migration system)
**MCP servers**: reuse existing MemPalace + CodeGraph installations
**Testing**: `go test ./...` with table-driven tests, mocked kitchen execs
**Lint**: `golangci-lint run` (includes govet, errcheck, staticcheck)
**Go skills**: samber/cc-skills-golang (installed at `ai_local/skills/golang/`)

---

## Orchestration Rules

1. Read `tasks.md` (prep list) to establish current state before every session
2. Find the first service where prerequisites are met and not all courses are served
3. Each course maps to a kitchen -- check `carte.md` before delegating
4. After each course lands: tick the task in prep list
5. At every palate cleanser: stop, verify, never self-advance
6. Tests first on every course, no exceptions
7. Conventional commits: `feat(milliways):`, `fix(milliways):`, etc.
8. `@reviewer` 4-lens review before every commit

---

## Service 1 — Core + First Kitchen (2 weeks, 10 SP) [39/43 tasks done, committed and pushed]

**Delivers**: Working CLI that routes a task to claude, streams output, logs to ledger
**Prerequisites**: Go toolchain installed, claude CLI available
**Status**: Committed and pushed. 4 remaining tasks are integration-level (real CLI exec, JSON output, explain output, jq verification).
**Courses**:

| Course | SP | Kitchen to Build | Deliverable |
|--------|----|-----------------|-------------|
| MW-1: CLI skeleton | 2 | — | Cobra CLI with --kitchen, --json, --recipe, --tui, --explain flags |
| MW-2: Kitchen interface | 1 | — | Kitchen Go interface + Result type |
| MW-3: Claude adapter | 2 | claude | `claude -p` exec, stdout streaming, exit code |
| MW-4: Ledger | 2 | — | ndjson append + SQLite index, ledger record struct |
| MW-5: Keyword router | 1 | — | carte.yaml parsing, keyword -> kitchen lookup |
| MW-5B: Kitchen onboarding | 2 | — | Status detection, `--setup`, first-run welcome, `milliways status`, graceful degradation |
| MW-INT1: Integration test | 2 | — | `milliways "explain hello world"` -> claude responds, ledger entry written |

### Palate Cleanser 1 — CLI MVP

**Verify**:
1. `milliways "explain the auth flow"` -> routes to claude, streams response
2. `milliways --kitchen claude "hello"` -> forces claude kitchen
3. `milliways --json "hello"` -> JSON output with kitchen, duration, exit_code
4. `~/.config/milliways/ledger.ndjson` has one record per invocation
5. `milliways --explain "refactor store.py"` -> prints "keyword: refactor -> aider" without executing
6. `milliways status` -> shows kitchen availability, pantry health, ledger stats
7. `milliways --setup opencode` -> installs if missing, checks auth, reports ready
8. With only claude available -> all tasks route to claude (single-kitchen mode, no errors)
9. `go test ./...` passes with >=80% coverage

---

## Service 2 — Pantry + Sommelier (3 weeks, 15 SP)

**Delivers**: Unified PantryDB, knowledge graphs, circuit breaker, skill catalog, intelligent routing
**Prerequisites**: Palate Cleanser 1 passed, MemPalace + CodeGraph MCP servers available

| Course | SP | Deliverable |
|--------|----|-------------|
| MW-6: PantryDB | 3 | Single `milliways.db` with migration system, typed accessors, table-per-graph schema |
| MW-6B: Ledger migration | 1 | Refactor existing ledger (ndjson + SQLite dual-write) to use PantryDB as backing store |
| MW-7: MemPalace MCP client | 2 | Go MCP client, mempalace_search and mempalace_kg_query from Go |
| MW-8: CodeGraph MCP client | 2 | codegraph_context and codegraph_impact from Go |
| MW-9: GitGraph | 2 | Post-commit hook, churn/hotspot materialization in PantryDB |
| MW-10: QualityGraph | 2 | Extend CodeGraph data with complexity + coverage metrics in PantryDB |
| MW-10B: Circuit breaker | 1 | Read `~/.claude/mode`, filter kitchens and routing paths based on active mode |
| MW-10C: Skill catalog | 1 | Scan skill directories on startup, index available skills with metadata |
| MW-11: Enriched routing | 2 | Sommelier tier 2: consult pantry before routing |
| MW-12: Learned routing | 1 | Sommelier tier 3: query ledger for historical success |

### Palate Cleanser 2 — Intelligent Routing

**Verify**:
1. `milliways.db` exists with versioned schema, all tables migrated from separate .db files
2. `milliways "refactor store.py"` -> sommelier consults CodeGraph (complexity=34), GitGraph (churn=18) -> routes to claude (not aider) because risk is high
3. `milliways --explain "refactor store.py"` -> shows routing reasoning with pantry signals
4. After 10+ ledger entries, `milliways "similar task"` -> learned routing influences choice
5. MemPalace search returns relevant decisions when routing
6. Circuit breaker reads `~/.claude/mode` and restricts routing accordingly
7. `milliways skills` -> lists discovered skills from scanned directories
8. `go test ./...` >=80% coverage including mocked MCP responses

---

## Service 3 — All Kitchens + Recipes (3 weeks, 15 SP)

**Delivers**: All 6 kitchen adapters, recipe engine with failure recovery, async/detached dispatch modes
**Prerequisites**: Palate Cleanser 2 passed

| Course | SP | Deliverable |
|--------|----|-------------|
| MW-13: OpenCode adapter | 2 | `opencode run` exec, --dir scoping, stdout streaming |
| MW-14: Gemini adapter | 1 | `gemini` exec, stdout streaming |
| MW-15: Aider adapter | 2 | `aider --message --yes-always` exec, git integration |
| MW-16: Goose adapter | 1 | `goose` exec, stdout streaming |
| MW-17: Cline adapter | 1 | `cline -y --json` exec, JSON output parsing |
| MW-18: Recipe engine | 2 | Sequential course execution with failure recovery (retry, fallback, save-partial, abandon) |
| MW-19: Context handoff | 2 | Temp file JSON context between courses, --keep-context flag |
| MW-19B: Async dispatch | 2 | Ticket system: submit task, get ticket ID, poll or callback for result |
| MW-19C: Detached dispatch | 1 | Fire-and-forget mode: dispatch to kitchen, log to ledger, no wait |
| MW-19D: Resource quotas | 1 | Configurable per-kitchen limits: max concurrent, max daily dispatches, cost ceiling |

### Palate Cleanser 3 — Full Menu

**Verify**:
1. `milliways --kitchen opencode "add rate limiting to api.py"` -> opencode runs locally
2. `milliways --kitchen gemini "what is DORA-EU Article 25?"` -> gemini responds with search grounding
3. `milliways --recipe implement-feature "add JWT auth"` -> think(claude) -> code(opencode) -> test(opencode) -> review(claude) -> commit(aider) -- all 5 courses complete
4. Recipe with failing course: retry kicks in, then fallback kitchen, then save-partial with context preserved
5. `milliways --async "research DORA compliance"` -> returns ticket ID, `milliways ticket <id>` shows result
6. `milliways --detached "update changelog"` -> dispatches and returns immediately
7. Resource quota exceeded -> dispatch blocked with clear message and next-available estimate
8. Context files in /tmp/milliways-* contain previous course output
9. Ledger shows 5 entries for the recipe, each with correct kitchen
10. `go test ./...` >=80% coverage

---

## Service 4 — TUI (2 weeks, 10 SP)

**Delivers**: Interactive Bubble Tea terminal UI with process map
**Prerequisites**: Palate Cleanser 3 passed

| Course | SP | Deliverable |
|--------|----|-------------|
| MW-20: Bubble Tea app shell | 2 | Main model, update loop, keyboard navigation |
| MW-21: Input component | 1 | Task input with tab-completion for kitchens and recipes |
| MW-22: Output viewport | 2 | Scrollable streaming output with syntax highlighting |
| MW-23: Ledger panel | 1 | Live-updating table of recent dispatches |
| MW-24: Kitchen selector | 2 | Visual kitchen picker, routing explanation panel |
| MW-25A: Process map | 2 | Top-right live minimap: sommelier reasoning, course progress, elapsed time |

### Palate Cleanser 4 — Interactive Mode

**Verify**:
1. `milliways --tui` opens interactive mode with 4 panels: input (bottom), output (center), process map (top-right), ledger (bottom-right)
2. Type a task -> process map shows sommelier reasoning -> routes to kitchen -> output streams in viewport
3. During recipe: process map shows course list with check/active/pending progress
4. Ledger panel updates after each dispatch
5. Tab-complete shows kitchen names and recipe names
6. Ctrl+C gracefully kills current kitchen subprocess
7. Dark mode theme applied via Lipgloss
8. `--verbose` (headless) prints `[sommelier]` and `[dispatch]` lines to stderr
9. Process map shows async ticket status and detached dispatch indicators

---

## Service 5 — Full Pantry + Carte Integration (3 weeks, 12 SP)

**Delivers**: All knowledge graphs, tiered reporting, hook chain, carte.md integration, routing accuracy measurement
**Prerequisites**: Palate Cleanser 4 passed

| Course | SP | Deliverable |
|--------|----|-------------|
| MW-25: DepGraph | 2 | Parse lock files (go.mod, package.json, Cargo.toml, pdm.lock), CVE lookup via API |
| MW-26: TopologyGraph | 2 | Import from simulator-topology-visualization SQLite, service dependency lookup |
| MW-27: Carte.md parser | 2 | Read OpenSpec carte.md, map tasks to kitchens with context injection |
| MW-28: opsx:apply integration | 1 | `milliways --recipe opsx:apply "change-name"` reads tasks.md + carte.md, executes each task via mapped kitchen |
| MW-29: Routing accuracy | 1 | `milliways report --accuracy` -- routing success rate by kitchen, cost breakdown, recommendation for carte.yaml tuning |
| MW-29B: Tiered CLI feedback | 1 | `milliways report --tiered` -- summary/detail/deep tiers, actionable recommendations per tier |
| MW-29C: Hook chain | 2 | 6 lifecycle events: pre-route, post-route, pre-dispatch, post-dispatch, pre-recipe, post-recipe -- configurable shell hooks |
| MW-29D: Routing accuracy measurement | 1 | Compare keyword-only vs enriched vs learned routing over time, expose as `milliways report --routing-drift` |

### Palate Cleanser 5 — Full Pantry

**Verify**:
1. `milliways report` shows routing stats from ledger: success %, cost, duration per kitchen
2. `milliways report --tiered` shows summary tier by default, `--tiered=deep` shows per-task breakdown
3. `milliways --recipe opsx:apply "wave3-quick-wins"` reads tasks.md + carte.md, routes each task correctly
4. DepGraph flags a CVE-exposed dependency -> sommelier routes to claude (security review)
5. TopologyGraph shows service with 5 dependents -> routes to claude (careful review)
6. Hook chain fires: pre-route hook injects custom context, post-dispatch hook writes audit log
7. Routing accuracy from ledger is measurably better than keyword-only (compare first 50 vs last 50 entries)
8. `milliways report --routing-drift` shows trend over time

---

## Service 6 — Neovim Plugin (2 weeks, 8 SP)

**Delivers**: milliways.nvim Lua plugin for seamless Neovim integration
**Prerequisites**: Palate Cleanser 5 passed, Milliways binary on PATH

| Course | SP | Deliverable |
|--------|----|-------------|
| MW-30: Plugin skeleton | 2 | milliways.nvim Lua plugin structure, lazy.nvim compatible, setup function |
| MW-31: Commands | 2 | `:Milliways <prompt>`, `:MilliwaysKitchen <name>`, `:MilliwaysRecipe <name>`, `:MilliwaysStatus`, `:MilliwaysExplain` |
| MW-32: Context injection | 1 | Auto-inject current buffer, visual selection, LSP diagnostics, git diff into task context |
| MW-33: Floating window | 2 | Streaming output in floating window, syntax-highlighted, scrollable, dismissible with `q` or `<Esc>` |
| MW-34: Keybindings | 1 | Configurable keybindings: `<leader>mw` dispatch, `<leader>me` explain, `<leader>mk` kitchen picker, `<leader>ms` status |

### Palate Cleanser 6 — Neovim Integration

**Verify**:
1. `:Milliways "explain this function"` with cursor on a function -> dispatches with function context, streams result in floating window
2. Visual select code block -> `:Milliways "refactor this"` -> selected code injected as context
3. `:MilliwaysKitchen claude` -> pins kitchen, `:Milliways "review"` -> uses claude
4. `:MilliwaysStatus` -> shows kitchen availability in floating window
5. `:MilliwaysExplain "refactor store.py"` -> shows routing reasoning without executing
6. Floating window auto-sizes, supports scroll, closes on `q`
7. `<leader>mw` from normal mode prompts for task in command line, dispatches on Enter
8. Plugin loads cleanly with lazy.nvim, no errors on `:checkhealth milliways`

---

## Implementation Standards

- **TDD**: Write failing test before implementation for every course
- **Coverage**: >=80% on all changed files -- `go test -coverprofile=coverage.out ./...`
- **Lint**: `golangci-lint run` clean (govet, errcheck, staticcheck, gosimple)
- **Format**: `gofmt` applied to all files
- **Go skills**: samber/cc-skills-golang available at `ai_local/skills/golang/` -- consult for idiomatic patterns
- **Commits**: Conventional -- `feat(milliways): add claude kitchen adapter`
- **Branch per course**: `feat/MW-{N}/{description}`
- **Review**: `@reviewer` 4-lens review (correctness, security, performance, maintainability) before every commit

---

## Reporting Chain

**Parent tasting menu**: none -- top-level programme
**Completion signal**: Palate Cleanser 6 passed -- Milliways routes tasks across all 6 kitchens with pantry-informed intelligence, measurable routing accuracy improvement, and seamless Neovim integration via `:Milliways` floating window
