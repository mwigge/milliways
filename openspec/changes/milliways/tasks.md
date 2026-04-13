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

- [x] MW-6.1 Create `internal/pantry/db.go` — PantryDB struct with single `*sql.DB` connection to milliways.db (WAL mode, busy_timeout=5000)
- [x] MW-6.2 Create `internal/pantry/schema.go` — full schema v1 (mw_schema, mw_ledger, mw_tickets, mw_gitgraph, mw_quality, mw_deps, mw_routing, mw_quotas) with indexes
- [x] MW-6.3 Implement migration runner — check mw_schema version, apply sequentially
- [x] MW-6.4 Implement `LedgerStore` — Insert (returns ID), Stats, Total
- [x] MW-6.5 `TicketStore` — Create (generates mw- ID), Get, List (filter by status), UpdateStatus
- [x] MW-6.6 Implement `RoutingStore` — RecordOutcome, BestKitchen(task_type, file_profile, minDataPoints)
- [x] MW-6.7 Implement `QuotaStore` — Increment, DailyDispatches
- [x] MW-6.8 Typed accessors: `db.Ledger()`, `db.Routing()`, `db.Quotas()`
- [x] MW-6.9 Refactor cmd/milliways/main.go to use PantryDB
- [x] MW-6.10 Delete internal/ledger/store.go, dual.go (absorbed into pantry)
- [x] MW-6.11 Keep internal/ledger/writer.go for ndjson-only audit trail
- [x] MW-6.12 Unit tests: migration idempotency, ledger CRUD+stats, routing record/query, quota increment/query (pantry 85% coverage)

### Course MW-7: MemPalace Integration [2 SP]

- [x] MW-7.1 `MemPalaceClient` wrapping MCP client (internal/pantry/mempalace.go)
- [x] MW-7.2 `Search(query, wing, limit) -> []Drawer` — calls `mempalace_search` via MCPClient.CallTool
- [x] MW-7.3 `KGQuery(subject, predicate) -> []Triple` — calls `mempalace_kg_query` via MCPClient.CallTool
- [x] MW-7.4 MemPalace context available via MCP — sommelier receives signals from assembleSignals()
- [x] MW-7.5 Unit tests: parseToolContent (direct JSON, MCP wrapper, empty), extractText, JSON-RPC marshal/unmarshal

### Course MW-8: CodeGraph Integration [2 SP]

- [x] MW-8.1 `CodeGraphClient` wrapping MCP client (internal/pantry/codegraph.go)
- [x] MW-8.2 `Context(task) -> string` — calls `codegraph_context` via MCPClient.CallTool
- [x] MW-8.3 `Impact(symbol, depth) -> ImpactResult` — calls `codegraph_impact` via MCPClient.CallTool
- [x] MW-8.4 `Search(query, limit)` + `FileComplexity(file)` for routing signals
- [x] MW-8.5 Tests: shared with MCP client tests (parseToolContent, extractText)

### Course MW-9: GitGraph [2 SP]

- [x] MW-9.1 `GitGraphStore` in pantry: Sync (parses git log --numstat), IsHotspot, classifyStability
- [x] MW-9.2 Sync parses git log, aggregates 30d/90d churn, author counts, upserts via transaction
- [x] MW-9.3 classifyStability: stable (<3), active (3-15), volatile (>15)
- [x] MW-9.4 `milliways pantry sync [repo-path]` subcommand for GitGraph sync
- [x] MW-9.5 `IsHotspot(repo, file) -> *FileStats` query
- [x] MW-9.6 Tests: stability classification, IsHotspot not-found, upsert+query, real repo sync

### Course MW-10: QualityGraph [2 SP]

- [x] MW-10.1 QualityStore in PantryDB: Upsert, FileRisk (max complexity, min coverage, sum smells, COALESCE for NULLs)
- [ ] MW-10.2 Populate from CodeGraph AST data (deferred — requires live CodeGraph MCP + tree-sitter parser)
- [x] MW-10.3 ImportCoverage: batch import from coverage-by-file map (transaction-based)
- [x] MW-10.4 `FileRisk(repo, file) -> QualityMetrics` query function
- [x] MW-10.5 Unit tests: upsert, file risk aggregation, idempotent update, import coverage, not-found

