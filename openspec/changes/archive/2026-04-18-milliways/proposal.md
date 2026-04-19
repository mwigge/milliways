# Milliways — The Restaurant at the End of the Universe

> "The Restaurant at the End of the Universe is one of the most extraordinary
> ventures in the entire history of catering."

## Why

Every AI coding CLI excels at something different. Claude Code thinks deeply and reviews adversarially. OpenCode runs local models at zero cost. Gemini CLI has Google Search grounding and a free tier. Aider does git-first multi-file editing. Goose orchestrates 70+ MCP tools. Cline runs parallel fleets.

Today a developer must manually choose which CLI to open, copy context between them, and remember which tool worked best for which task. Knowledge is siloed — Claude Code's MemPalace memories don't inform Gemini's research. CodeGraph's blast radius analysis doesn't reach Aider's refactoring. Each tool is a restaurant that only serves one cuisine.

Milliways doesn't cook. It seats you, takes your order, and brings the best dish from whichever kitchen excels at making it. One CLI to rule them all.

## What

A Go CLI (Bubble Tea + Lipgloss) that:

1. **Routes tasks to the best kitchen** based on task type, file risk, and historical success
2. **Shares a single pantry** of knowledge graphs across all kitchens
3. **Sequences multi-course meals** (recipes) across kitchens with context handoff
4. **Logs every dish served** to a routing ledger for continuous improvement
5. **Runs headless** (scriptable, pipeable) or as an **interactive TUI**

## The Menu Vocabulary

| Milliways Term | Meaning | OpenSpec Equivalent |
|----------------|---------|-------------------|
| Tasting menu | A coordinated multi-course delivery plan | Umbrella change |
| Course | One deliverable, prepared by a kitchen | Constituent change |
| Service | A wave of courses served together | BLOCK |
| Palate cleanser | Hard stop, verify, advance | GATE |
| Kitchen | A CLI tool with its own models and strengths | Agent / tier |
| Station | A capability within a kitchen | Skill / agent role |
| Carte | Which kitchen cooks each task | menu.md (new artifact) |
| Pantry | Shared knowledge all kitchens consume | MCP servers + knowledge graphs |
| Recipe | A fixed multi-course workflow | Pipeline / workflow |
| Maitre d' | Orchestrates timing, doesn't cook | Milliways CLI itself |
| Sommelier | Picks the right kitchen for each task | Router intelligence |
| Ledger | What was served, cost, duration, outcome | Routing feedback DB |
| Mise en place | Context prepared before cooking | Knowledge injection per dispatch |
| Prep list | Granular checkboxes | tasks.md |
| Chef's concept | Why this meal exists | proposal.md |
| Recipe book | How to make it, decisions | design.md |
| Service plan | Orchestration rules, sequence, gates | delivery.md |
| Dispatch | Sending a course to its kitchen for execution | CLI invocation |
| Ticket | Tracking token for an async dispatch | Job ID |
| Hook chain | Events fired at routing boundaries | Lifecycle callbacks |
| Circuit breaker | Mode-aware filter that blocks forbidden kitchens/paths | Policy gate |

## Kitchens (initial menu)

| Kitchen | CLI | Stations | Cost Tier | Strength |
|---------|-----|----------|-----------|----------|
| claude | `claude -p` | think, plan, review, explore, sign-off | cloud ($3-75/M) | Deep reasoning, 1M context, adversarial review |
| opencode | `opencode run` | code, test, refactor, lint, commit | local ($0) | devstral:24b, scoped --dir, plugins |
| gemini | `gemini` | search, compare, docs, research | free (1k/day) | Google Search grounding, 1M context |
| aider | `aider --message` | multi-file, git-commit | cloud/local | Git-first auto-commits, multi-file edits |
| goose | `goose` | tools, database, api, mcp | local/cloud | 70+ MCP extensions, provider-agnostic |
| cline | `cline -y --json` | fleet, parallel | cloud | Parallel subagent execution, JSON output |
| openhands | `delegate_to_openhands.sh` | sandbox, long-running, risky-exec | local ($0) | Docker sandbox, async-only, isolated execution |

