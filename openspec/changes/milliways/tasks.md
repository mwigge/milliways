# Prep List — Milliways

## Service 1 — Core + First Kitchen (10 SP)

### Course MW-1: CLI Skeleton [2 SP]

- [x] MW-1.1 `go mod init github.com/mwigge/milliways` with Go 1.22+
- [x] MW-1.2 Add Cobra dependency, create `cmd/milliways/main.go` with root command
- [x] MW-1.3 Flags: `--kitchen` (force kitchen), `--json` (JSON output), `--recipe` (multi-course), `--tui` (interactive), `--explain` (show routing without executing), `--keep-context` (preserve temp files)
- [x] MW-1.4 `--version` flag, `--help` with restaurant-themed descriptions
- [x] MW-1.5 Config loader: read `~/.config/milliways/carte.yaml`, merge with defaults
- [x] MW-1.6 Unit tests for config loading and flag parsing
- [ ] MW-1.7 Refactor: replace separate ledger.db with unified milliways.db via PantryDB pattern

### Course MW-2: Kitchen Interface [1 SP]

- [x] MW-2.1 Define `Kitchen` interface: `Name() string`, `Exec(ctx, Task) (Result, error)`, `Stations() []string`, `CostTier() CostTier`
- [x] MW-2.2 Define `Task` struct: `Prompt`, `Dir`, `Context`, `OnLine func(string)`
- [x] MW-2.3 Define `Result` struct: `ExitCode`, `Output`, `Duration`
- [x] MW-2.4 Define `CostTier` enum: `CostTierUnknown`, `Cloud`, `Local`, `Free`
- [x] MW-2.5 `KitchenRegistry` — load from carte.yaml, lookup by name or station
- [x] MW-2.6 Unit tests for registry lookup

### Course MW-3: Claude Kitchen Adapter [2 SP]

- [x] MW-3.1 Implement `Status()` — `exec.LookPath` + allowedCmds allowlist; report unavailable kitchens in `--explain` and skip during routing
- [x] MW-3.2 Implement `GenericKitchen.Exec()` — exec CLI with stdout streaming via `bufio.Scanner`, scanner.Err() checked
- [x] MW-3.3 Capture exit code, compute duration
- [x] MW-3.4 Handle timeout via `context.WithTimeout` (default 5 minutes, configurable)
- [x] MW-3.5 Handle missing binary gracefully (error message with install instructions)
- [x] MW-3.6 Unit tests: Exec happy path, disallowed cmd, not-ready kitchen, defensive copies
- [ ] MW-3.7 Integration test: real `claude -p "say hello"` returns non-empty output

### Course MW-4: Ledger [2 SP]

- [x] MW-4.1 Define `LedgerEntry` struct: ts, task_hash, task_type, kitchen, station, file, duration_s, exit_code, cost_est_usd, outcome
- [x] MW-4.2 ndjson writer: append one JSON line to `~/.config/milliways/ledger.ndjson` (0600 permissions)
- [x] MW-4.3 SQLite writer: INSERT into `ledger.db` (same fields, indexed by kitchen + task_type + outcome)
- [x] MW-4.4 Dual write on every kitchen dispatch (ndjson + SQLite via DualWriter)
- [x] MW-4.5 `milliways report` subcommand: read ledger ndjson, print summary (dispatches per kitchen, success rate)
- [x] MW-4.6 Unit tests for write + read + hash determinism

### Course MW-5: Keyword Router [1 SP]

- [x] MW-5.1 Parse `routing.keywords` from carte.yaml into sorted slice (longest-match-first, deterministic)
- [x] MW-5.2 `Route(prompt string) Decision` — scan prompt for keywords, return first match with reason
- [x] MW-5.3 `--explain` mode: print routing decision without executing
- [x] MW-5.4 Fallback: if no keyword matches, use `routing.default`; cascade to budget_fallback; cascade to first ready
- [x] MW-5.5 Unit tests: keyword matching, longest-match, deterministic order, fallback, unavailable kitchen, force route