### Course MW-11: Enriched Routing (Sommelier Tier 2) [2 SP]

- [x] MW-11.1 `RouteEnriched(prompt, signals)` — three-tier routing with signals
- [x] MW-11.2 Signal aggregation: Signals struct with RiskLevel() scoring across churn, complexity, coverage, authors
- [x] MW-11.3 Risk scoring: LOW → keyword; MEDIUM → keyword; HIGH → override to claude for safety
- [x] MW-11.4 `--explain --verbose` shows pantry signals, risk level, learned kitchen in routing reasoning
- [x] MW-11.5 Unit tests: high/medium/low risk override, nil signals graceful, learned override, unavailable fallthrough

### Course MW-11B: Circuit Breaker [2 SP]

- [x] MW-11B.1 ReadMode() from ~/.claude/mode (default: "private" if missing)
- [x] MW-11B.2 PathAllowed(path, mode) with company/private/neutral path lists
- [x] MW-11B.3 Mode logged on every dispatch via --verbose
- [ ] MW-11B.4 Filter kitchen list by mode (carte.yaml `kitchens.X.modes: [company, private]`) — deferred to Service 3
- [x] MW-11B.5 Pass MILLIWAYS_MODE env var to kitchen subprocess via Task.Env
- [x] MW-11B.6 Unit tests: 8 company paths + 6 private paths tested

### Course MW-11C: Skill Catalog [1 SP]

- [x] MW-11C.1 ScanSkills() scans ~/.claude/skills/ for SKILL.md frontmatter — extracts name + description
- [x] MW-11C.2 Scan ~/.config/opencode/plugins/ for .ts files — extracts plugin names
- [x] MW-11C.3 SkillCatalog with ForKitchen(), HasSkill(query), Total()
- [x] MW-11C.4 Sommelier SkillHint: if HasSkill(prompt) matches, pass hint to RouteEnriched → boost matching kitchen
- [x] MW-11C.5 Unit tests: scanSkillDir, scanPluginDir, HasSkill, ForKitchen, readSkillDescription with fixture dirs

### Course MW-12: Learned Routing (Sommelier Tier 3) [1 SP]

- [x] MW-12.1 RoutingStore.BestKitchen queries mw_routing for highest success rate per task_type
- [x] MW-12.2 Minimum data points parameter (default 5) before learned routing activates
- [x] MW-12.3 `--explain --verbose` shows learned preference in routing reasoning
- [x] MW-12.4 Unit tests: BestKitchen with sufficient/insufficient data, RecordOutcome (pantry/db_test.go)

### Course MW-12B: Tiered-CLI Feedback [2 SP]

- [x] MW-12B.1 ClassifyTaskType() in sommelier/classify.go (think/code/refactor/search/review/test/general)
- [x] MW-12B.2 PostDispatch in main.go calls pdb.Routing().RecordOutcome() with classified task_type
- [x] MW-12B.3 `milliways report --tiered` queries mw_ledger per task_type × kitchen, computes composite + lift
- [x] MW-12B.4 Unit tests: ClassifyTaskType (15 prompts) in classify_test.go

- [ ] 🍋 **Palate Cleanser 2** — Intelligent routing verified: pantry signals influence routing, --explain shows reasoning, learned routing activates after sufficient data

---

## Service 3 — All Kitchens + Recipes (18 SP)

### Course MW-13: OpenCode Adapter [2 SP]

- [x] MW-13.1 OpenCode adapter works via GenericKitchen (cmd=opencode, args=[run])
- [x] MW-13.2 --dir support via Task.Dir in GenericKitchen.Exec
- [x] MW-13.3 Streaming stdout + exit code via GenericKitchen
- [ ] MW-13.4 Parse `opencode run -o json` for structured output when available
- [x] MW-13.5 Covered by GenericKitchen_Exec tests

### Course MW-14: Gemini Adapter [1 SP]

- [x] MW-14.1 Gemini adapter works via GenericKitchen (cmd=gemini)
- [x] MW-14.2 Streaming stdout + exit code via GenericKitchen
- [x] MW-14.3 Covered by GenericKitchen_Exec tests

### Course MW-15: Aider Adapter [2 SP]

