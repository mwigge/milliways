# Prep List — Milliways

## Service 1 — Core + First Kitchen (10 SP)

### Course MW-1: CLI Skeleton [2 SP]

- [x] MW-1.1 `go mod init github.com/mwigge/milliways` with Go 1.22+
- [x] MW-1.2 Add Cobra dependency, create `cmd/milliways/main.go` with root command
- [x] MW-1.3 Flags: `--kitchen` (force kitchen), `--json` (JSON output), `--recipe` (multi-course), `--tui` (interactive), `--explain` (show routing without executing), `--keep-context` (preserve temp files)
- [x] MW-1.4 `--version` flag, `--help` with restaurant-themed descriptions
- [x] MW-1.5 Config loader: read `~/.config/milliways/carte.yaml`, merge with defaults
- [x] MW-1.6 Unit tests for config loading and flag parsing

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
- [x] MW-5B.3 Install helpers table: kitchen → install command (brew/npm/pip)
- [x] MW-5B.4 `milliways setup <kitchen>` — run install command, wait, re-check status, guide auth if needed
- [ ] MW-5B.5 First-run welcome screen: list all kitchens with status, offer [i]nstall / [a]uth / [s]kip
- [x] MW-5B.6 `milliways status` — display kitchen availability, action column, ledger stats
- [x] MW-5B.7 Graceful degradation: sommelier routes only to `Ready` kitchens, skips others with fallback
- [x] MW-5B.8 Single-kitchen mode: works with just one kitchen (reduced routing, no errors)
- [x] MW-5B.9 carte.yaml `enabled: true/false` per kitchen — user opt-out respected
- [x] MW-5B.10 Unit tests: status detection (Diagnose, ReadyCounts), setup ready/disabled paths

### Course MW-INT1: Integration Test [2 SP]

- [ ] MW-INT1.1 End-to-end test: `milliways "explain hello world"` → stdout contains response, ledger has entry
- [ ] MW-INT1.2 `milliways --kitchen claude "hello"` → forces claude
- [ ] MW-INT1.3 `milliways --json "hello"` → valid JSON output
- [ ] MW-INT1.4 `milliways --explain "refactor store.py"` → prints routing reasoning
- [ ] MW-INT1.5 Verify ledger.ndjson parseable by `jq`

- [ ] 🍋 **Palate Cleanser 1** — CLI MVP verified: route, stream, log, explain all working

---

## Service 2 — Pantry + Sommelier (13 SP)

### Course MW-6: MCP Client [2 SP]

- [ ] MW-6.1 Generic MCP client: spawn subprocess, communicate via stdin/stdout JSON-RPC
- [ ] MW-6.2 `Call(method string, params any) (json.RawMessage, error)` with timeout
- [ ] MW-6.3 Connection pooling: keep MCP servers alive between calls within a session
- [ ] MW-6.4 Graceful shutdown: terminate child processes on Milliways exit
- [ ] MW-6.5 Unit tests with mock MCP server (echo responses)

### Course MW-7: MemPalace Integration [2 SP]

- [ ] MW-7.1 `MemPalaceClient` wrapping MCP client
- [ ] MW-7.2 `Search(query, wing, limit) → []Drawer` — call `mempalace_search`
- [ ] MW-7.3 `KGQuery(subject, predicate) → []Triple` — call `mempalace_kg_query`
- [ ] MW-7.4 Inject search results into sommelier context: "MemPalace says: {decision summary}"
- [ ] MW-7.5 Unit tests with mocked MCP responses

### Course MW-8: CodeGraph Integration [2 SP]

- [ ] MW-8.1 `CodeGraphClient` wrapping MCP client
- [ ] MW-8.2 `Context(task) → string` — call `codegraph_context`
- [ ] MW-8.3 `Impact(symbol, depth) → ImpactResult` — call `codegraph_impact`
- [ ] MW-8.4 Extract file complexity + caller count as routing signals
- [ ] MW-8.5 Unit tests with mocked MCP responses

### Course MW-9: GitGraph [2 SP]