### Course MW-5B: Kitchen Onboarding [2 SP]

- [x] MW-5B.1 Three-level status check per kitchen: `Ready` / `NeedsAuth` / `NotInstalled` / `Disabled` (+ `StatusUnknown` sentinel)
- [ ] MW-5B.2 Auth probe per kitchen (e.g. `claude -p "test"` exit code, `gcloud auth list` for gemini)
- [x] MW-5B.3 Install helpers table: kitchen -> install command (brew/npm/pip)
- [x] MW-5B.4 `milliways setup <kitchen>` — run install command, wait, re-check status, guide auth if needed
- [ ] MW-5B.5 First-run welcome screen: list all kitchens with status, offer [i]nstall / [a]uth / [s]kip
- [x] MW-5B.6 `milliways status` — display kitchen availability, action column, ledger stats
- [x] MW-5B.7 Graceful degradation: sommelier routes only to `Ready` kitchens, skips others with fallback
- [x] MW-5B.8 Single-kitchen mode: works with just one kitchen (reduced routing, no errors)
- [x] MW-5B.9 carte.yaml `enabled: true/false` per kitchen — user opt-out respected
- [x] MW-5B.10 Unit tests: status detection (Diagnose, ReadyCounts), setup ready/disabled paths

### Course MW-INT1: Integration Test [2 SP]

- [x] MW-INT1.1 Integration test: config -> registry -> sommelier -> exec -> dual ledger (happy path, fallback, single-kitchen, explain, force)
- [x] MW-INT1.2 Exec integration tests: streaming output, non-zero exit code, context timeout, nil OnLine, Dir scoping
- [ ] MW-INT1.3 `milliways --json "hello"` -> valid JSON output (CLI-level test, needs test harness)
- [ ] MW-INT1.4 `milliways --explain "refactor store.py"` -> prints routing reasoning
- [ ] MW-INT1.5 Verify ledger.ndjson parseable by `jq`

- [ ] 🍋 **Palate Cleanser 1** — CLI MVP verified: route, stream, log, explain all working

---

## Service 2 — Pantry + Sommelier (18 SP)

### Course MW-6: PantryDB — Unified Database [3 SP]

- [ ] MW-6.1 Create `internal/pantry/db.go` — PantryDB struct with single `*sql.DB` connection to milliways.db (WAL mode, busy_timeout=5000)
- [ ] MW-6.2 Create `internal/pantry/migrations/001_initial.sql` — full schema (mw_schema, mw_ledger, mw_tickets, mw_gitgraph, mw_quality, mw_deps, mw_routing, mw_quotas) with indexes
- [ ] MW-6.3 Implement migration runner — read `go:embed` migrations, apply sequentially, update mw_schema
- [ ] MW-6.4 Implement `LedgerStore` — Insert, Stats, Total (replaces internal/ledger/store.go)
- [ ] MW-6.5 Implement `TicketStore` — Create, Get, List, UpdateStatus
- [ ] MW-6.6 Implement `RoutingStore` — RecordOutcome, BestKitchen(task_type, file_profile)
- [ ] MW-6.7 Implement `QuotaStore` — Increment, CheckLimit, DailyUsage
- [ ] MW-6.8 Typed accessors: `db.Ledger()`, `db.Tickets()`, `db.Routing()`, `db.Quotas()`
- [ ] MW-6.9 Refactor cmd/milliways/main.go to use PantryDB instead of separate ledger.NewDualWriter
- [ ] MW-6.10 Delete internal/ledger/store.go, dual.go (absorbed into pantry)
- [ ] MW-6.11 Keep internal/ledger/writer.go for ndjson-only audit trail
- [ ] MW-6.12 Unit tests for PantryDB: migration, ledger CRUD, routing scores, quota checks (>=80% coverage)

### Course MW-7: MemPalace Integration [2 SP]