## Dispatch Modes

Not every order is the same. Sometimes you wait at the table for your dish. Sometimes you place an order and check back later. Sometimes you send a dish to a friend's table and forget about it. Sometimes you order a full tasting menu.

| Mode | Flag | Behaviour | Use Case |
|------|------|-----------|----------|
| **Sync** | (default) | Maitre d' waits for the kitchen to finish. Stdout streams to terminal. Exit code returned. | Quick tasks: explain, lint, review |
| **Async** | `--async` | Maitre d' issues a ticket. Kitchen runs in background. `milliways status <ticket>` to check. | Long-running: refactor, test suite, security audit |
| **Detached** | `--detach` | Kitchen runs in a separate process group. Survives Milliways exit. Result written to ledger when done. | Fire-and-forget: OpenHands sandbox, overnight batch |
| **Recipe** | `--recipe <name>` | Multi-course sequential execution. Each course feeds the next. Palate cleansers between services. | Coordinated workflows: implement-feature, fix-bug |

**Why four modes?** Sync is the natural default -- type a question, get an answer. But some kitchens are slow (OpenHands in Docker, large refactors). Forcing sync would block the terminal. Async gives you a ticket to check later. Detached goes further -- the kitchen keeps cooking even if you close Milliways, because some jobs (overnight code generation, long security scans) should not die with the terminal session. Recipe mode exists because real development workflows are multi-course meals, not single dishes.

```
milliways "explain this function"              # sync (default)
milliways --async "refactor auth module"       # returns ticket MW-0042
milliways status MW-0042                       # check ticket
milliways --detach "run full security audit"   # survives exit
milliways --recipe implement-feature "add JWT" # multi-course
```

## Hook Chain

The maitre d' fires events at every stage of service. Kitchens have their own internal hooks (Claude Code has `.claude/settings.json` hooks, OpenCode has plugins), but Milliways orchestrates at the routing layer with its own hook chain.

| Hook Event | When | Purpose |
|------------|------|---------|
| `SessionStart` | Milliways process starts | Load carte, validate kitchens, open ledger |
| `PreRoute` | Before sommelier picks a kitchen | Circuit breaker, quota check, mode filter |
| `PostRoute` | After sommelier decision, before dispatch | Pantry injection (mise en place), context preparation |
| `PreDispatch` | Immediately before exec to kitchen | Final quota enforcement, spawn tracking |
| `PostDispatch` | Kitchen returns (or ticket resolves) | Ledger write, quality capture, failure recovery |
| `SessionEnd` | Milliways process exits | Flush ledger, release locks, summarise session |

Hooks are Go functions registered at startup. The carte can declare additional hooks per-kitchen. Each hook receives a context struct with the task, routing decision, kitchen state, and ledger handle.

**What hooks handle:**
- **Circuit breaker** -- PreRoute blocks forbidden kitchens based on `~/.claude/mode`
- **Pantry injection** -- PostRoute queries CodeGraph, GitGraph, MemPalace to build mise en place
- **Quota enforcement** -- PreRoute and PreDispatch check per-kitchen and global limits
- **Failure recovery** -- PostDispatch detects failures and optionally re-routes to a fallback kitchen
- **Feedback learning** -- PostDispatch writes outcome to ledger, updates routing weights
- **Ledger integrity** -- SessionEnd ensures all pending writes are flushed

## Circuit Breaker

Reads `~/.claude/mode` which contains either `company` or `private`.

In **company mode**, certain kitchens may be restricted. The company might only approve Claude Code and OpenCode. Gemini, Goose, and Cline might be blocked. Writable paths might be restricted to approved repositories only. The circuit breaker is a hard stop -- it runs as a PreRoute hook and rejects the dispatch before the sommelier even considers the kitchen. No retry, no fallback to the blocked kitchen, no "try anyway".