- [ ] MW-9.1 Create `gitgraph.db` schema: files(path, repo, churn_30d, churn_90d, authors_30d, last_author, last_changed, stability TEXT)
- [ ] MW-9.2 `gitgraph sync` command: parse `git log --numstat` for target repo, upsert file stats
- [ ] MW-9.3 Materialize stability: STABLE (churn < 3/90d), ACTIVE (3-15), VOLATILE (>15)
- [ ] MW-9.4 Post-commit hook script: `milliways pantry gitgraph sync --repo .`
- [ ] MW-9.5 `IsHotspot(file) → (churn, stability, lastAuthor)` query function
- [ ] MW-9.6 Unit tests with fixture git history

### Course MW-10: QualityGraph [2 SP]

- [ ] MW-10.1 Create `qualitygraph.db` schema: metrics(file, function, cyclomatic_complexity, cognitive_complexity, coverage_pct, smell_count, last_updated)
- [ ] MW-10.2 Populate from CodeGraph AST data (tree-sitter already parses function bodies)
- [ ] MW-10.3 Populate coverage from pytest/vitest coverage JSON output (optional, when available)
- [ ] MW-10.4 `FileRisk(file) → (complexity, coverage, smells)` query function
- [ ] MW-10.5 Unit tests

### Course MW-11: Enriched Routing (Sommelier Tier 2) [2 SP]

- [ ] MW-11.1 `EnrichedRoute(prompt, file) → (kitchen, reason, signals)` — consults pantry
- [ ] MW-11.2 Signal aggregation: CodeGraph complexity + GitGraph churn + QualityGraph coverage
- [ ] MW-11.3 Risk scoring: LOW (all green) → keyword routing; MEDIUM (one amber) → careful kitchen; HIGH (multiple red) → claude
- [ ] MW-11.4 `--explain` shows all pantry signals in routing reasoning
- [ ] MW-11.5 Unit tests with mock pantry responses

### Course MW-12: Learned Routing (Sommelier Tier 3) [1 SP]

- [ ] MW-12.1 Query ledger.db: for this task_type + file_complexity_bucket + file_churn_bucket, which kitchen had highest success rate?
- [ ] MW-12.2 Minimum 5 data points before learned routing overrides keyword
- [ ] MW-12.3 `--explain` shows learned preference when applicable
- [ ] MW-12.4 Unit tests with fixture ledger data

- [ ] 🍋 **Palate Cleanser 2** — Intelligent routing verified: pantry signals influence routing, --explain shows reasoning, learned routing activates after sufficient data

---

## Service 3 — All Kitchens + Recipes (11 SP)

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
- [ ] MW-18.2 `ExecuteRecipe(name, prompt) → []Result` — run courses sequentially
- [ ] MW-18.3 Each course receives original prompt + accumulated context
- [ ] MW-18.4 Stop on first failure (exit_code != 0) unless `--continue-on-error`
- [ ] MW-18.5 Log each course separately in ledger
- [ ] MW-18.6 Unit tests with mocked kitchen executions

### Course MW-19: Context Handoff [2 SP]

- [ ] MW-19.1 After each course: write `/tmp/milliways-{recipe-id}-{n}.json` with output, files_changed, exit_code
- [ ] MW-19.2 Before next course: inject previous course output into prompt (configurable: full text, summary, or files-only)
- [ ] MW-19.3 `--keep-context` flag: don't delete temp files after recipe completes
- [ ] MW-19.4 Context size limit: if previous output > 10KB, summarize via utility kitchen (haiku/qwen)
- [ ] MW-19.5 Unit tests for context serialization and injection

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
- [ ] MW-24.3 Select → pins kitchen for next dispatch
- [ ] MW-24.4 Routing explanation panel: shows what sommelier would choose and why

### Course MW-25A: Process Map [2 SP]