- [ ] MW-7.1 `MemPalaceClient` wrapping MCP client
- [ ] MW-7.2 `Search(query, wing, limit) -> []Drawer` — call `mempalace_search`
- [ ] MW-7.3 `KGQuery(subject, predicate) -> []Triple` — call `mempalace_kg_query`
- [ ] MW-7.4 Inject search results into sommelier context: "MemPalace says: {decision summary}"
- [ ] MW-7.5 Unit tests with mocked MCP responses

### Course MW-8: CodeGraph Integration [2 SP]

- [ ] MW-8.1 `CodeGraphClient` wrapping MCP client
- [ ] MW-8.2 `Context(task) -> string` — call `codegraph_context`
- [ ] MW-8.3 `Impact(symbol, depth) -> ImpactResult` — call `codegraph_impact`
- [ ] MW-8.4 Extract file complexity + caller count as routing signals
- [ ] MW-8.5 Unit tests with mocked MCP responses

### Course MW-9: GitGraph [2 SP]

- [ ] MW-9.1 Implement `GitGraphStore` in pantry package — Sync, IsHotspot, FileStability
- [ ] MW-9.2 `gitgraph sync` command: parse `git log --numstat` for target repo, upsert file stats
- [ ] MW-9.3 Materialize stability: STABLE (churn < 3/90d), ACTIVE (3-15), VOLATILE (>15)
- [ ] MW-9.4 Post-commit hook script: `milliways pantry gitgraph sync --repo .`
- [ ] MW-9.5 `IsHotspot(file) -> (churn, stability, lastAuthor)` query function
- [ ] MW-9.6 Unit tests with fixture git history

### Course MW-10: QualityGraph [2 SP]

- [ ] MW-10.1 QualityGraph schema in PantryDB (mw_quality table): metrics(file, function, cyclomatic_complexity, cognitive_complexity, coverage_pct, smell_count, last_updated)
- [ ] MW-10.2 Populate from CodeGraph AST data (tree-sitter already parses function bodies)
- [ ] MW-10.3 Populate coverage from pytest/vitest coverage JSON output (optional, when available)
- [ ] MW-10.4 `FileRisk(file) -> (complexity, coverage, smells)` query function
- [ ] MW-10.5 Unit tests

### Course MW-11: Enriched Routing (Sommelier Tier 2) [2 SP]

- [ ] MW-11.1 `EnrichedRoute(prompt, file) -> (kitchen, reason, signals)` — consults pantry
- [ ] MW-11.2 Signal aggregation: CodeGraph complexity + GitGraph churn + QualityGraph coverage
- [ ] MW-11.3 Risk scoring: LOW (all green) -> keyword routing; MEDIUM (one amber) -> careful kitchen; HIGH (multiple red) -> claude
- [ ] MW-11.4 `--explain` shows all pantry signals in routing reasoning
- [ ] MW-11.5 Unit tests with mock pantry responses

### Course MW-11B: Circuit Breaker [2 SP]

- [ ] MW-11B.1 Read `~/.claude/mode` on SessionStart (default: "private" if file missing)
- [ ] MW-11B.2 Define path restrictions per mode in carte.yaml
- [ ] MW-11B.3 PreRoute hook: check if task targets a blocked path -> hard stop with clear error
- [ ] MW-11B.4 Filter kitchen list by mode (carte.yaml `kitchens.X.modes: [company, private]`)
- [ ] MW-11B.5 Pass mode as env var to kitchen subprocess (MILLIWAYS_MODE=company)
- [ ] MW-11B.6 Unit tests: mode detection, path filtering, kitchen filtering

### Course MW-11C: Skill Catalog [1 SP]

- [ ] MW-11C.1 On SessionStart scan `~/.claude/skills/` for SKILL.md files — extract name + description
- [ ] MW-11C.2 Scan `~/.config/opencode/plugins/` for .ts files — extract plugin names
- [ ] MW-11C.3 Build in-memory catalog: kitchen -> []skill_name
- [ ] MW-11C.4 Sommelier uses catalog: if task mentions "security" and claude has "security-review" skill -> boost claude
- [ ] MW-11C.5 Unit tests with fixture skill directories

### Course MW-12: Learned Routing (Sommelier Tier 3) [1 SP]