In **private mode**, all installed kitchens are available. No path restrictions beyond what the kitchen itself enforces.

```yaml
# ~/.config/milliways/carte.yaml
circuit_breaker:
  company:
    allowed_kitchens: [claude, opencode]
    writable_paths: ["/approved/repos/*"]
  private:
    allowed_kitchens: all
    writable_paths: all
```

The circuit breaker is not a suggestion. It is a gate. If a recipe includes a course routed to a blocked kitchen, the entire recipe fails at planning time, not mid-execution.

## Resource Quotas

Kitchens are not equal in resource consumption. Claude Code uses cloud tokens (money). OpenCode uses local GPU (memory). OpenHands spawns Docker containers (CPU, memory, disk). Without quotas, a runaway recipe could exhaust resources.

### Per-Kitchen Quotas

```yaml
kitchens:
  openhands:
    max_concurrent: 1          # only one sandbox at a time
    max_duration: 1800         # 30 minutes max per dispatch
    max_memory_mb: 4096        # Docker memory limit
    daily_dispatches: 20       # no more than 20 per day
    cooldown_sec: 30           # wait between dispatches
  claude:
    max_concurrent: 3
    max_duration: 600
    daily_dispatches: 100
  opencode:
    max_concurrent: 2
    max_duration: 300
```

### Global Quotas

```yaml
global:
  max_total_concurrent: 4      # across all kitchens
  pause_if_memory_above: 90    # % system memory, pause new dispatches
```

Quotas are enforced by PreRoute (daily limit, cooldown) and PreDispatch (concurrent count, memory check). When a quota is hit, the dispatch is rejected with a clear message. For recipes, the course waits until the quota clears (up to a configurable timeout) rather than failing immediately.

This is critical for OpenHands-as-kitchen. A Docker container that runs for hours consuming 8 GB of RAM with no limits would make the developer's machine unusable. Quotas make delegation safe.

## Tiered-CLI Feedback Loop

Milliways exists because no single kitchen is best at everything. But that claim needs proof.

The routing ledger captures every dispatch: task_type, kitchen, duration, exit_code, quality_score (if measurable), cost_estimate. Over time, this data proves (or disproves) the multi-CLI hypothesis.

**The thesis**: No single CLI scores above 70% success across all task types. Milliways multi-CLI routing scores 92%+.

```
milliways report --tiered
```

Output:

```
Task Type       | Best Kitchen | Solo Score | Milliways Score | Lift
----------------|-------------|------------|-----------------|------
explain         | claude      | 94%        | 94%             | +0%
refactor        | aider       | 82%        | 88%             | +6%
research        | gemini      | 91%        | 91%             | +0%
implement       | opencode    | 71%        | 89%             | +18%
review          | claude      | 88%        | 93%             | +5%
security-audit  | goose       | 65%        | 85%             | +20%
sandbox-exec    | openhands   | 73%        | 82%             | +9%
----------------|-------------|------------|-----------------|------
Weighted Average|             | 68%        | 92%             | +24%
```

The lift comes from routing: a task that would fail in one kitchen succeeds in another. The ledger makes this measurable. If a kitchen consistently underperforms on a task type, the sommelier learns to stop sending it there.

`milliways report --tiered` is not vanity metrics. It is the existence proof for Milliways itself. If the lift is zero, Milliways has no reason to exist.

## Neovim Plugin

`milliways.nvim` -- a thin Lua plugin that calls the `milliways` binary. No persistent process, no daemon, no socket. Under 1 MB memory footprint.

### Commands

| Command | Action |
|---------|--------|
| `:Milliways <prompt>` | Route prompt through Milliways, show result in floating window |
| `:MilliwaysExplain` | Explain visual selection or current function via best kitchen |
| `:MilliwaysKitchen <name> <prompt>` | Force a specific kitchen |
| `:MilliwaysRecipe <name> <prompt>` | Run a multi-course recipe |
| `:MilliwaysStatus` | Show active tickets and recent ledger entries |

### Context Injection