- [x] MW-15.1 Aider adapter works via GenericKitchen (cmd=aider, args=[--message, --yes-always])
- [ ] MW-15.2 Pass `--file` for targeted files when context provides them
- [ ] MW-15.3 Detect git commits made by aider (parse stdout for commit hash)
- [x] MW-15.4 Covered by GenericKitchen_Exec tests

### Course MW-16: Goose Adapter [1 SP]

- [x] MW-16.1 Goose adapter works via GenericKitchen (cmd=goose)
- [x] MW-16.2 Streaming stdout + exit code via GenericKitchen
- [x] MW-16.3 Covered by GenericKitchen_Exec tests

### Course MW-17: Cline Adapter [1 SP]

- [x] MW-17.1 Cline adapter works via GenericKitchen (cmd=cline, args=[-y, --json])
- [ ] MW-17.2 Parse JSON output for structured result
- [x] MW-17.3 Covered by GenericKitchen_Exec tests

### Course MW-18: Recipe Engine [2 SP]

- [x] MW-18.1 Recipe steps parsed from carte.yaml in dispatchRecipe()
- [x] MW-18.2 recipe.Engine.Execute() runs courses sequentially
- [x] MW-18.3 Previous course output injected into next prompt
- [x] MW-18.4 Stops on first failure (engine.go returns error)
- [x] MW-18.5 Each course logged to mw_ledger with dispatch_mode=recipe
- [x] MW-18.6 Tests: single/multi/failure/unavailable/empty/context

### Course MW-18B: Async Dispatch [2 SP]

- [x] MW-18B.1 milliways --async spawns background goroutine, returns ticket
- [x] MW-18B.2 Ticket written to mw_tickets (running, output_path)
- [x] MW-18B.3 Background updates ticket + writes ledger on completion
- [x] MW-18B.4 milliways ticket {id} shows status
- [x] MW-18B.5 milliways tickets lists all with status
- [x] MW-18B.6 Tests: async dispatch + list + completion

### Course MW-18C: Detached Dispatch [2 SP]

- [x] MW-18C.1 milliways --detach creates ticket + marker
- [ ] MW-18C.2 Redirect stdout/stderr to ~/.config/milliways/detached/{pid}.log
- [x] MW-18C.3 Ticket with mode=detached
- [ ] MW-18C.4 `milliways detached` — list detached processes with status (check if pid still running)
- [ ] MW-18C.5 Completion detection: poll pid existence, update ticket on exit
- [x] MW-18C.6 Tests: detached dispatch creates ticket

### Course MW-19: Context Handoff [2 SP]

- [x] MW-19.1 Context files in /tmp/milliways-{id}-{n}.json
- [x] MW-19.2 Previous output injected as prompt prefix
- [x] MW-19.3 --keep-context flag preserves temp files
- [ ] MW-19.4 Context size limit: if previous output > 10KB, summarize via utility kitchen (haiku/qwen)
- [x] MW-19.5 Tested in recipe engine tests (context passing)

### Course MW-19B: Resource Quotas [2 SP]

- [x] MW-19B.1 QuotaConfig + GlobalQuotaConfig structs parsed from carte.yaml
- [x] MW-19B.2 QuotaCheck.Check counts running tickets per kitchen
- [x] MW-19B.3 QuotaCheck.Check queries DailyDispatches from mw_quotas
- [x] MW-19B.4 QuotaCheck.Check counts total running tickets globally
- [x] MW-19B.5 systemMemoryPercent() via sysctl + vm_stat on macOS
- [ ] MW-19B.6 Queue dispatch if at limit (wait for slot or timeout)
- [x] MW-19B.7 pdb.Quotas().Increment() called in PostDispatch
- [x] MW-19B.8 Tests: allowed by default, daily limit reached/not-reached, no quota configured, memory function

### Course MW-19C: Recipe Failure Recovery [1 SP]

- [x] MW-19C.1 ParseStrategy: stop/retry-course/skip-course/restart-from
- [x] MW-19C.2 HandleFailure applies strategy, returns continue/stop + retry result
- [x] MW-19C.3 RetryCourse re-executes with partial output as context
- [x] MW-19C.4 savePartial writes failed course output to /tmp/milliways-partial/
- [x] MW-19C.5 Tests: skip/stop/retry-success/retry-unavailable, ParseStrategy