- [ ] MW-12.1 Query ledger.db: for this task_type + file_complexity_bucket + file_churn_bucket, which kitchen had highest success rate?
- [ ] MW-12.2 Minimum 5 data points before learned routing overrides keyword
- [ ] MW-12.3 `--explain` shows learned preference when applicable
- [ ] MW-12.4 Unit tests with fixture ledger data

### Course MW-12B: Tiered-CLI Feedback [2 SP]

- [ ] MW-12B.1 Sommelier classifies task_type on every dispatch (think/code/refactor/search/review/test)
- [ ] MW-12B.2 PostDispatch writes to mw_routing: increment success_count or failure_count for (task_type, kitchen)
- [ ] MW-12B.3 `milliways report --tiered` — per-kitchen best task type, multi-CLI composite score, lift vs best single-CLI
- [ ] MW-12B.4 Unit tests for classification and report generation

- [ ] 🍋 **Palate Cleanser 2** — Intelligent routing verified: pantry signals influence routing, --explain shows reasoning, learned routing activates after sufficient data

---

## Service 3 — All Kitchens + Recipes (18 SP)

### Course MW-13: OpenCode Adapter [2 SP]

- [ ] MW-13.1 Implement `OpenCodeKitchen` — exec `opencode run {prompt}`
- [ ] MW-13.2 `--dir` flag support: scope to specific repository
- [ ] MW-13.3 Stream stdout, capture exit code
- [ ] MW-13.4 Parse `opencode run -o json` for structured output when available
- [ ] MW-13.5 Unit tests + integration test

### Course MW-14: Gemini Adapter [1 SP]

- [ ] MW-14.1 Implement `GeminiKitchen` — exec `gemini {prompt}`
- [ ] MW-14.2 Stream stdout, capture exit code
- [ ] MW-14.3 Unit tests

### Course MW-15: Aider Adapter [2 SP]

- [ ] MW-15.1 Implement `AiderKitchen` — exec `aider --message {prompt} --yes-always --no-suggest-shell-commands`
- [ ] MW-15.2 Pass `--file` for targeted files when context provides them
- [ ] MW-15.3 Detect git commits made by aider (parse stdout for commit hash)
- [ ] MW-15.4 Unit tests + integration test

### Course MW-16: Goose Adapter [1 SP]

- [ ] MW-16.1 Implement `GooseKitchen` — exec `goose {prompt}`
- [ ] MW-16.2 Stream stdout, capture exit code
- [ ] MW-16.3 Unit tests

### Course MW-17: Cline Adapter [1 SP]

- [ ] MW-17.1 Implement `ClineKitchen` — exec `cline -y --json {prompt}`
- [ ] MW-17.2 Parse JSON output for structured result
- [ ] MW-17.3 Unit tests

### Course MW-18: Recipe Engine [2 SP]

- [ ] MW-18.1 Parse recipe definitions from carte.yaml
- [ ] MW-18.2 `ExecuteRecipe(name, prompt) -> []Result` — run courses sequentially
- [ ] MW-18.3 Each course receives original prompt + accumulated context
- [ ] MW-18.4 Stop on first failure (exit_code != 0) unless `--continue-on-error`
- [ ] MW-18.5 Log each course separately in ledger
- [ ] MW-18.6 Unit tests with mocked kitchen executions

### Course MW-18B: Async Dispatch [2 SP]

- [ ] MW-18B.1 `milliways --async "task"` — spawn kitchen in background goroutine, return ticket ID immediately
- [ ] MW-18B.2 Write ticket to mw_tickets (status: running, pid, output_path)
- [ ] MW-18B.3 On completion: update ticket (status: complete/failed, exit_code, completed_at), write ledger entry
- [ ] MW-18B.4 `milliways ticket {id}` — show ticket status, output path
- [ ] MW-18B.5 `milliways tickets` — list all tickets with status
- [ ] MW-18B.6 Unit tests

### Course MW-18C: Detached Dispatch [2 SP]