The plugin injects editor state as mise en place before dispatching:

- **Visual selection** -- selected text becomes the primary context
- **Current file** -- full file path and content
- **LSP diagnostics** -- errors and warnings from the language server
- **Git diff** -- unstaged changes in the current buffer
- **Cursor position** -- line and column for precise context

### Output

Results appear in a floating window with action keybindings:

| Key | Action |
|-----|--------|
| `<CR>` | Apply suggestion to buffer |
| `y` | Yank result to clipboard |
| `r` | Retry with different kitchen |
| `q` | Close window |

The plugin is a convenience layer. Everything it does is equivalent to running `milliways` in a terminal. No special protocol, no RPC, no persistent state.

## Knowledge Architecture

All Milliways state lives in a single SQLite database: `~/.local/share/milliways/milliways.db`. WAL mode for concurrent reads during async dispatches. Target size: ~2 MB for a typical project.

### Tables (all `mw_` prefixed)

| Table | What It Stores | Routing Impact |
|-------|----------------|----------------|
| `mw_ledger` | Every dispatch: ts, task_hash, kitchen, duration, exit_code, cost | Historical success rates per task_type x kitchen |
| `mw_tickets` | Async/detached dispatch tracking: ticket_id, status, result_path | Ticket lookup for `milliways status` |
| `mw_gitgraph` | File hotspots, churn, blame, change coupling | high_churn -> claude-kitchen |
| `mw_quality` | Cyclomatic/cognitive complexity, coverage, smells per file | high_complexity + low_coverage -> careful kitchen |
| `mw_deps` | Packages, versions, CVEs, consumers | CVE-exposed -> security-first routing |
| `mw_routing` | Learned weights: task_type x kitchen -> success_rate | Sommelier consults this before every dispatch |
| `mw_quotas` | Runtime quota state: daily counts, last dispatch ts, active count | PreRoute/PreDispatch enforcement |

### What lives outside milliways.db

MemPalace and CodeGraph remain external, accessed via MCP. They are pantry items -- Milliways reads from them but does not own them. The ndjson ledger (`~/.local/share/milliways/ledger.ndjson`) is kept as a human-readable audit trail only. The SQLite `mw_ledger` table is the source of truth.

### Single-query routing

Because all routing-relevant data lives in one database, the sommelier can JOIN across tables in a single query:

```sql
SELECT k.kitchen, k.success_rate,
       g.churn_score, q.complexity, d.has_cve
FROM mw_routing k
LEFT JOIN mw_gitgraph g ON g.file_path = :file
LEFT JOIN mw_quality q ON q.file_path = :file
LEFT JOIN mw_deps d ON d.package = :package
WHERE k.task_type = :task_type
ORDER BY k.success_rate DESC;
```

No graph database. No in-memory cache. One SQLite file, one query, one routing decision.

## OpenHands as Kitchen

OpenHands runs in a Docker sandbox container. It cannot be synchronous -- container startup alone takes seconds, and typical tasks run for minutes. Async-only, always.

### Adapter

`delegate_to_openhands.sh` is the kitchen command. It:

1. Starts (or reuses) an OpenHands Docker container
2. Passes the task via stdin or file mount
3. Returns immediately with a ticket ID
4. Writes results to a known path when done
5. Milliways polls or watches for completion

### Constraints

OpenHands is the kitchen that most needs quotas:

- **max_concurrent: 1** -- Docker containers are heavy
- **max_memory_mb: 4096** -- hard Docker `--memory` limit
- **max_duration: 1800** -- 30-minute timeout, hard kill after
- **daily_dispatches: 20** -- prevent runaway automation
- **cooldown_sec: 30** -- let the system breathe between dispatches

### Why it matters

OpenHands can do things no other kitchen can: run arbitrary code in an isolated sandbox, install packages, execute tests in a clean environment, make network calls. But it is the most expensive kitchen in terms of local resources. Milliways makes it safe to delegate to OpenHands by wrapping it in quotas, tickets, and ledger tracking.