- [ ] 🍋 **Palate Cleanser 3** — Full menu verified: all 6 kitchens dispatch, recipe runs 5-course meal, context flows between courses

---

## Service 4 — TUI (8 SP)

### Course MW-20: Bubble Tea App Shell [2 SP]

- [x] MW-20.1 Bubble Tea Model with Init/Update/View, message types for lines/dispatch/tick
- [x] MW-20.2 Layout: input (bottom), output viewport (center), ledger (right), process map (top-right)
- [x] MW-20.3 Enter submits, Ctrl+C cancels/quits, Ctrl+D exits, up/down history
- [x] MW-20.4 Lipgloss theme: dark bg, colored kitchen badges, status icons
- [x] MW-20.5 --tui flag activates alt-screen TUI, absence means headless

### Course MW-21: Input Component [1 SP]

- [x] MW-21.1 textinput.Model with prompt cursor and placeholder
- [ ] MW-21.2 Tab completion: kitchen names, recipe names, `--` flags
- [x] MW-21.3 History: up/down recalls previous prompts (session-only)
- [x] MW-21.4 @kitchen prefix: @claude forces kitchen

### Course MW-22: Output Viewport [2 SP]

- [x] MW-22.1 viewport.Model with scrollable output
- [x] MW-22.2 KitchenBadge() with per-kitchen colors
- [ ] MW-22.3 Syntax highlighting for code blocks (tree-sitter or regex-based)
- [x] MW-22.4 GotoBottom() auto-scroll during streaming

### Course MW-23: Ledger Panel [1 SP]

- [x] MW-23.1 Right-side panel with last 8 dispatches
- [x] MW-23.2 Columns: time, kitchen badge, duration, status icon
- [x] MW-23.3 Live update after each dispatchDoneMsg

### Course MW-24: Kitchen Selector [2 SP]

- [ ] MW-24.1 `Ctrl+K` opens kitchen picker overlay
- [ ] MW-24.2 Show all kitchens with stations, cost tier, availability (is binary installed and authenticated?)
- [ ] MW-24.3 Select -> pins kitchen for next dispatch
- [ ] MW-24.4 Routing explanation panel: shows what sommelier would choose and why

### Course MW-25A: Process Map [2 SP]

- [x] MW-25A.1 processState rendered in top-right panel, always visible
- [x] MW-25A.2 Shows kitchen badge, status icon, elapsed time, risk level
- [ ] MW-25A.3 Recipe view: course list with status symbols (done, active with pulse, pending, failed, skipped), kitchen name per course, elapsed per course, total progress (e.g. "Course 2/5")
- [x] MW-25A.4 tickMsg at 100ms interval updates elapsed
- [x] MW-25A.5 --verbose headless equivalent already implemented
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

- [x] MW-27.1 ParseCarte reads markdown table from carte.md (Task|Kitchen|Station|Context)
- [x] MW-27.2 Carte.Route(taskID) returns kitchen + context sources
- [ ] MW-27.3 Resolve context injection: "CodeGraph: store.py symbols" -> call codegraph_context("store.py")
- [x] MW-27.4 Tests: parse 5-row table, Route lookup, empty file, missing file

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

- [x] MW-30.1 HookEvent enum: SessionStart, PreRoute, PostRoute, PreDispatch, PostDispatch, SessionEnd
- [x] MW-30.2 HookRunner loads hooks from config, executes per event
- [x] MW-30.3 Built-in hooks wired in dispatch flow (circuit breaker, pantry, ledger, quotas, feedback)
- [x] MW-30.4 User hooks: shell commands with MILLIWAYS_* env vars (kitchen, mode, task_type, risk, exit_code)
- [x] MW-30.5 Blocking hooks abort dispatch, non-blocking log to stderr and continue
- [x] MW-30.6 Tests: grouping, blocking success/failure, non-blocking, no-hooks, env vars, ParseHookEvent

### Course MW-31: Tiered-CLI Proof Report [1 SP]

- [x] MW-31.1 Already done — milliways report --tiered in main.go
- [x] MW-31.2 Already done — composite + single-CLI scores computed
- [x] MW-31.3 Already done — lift percentage displayed
- [x] MW-31.4 Tests covered by report integration

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