- [ ] MW-18C.1 `milliways --detach "task"` — spawn kitchen as OS process (survives Milliways exit)
- [ ] MW-18C.2 Redirect stdout/stderr to ~/.config/milliways/detached/{pid}.log
- [ ] MW-18C.3 Write ticket with mode="detached", pid set
- [ ] MW-18C.4 `milliways detached` — list detached processes with status (check if pid still running)
- [ ] MW-18C.5 Completion detection: poll pid existence, update ticket on exit
- [ ] MW-18C.6 Unit tests

### Course MW-19: Context Handoff [2 SP]

- [ ] MW-19.1 After each course: write `/tmp/milliways-{recipe-id}-{n}.json` with output, files_changed, exit_code
- [ ] MW-19.2 Before next course: inject previous course output into prompt (configurable: full text, summary, or files-only)
- [ ] MW-19.3 `--keep-context` flag: don't delete temp files after recipe completes
- [ ] MW-19.4 Context size limit: if previous output > 10KB, summarize via utility kitchen (haiku/qwen)
- [ ] MW-19.5 Unit tests for context serialization and injection

### Course MW-19B: Resource Quotas [2 SP]

- [ ] MW-19B.1 Parse quotas from carte.yaml (per-kitchen + global)
- [ ] MW-19B.2 PreDispatch hook: check max_concurrent (count running tickets for kitchen)
- [ ] MW-19B.3 PreDispatch hook: check daily_dispatches (query mw_quotas)
- [ ] MW-19B.4 PreDispatch hook: check global max_total_concurrent
- [ ] MW-19B.5 PreDispatch hook: check system memory (sysctl on macOS) against pause_if_memory_above
- [ ] MW-19B.6 Queue dispatch if at limit (wait for slot or timeout)
- [ ] MW-19B.7 Update mw_quotas after each dispatch
- [ ] MW-19B.8 Unit tests for quota enforcement

### Course MW-19C: Recipe Failure Recovery [1 SP]

- [ ] MW-19C.1 Parse on_failure strategy per recipe (stop/retry-course/skip-course/restart-from)
- [ ] MW-19C.2 On course failure: execute configured strategy
- [ ] MW-19C.3 Retry: same kitchen or fallback kitchen, inject partial output
- [ ] MW-19C.4 Save-partial: write to file, notify user
- [ ] MW-19C.5 Unit tests for each strategy

- [ ] 🍋 **Palate Cleanser 3** — Full menu verified: all 6 kitchens dispatch, recipe runs 5-course meal, context flows between courses

---

## Service 4 — TUI (8 SP)

### Course MW-20: Bubble Tea App Shell [2 SP]

- [ ] MW-20.1 Main Bubble Tea model: Init, Update, View
- [ ] MW-20.2 Layout: input panel (bottom), output viewport (center), ledger panel (right)
- [ ] MW-20.3 Keyboard: Enter submits, Ctrl+C cancels current dispatch, Ctrl+D exits
- [ ] MW-20.4 Lipgloss theme: dark background, colored kitchen badges
- [ ] MW-20.5 `--tui` flag activates, absence means headless

### Course MW-21: Input Component [1 SP]

- [ ] MW-21.1 Text input with prompt cursor
- [ ] MW-21.2 Tab completion: kitchen names, recipe names, `--` flags
- [ ] MW-21.3 History: up/down arrow recalls previous prompts (session-only)
- [ ] MW-21.4 Kitchen prefix: `@claude explain...` forces kitchen

### Course MW-22: Output Viewport [2 SP]

- [ ] MW-22.1 Scrollable viewport for streaming kitchen output
- [ ] MW-22.2 Kitchen badge header: `[claude] thinking...` with color
- [ ] MW-22.3 Syntax highlighting for code blocks (tree-sitter or regex-based)
- [ ] MW-22.4 Auto-scroll to bottom during streaming, scroll-lock on manual scroll

### Course MW-23: Ledger Panel [1 SP]

- [ ] MW-23.1 Right-side panel showing last 10 dispatches
- [ ] MW-23.2 Columns: time, kitchen (colored), duration, outcome (pass/fail icon)
- [ ] MW-23.3 Live update after each dispatch