## Pantry (shared knowledge, all kitchens)

### Existing (already built)

| Store | Backend | What It Knows |
|-------|---------|---------------|
| MemPalace | ChromaDB + Temporal KG | Decisions, context, cross-session memory (10,839 embeddings) |
| CodeGraph | tree-sitter + SQLite/FTS5 | Symbols, call graphs, blast radius, impact analysis |
| TaskQueue | SQLite | Task delegation, status, agent-to-agent coordination |
| OpenSpec | Markdown files | Proposals, designs, specs, tasks, delivery plans |

### New (to build)

| Store | Backend | What It Knows | Routing Impact |
|-------|---------|---------------|----------------|
| GitGraph | milliways.db (`mw_gitgraph`) | Hotspots, churn, blame, change coupling | high_churn -> claude-kitchen |
| QualityGraph | milliways.db (`mw_quality`) | Cyclomatic/cognitive complexity, coverage, smells | high_complexity + low_coverage -> careful kitchen |
| DepGraph | milliways.db (`mw_deps`) | Packages, versions, CVEs, consumers | CVE-exposed -> security-first routing |
| RoutingLedger | milliways.db (`mw_ledger`) + ndjson | Task type, kitchen, outcome, cost, duration | Learned preference from history |
| TopologyGraph | SQLite (from simulator) | Services, dependencies, blast radius | High-fanout -> escalate review |

## Recipes (multi-course workflows)

| Recipe | Courses | Flow |
|--------|---------|------|
| implement-feature | think -> code -> test -> review -> commit | claude -> opencode -> opencode -> claude -> aider |
| fix-bug | research -> diagnose -> fix -> test -> commit | gemini -> claude -> opencode -> opencode -> aider |
| security-audit | scan -> review -> fix -> verify | goose -> claude -> opencode -> claude |
| explore-idea | research -> think -> design -> propose | gemini -> claude -> claude -> claude |
| refactor-module | analyze -> plan -> refactor -> review | claude -> claude -> aider -> claude |

## Scope

### In scope
- Go CLI binary (Bubble Tea TUI + headless mode)
- Kitchen adapters (claude, opencode, gemini, aider, goose, cline, openhands)
- Sommelier router (keyword -> kitchen, with pantry consultation)
- Pantry integration (MCP clients for MemPalace, CodeGraph, TaskQueue)
- New knowledge tables in milliways.db (GitGraph, QualityGraph, DepGraph, RoutingLedger)
- Recipe engine (multi-course sequential execution with context handoff)
- Dispatch modes (sync, async, detached, recipe)
- Hook chain (SessionStart through SessionEnd)
- Circuit breaker (company/private mode enforcement)
- Resource quotas (per-kitchen and global)
- Configuration via `~/.config/milliways/carte.yaml`
- Routing ledger with `--json` output for analysis
- Tiered-CLI feedback reporting (`milliways report --tiered`)
- Neovim plugin (`milliways.nvim`)
- Carte.md as new OpenSpec artifact type

### Out of scope
- Web UI (CLI/TUI only)
- Model hosting (kitchens bring their own models)
- Replacing any kitchen (Milliways orchestrates, never cooks)
- Cloud deployment (local-first, single developer tool)
- Multi-user / team features (single-seat tool)

## Core Principle: CLI-native, Zero Credentials

Milliways never touches API keys, tokens, or credentials. Each kitchen is a CLI tool the user has already logged into independently. Milliways calls the binary -- same as if the user typed the command themselves.

Milliways target memory: <20 MB resident. No in-memory caches. All state on disk in milliways.db. If Milliways is killed at any point, no data is lost -- WAL mode and ndjson append handle crash recovery.

One SQLite DB for all Milliways state. MemPalace and CodeGraph are accessed via MCP as pantry items, not owned by Milliways.