- [ ] MW-25A.1 `ProcessMap` Bubble Tea component — top-right corner, always visible
- [ ] MW-25A.2 Single dispatch view: task summary, sommelier reasoning (keywords, pantry signals, risk level), active kitchen, status (● streaming / ✓ done / ✗ failed), elapsed time
- [ ] MW-25A.3 Recipe view: course list with status symbols (✓ done, ● active with pulse, ○ pending, ✗ failed, ⊘ skipped), kitchen name per course, elapsed per course, total progress (e.g. "Course 2/5")
- [ ] MW-25A.4 Update on 100ms tick for elapsed time, immediate update on state transitions
- [ ] MW-25A.5 Headless equivalent: `--verbose` prints state transitions to stderr as `[sommelier]` and `[dispatch]` lines
- [ ] MW-25A.6 Vitest-style test: mock dispatch state changes, verify render output

- [ ] 🍋 **Palate Cleanser 4** — Interactive mode verified: type task, routes to kitchen, streams output, process map shows live state, ledger updates, Ctrl+C cancels cleanly

---

## Service 5 — Full Pantry + Carte Integration (8 SP)

### Course MW-25: DepGraph [2 SP]

- [ ] MW-25.1 Create `depgraph.db` schema: deps(repo, package, version, latest_version, cve_ids TEXT, consumers TEXT)
- [ ] MW-25.2 Parser for: go.mod, package.json, Cargo.toml, pdm.lock
- [ ] MW-25.3 CVE lookup: query GitHub Advisory Database API (or osv.dev)
- [ ] MW-25.4 `milliways pantry depgraph sync --repo .`
- [ ] MW-25.5 `HasCVE(package) → (cve_id, severity)` query
- [ ] MW-25.6 Unit tests

### Course MW-26: TopologyGraph [2 SP]

- [ ] MW-26.1 Import from simulator-topology-visualization SQLite (topology_nodes, topology_edges)
- [ ] MW-26.2 `ServiceFanout(service) → int` — count downstream dependents
- [ ] MW-26.3 `BlastRadius(service) → []string` — transitive dependents
- [ ] MW-26.4 Feed into sommelier: high fanout → escalate to claude
- [ ] MW-26.5 Unit tests

### Course MW-27: Carte.md Parser [2 SP]

- [ ] MW-27.1 Parse carte.md markdown table: task → kitchen → station → context injection
- [ ] MW-27.2 `CarteRoute(change, task) → (kitchen, context_sources)` lookup
- [ ] MW-27.3 Resolve context injection: "CodeGraph: store.py symbols" → call codegraph_context("store.py")
- [ ] MW-27.4 Unit tests with fixture carte.md

### Course MW-28: opsx:apply Integration [1 SP]

- [ ] MW-28.1 `milliways --recipe opsx:apply "change-name"` — read tasks.md, find first unchecked task
- [ ] MW-28.2 Look up task in carte.md → get kitchen + context
- [ ] MW-28.3 Dispatch to kitchen with injected context
- [ ] MW-28.4 On success: tick task in tasks.md (if --auto-tick flag)

### Course MW-29: Routing Accuracy Report [1 SP]

- [ ] MW-29.1 `milliways report --accuracy` — compare keyword routing vs enriched vs learned
- [ ] MW-29.2 Show: first 50 dispatches success rate vs last 50
- [ ] MW-29.3 Suggest carte.yaml tuning: "consider routing 'refactor' to claude instead of aider (78% vs 62% success)"
- [ ] MW-29.4 Unit tests

- [ ] 🍋 **Grand Finale** — Full Milliways verified: all pantry graphs populated, carte.md drives opsx:apply routing, routing accuracy measurably improved over keyword-only, `milliways report` shows value delivered per kitchen

---

## Future Courses (not scheduled)

- [ ] MW-F1: `milliways rate last good/bad` — explicit feedback for learned routing
- [ ] MW-F2: Parallel kitchen execution for independent recipe courses
- [ ] MW-F3: `milliways watch` — file watcher that auto-dispatches on save
- [ ] MW-F4: Plugin system for custom kitchens (WASM or Go plugins)
- [ ] MW-F5: `milliways pair` — two kitchens work simultaneously on same task, diff results
- [ ] MW-F6: Neovim integration via RPC (`:Milliways` command)
- [ ] MW-F7: OpenSpec tasting-menu template generator (`milliways init-menu`)