### Course MW-24: Kitchen Selector [2 SP]

- [ ] MW-24.1 `Ctrl+K` opens kitchen picker overlay
- [ ] MW-24.2 Show all kitchens with stations, cost tier, availability (is binary installed and authenticated?)
- [ ] MW-24.3 Select -> pins kitchen for next dispatch
- [ ] MW-24.4 Routing explanation panel: shows what sommelier would choose and why

### Course MW-25A: Process Map [2 SP]

- [ ] MW-25A.1 `ProcessMap` Bubble Tea component — top-right corner, always visible
- [ ] MW-25A.2 Single dispatch view: task summary, sommelier reasoning (keywords, pantry signals, risk level), active kitchen, status (streaming / done / failed), elapsed time
- [ ] MW-25A.3 Recipe view: course list with status symbols (done, active with pulse, pending, failed, skipped), kitchen name per course, elapsed per course, total progress (e.g. "Course 2/5")
- [ ] MW-25A.4 Update on 100ms tick for elapsed time, immediate update on state transitions
- [ ] MW-25A.5 Headless equivalent: `--verbose` prints state transitions to stderr as `[sommelier]` and `[dispatch]` lines
- [ ] MW-25A.6 Vitest-style test: mock dispatch state changes, verify render output

- [ ] 🍋 **Palate Cleanser 4** — Interactive mode verified: type task, routes to kitchen, streams output, process map shows live state, ledger updates, Ctrl+C cancels cleanly

---

## Service 5 — Full Pantry + Carte Integration (12 SP)

### Course MW-25: DepGraph [2 SP]

- [ ] MW-25.1 Create `depgraph.db` schema: deps(repo, package, version, latest_version, cve_ids TEXT, consumers TEXT)
- [ ] MW-25.2 Parser for: go.mod, package.json, Cargo.toml, pdm.lock
- [ ] MW-25.3 CVE lookup: query GitHub Advisory Database API (or osv.dev)
- [ ] MW-25.4 `milliways pantry depgraph sync --repo .`
- [ ] MW-25.5 `HasCVE(package) -> (cve_id, severity)` query
- [ ] MW-25.6 Unit tests

### Course MW-26: TopologyGraph [2 SP]

- [ ] MW-26.1 Import from simulator-topology-visualization SQLite (topology_nodes, topology_edges)
- [ ] MW-26.2 `ServiceFanout(service) -> int` — count downstream dependents
- [ ] MW-26.3 `BlastRadius(service) -> []string` — transitive dependents
- [ ] MW-26.4 Feed into sommelier: high fanout -> escalate to claude
- [ ] MW-26.5 Unit tests

### Course MW-27: Carte.md Parser [2 SP]

- [ ] MW-27.1 Parse carte.md markdown table: task -> kitchen -> station -> context injection
- [ ] MW-27.2 `CarteRoute(change, task) -> (kitchen, context_sources)` lookup
- [ ] MW-27.3 Resolve context injection: "CodeGraph: store.py symbols" -> call codegraph_context("store.py")
- [ ] MW-27.4 Unit tests with fixture carte.md

### Course MW-28: opsx:apply Integration [1 SP]

- [ ] MW-28.1 `milliways --recipe opsx:apply "change-name"` — read tasks.md, find first unchecked task
- [ ] MW-28.2 Look up task in carte.md -> get kitchen + context
- [ ] MW-28.3 Dispatch to kitchen with injected context
- [ ] MW-28.4 On success: tick task in tasks.md (if --auto-tick flag)

### Course MW-29: Routing Accuracy Report [1 SP]

- [ ] MW-29.1 `milliways report --accuracy` — compare keyword routing vs enriched vs learned
- [ ] MW-29.2 Show: first 50 dispatches success rate vs last 50
- [ ] MW-29.3 Suggest carte.yaml tuning: "consider routing 'refactor' to claude instead of aider (78% vs 62% success)"
- [ ] MW-29.4 Unit tests