```
What Milliways does:           What Milliways does NOT do:
---------------------          -----------------------------
  exec claude -p "..."           call api.anthropic.com
  exec opencode run "..."        read ANTHROPIC_API_KEY
  exec gemini "..."              store any credential
  check if binary exists         manage authentication
  stream stdout line by line     proxy HTTP requests
  capture exit code              intercept model responses
  enforce quotas + hooks         replace kitchen-internal hooks
  write to milliways.db          run an in-memory cache
```

If a kitchen isn't installed or authenticated, Milliways skips it and routes to the next available kitchen. `milliways --explain` shows which kitchens are available and which are missing.

## Non-Goals
- API key management or credential storage
- Token-level cost optimization (kitchens handle their own billing)
- Acting as a proxy or gateway between user and model providers
- Model fine-tuning or training
- Replacing OpenSpec (Milliways consumes OpenSpec, doesn't replace it)
- Replacing any kitchen CLI (Milliways orchestrates, never cooks)
- IDE integration beyond Neovim plugin (terminal-first)
- Model management (Milliways observes Ollama state but doesn't control which model is loaded)
- Replicating kitchen-internal hooks (each kitchen manages its own hook chain; Milliways hooks operate at the routing layer only)
- Graph database or heavy analytics engine (SQLite at our scale is sufficient)

## Acceptance Criteria

### Service 1 — Core + First Kitchen (MVP)
- `milliways "explain the auth flow"` routes to claude, streams response, logs to ledger
- `milliways --kitchen opencode "add rate limiting"` forces a specific kitchen
- `milliways --json "task"` outputs structured JSON result
- Ledger records: ts, task hash, kitchen, duration, exit code
- milliways.db created on first run with all `mw_` tables

### Service 2 — Pantry + Sommelier
- Sommelier consults CodeGraph + GitGraph before routing
- High-churn + high-complexity file -> routes to claude automatically
- `milliways --explain "task"` shows routing reasoning without executing

### Service 3 — Recipes + Multi-Course
- `milliways --recipe implement-feature "add JWT auth"` executes full think->code->test->review->commit
- Context passes between courses (stdout -> next course stdin or temp file)
- Each course logged separately in ledger
- Async and detached dispatch modes work for individual courses

### Service 4 — TUI
- `milliways --tui` opens interactive mode with Bubble Tea
- Split panes: input, output stream, ledger
- Kitchen selector with tab completion

### Service 5 — Hooks, Circuit Breaker, Quotas
- Hook chain fires all six events (SessionStart through SessionEnd) with measurable latency <5ms per hook
- Circuit breaker reads `~/.claude/mode` and blocks dispatches to non-allowed kitchens in company mode
- Blocked kitchen in a recipe fails the entire recipe at planning time, not mid-execution
- Per-kitchen quotas enforced: max_concurrent, max_duration, daily_dispatches, cooldown_sec
- Global quotas enforced: max_total_concurrent, pause_if_memory_above
- OpenHands kitchen runs async-only with Docker memory limits from quota config
- PostDispatch hook writes outcome to ledger and updates routing weights in mw_routing
- Failure recovery: PostDispatch re-routes to fallback kitchen on non-zero exit code (configurable)

### Service 6 — Full Pantry + Feedback Loop
- All knowledge tables populated and consulted (mw_gitgraph, mw_quality, mw_deps, mw_routing)
- Routing accuracy measurably better than keyword-only (A/B test via ledger)
- `milliways report --tiered` produces task_type x kitchen matrix with lift calculation
- Tiered report demonstrates >20% weighted lift over best single-kitchen baseline

### Neovim Plugin
- `:Milliways "explain this"` dispatches to milliways binary and shows result in floating window
- `:MilliwaysExplain` sends visual selection with LSP diagnostics as context
- `:MilliwaysKitchen claude "review"` forces kitchen selection
- `:MilliwaysRecipe implement-feature "task"` runs recipe with progress in floating window
- `:MilliwaysStatus` shows active tickets and recent ledger
- Apply (CR), yank (y), retry (r), close (q) keybindings work in result window
- Plugin memory footprint <1 MB (no persistent process, no daemon)