### Course MW-30: Hook Chain Implementation [3 SP]

- [ ] MW-30.1 Define HookEvent enum: SessionStart, PreRoute, PostRoute, PreDispatch, PostDispatch, SessionEnd
- [ ] MW-30.2 Hook runner: load hooks from carte.yaml, execute in order per event
- [ ] MW-30.3 Built-in hooks: circuit breaker (PreRoute), pantry injection (PreRoute), ledger write (PostDispatch), quota enforcement (PreDispatch), feedback update (PostDispatch)
- [ ] MW-30.4 User hooks: shell commands configured in carte.yaml per event
- [ ] MW-30.5 Hook failure handling: built-in hooks can abort dispatch, user hooks are best-effort
- [ ] MW-30.6 Unit tests for hook chain execution and failure handling

### Course MW-31: Tiered-CLI Proof Report [1 SP]

- [ ] MW-31.1 `milliways report --tiered` — query mw_routing for per-kitchen-per-task-type scores
- [ ] MW-31.2 Compute composite multi-CLI score and best-single-CLI score
- [ ] MW-31.3 Display lift percentage and per-task-type breakdown
- [ ] MW-31.4 Unit tests

- [ ] 🍋 **Grand Finale** — Full Milliways verified: all pantry graphs populated, carte.md drives opsx:apply routing, routing accuracy measurably improved over keyword-only, `milliways report` shows value delivered per kitchen

---

## Service 6 — Neovim Plugin (5 SP)

### Course MW-32: milliways.nvim Plugin [3 SP]

- [ ] MW-32.1 Create nvim-plugin/lua/milliways/init.lua — plugin setup and command registration
- [ ] MW-32.2 :Milliways command — prompt input, call `milliways --json`, display in floating window
- [ ] MW-32.3 :MilliwaysExplain — call `milliways --explain --json`, display routing decision
- [ ] MW-32.4 :MilliwaysKitchen — pick kitchen via telescope/fzf, then dispatch
- [ ] MW-32.5 :MilliwaysRecipe — pick recipe, dispatch multi-course
- [ ] MW-32.6 :MilliwaysStatus — call `milliways status`, display in floating window
- [ ] MW-32.7 :MilliwaysDetached — call `milliways detached`, display tickets

### Course MW-33: Neovim Context Injection [2 SP]

- [ ] MW-33.1 Visual selection -> pass as --context-lines to milliways
- [ ] MW-33.2 Current file path -> pass as --context-file
- [ ] MW-33.3 LSP symbol at cursor -> pass as --context-symbol
- [ ] MW-33.4 Git diff of current buffer -> pass as --context-diff
- [ ] MW-33.5 Floating window actions: q(close), a(apply diff to buffer), y(yank), r(retry with different kitchen)
- [ ] MW-33.6 Keybindings: <leader>mm, <leader>me, <leader>ms, <leader>mr, <leader>mk

- [ ] 🍋 **Palate Cleanser 6** — Neovim verified: select code -> :Milliways explain -> floating window shows response from correct kitchen, a(apply) patches buffer

---

## Future Courses (not scheduled)

- [ ] MW-F1: `milliways rate last good/bad` — explicit feedback for learned routing
- [ ] MW-F2: Parallel kitchen execution for independent recipe courses
- [ ] MW-F3: `milliways watch` — file watcher that auto-dispatches on save
- [ ] MW-F4: Plugin system for custom kitchens (WASM or Go plugins)
- [ ] MW-F5: `milliways pair` — two kitchens work simultaneously on same task, diff results
- [ ] MW-F6: Neovim integration via RPC (`:Milliways` command)
- [ ] MW-F7: OpenSpec tasting-menu template generator (`milliways init-menu`)
- [ ] MW-F8: A/B dispatch mode (`milliways --compare "task"` routes to two kitchens, compares)
- [ ] MW-F9: OpenHands as kitchen (async-only, Docker limits from quotas)
- [ ] MW-F10: Subdispatch observation (read subdispatch.ndjson from tiered-agent-architecture hooks)
