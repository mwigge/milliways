# Design — Milliways

## Architecture

```
$ milliways "refactor auth middleware to use JWT"
       │
       ▼
┌──────────────────────────────────────────────────────────────┐
│                      MAITRE D' (Go binary)                    │
│                                                                │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐    │
│  │ CLI      │  │Sommelier │  │ Recipe   │  │ PantryDB │    │
│  │ Parser   │→ │ (router) │→ │ Engine   │→ │ (SQLite) │    │
│  └──────────┘  └────┬─────┘  └──────────┘  └──────────┘    │
│                      │                                        │
│              ┌───────▼────────┐                               │
│              │  HOOK CHAIN    │                               │
│              │  (6 events)    │                               │
│              └───────┬────────┘                               │
│                      │                                        │
│         ┌────────────┼────────────┐                           │
│         ▼            ▼            ▼                           │
│    MemPalace    CodeGraph     milliways.db                   │
│    (MCP)        (MCP)        (all mw_* tables)               │
└──────────────────────────────────────────────────────────────┘
       │
       │  exec.Command() per kitchen
       │  4 dispatch modes: sync | async | detached | recipe
       │
       ├──────────────────┬──────────────────┬─────────────────┐
       ▼                  ▼                  ▼                 ▼
┌─────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────┐
│ claude -p   │  │ opencode run │  │ gemini       │  │ aider    │
│ (cloud)     │  │ (local)      │  │ (free)       │  │ (git)    │
│             │  │              │  │              │  │          │
│ stdout ────►│  │ stdout ─────►│  │ stdout ─────►│  │ stdout ──►│
│ exit code   │  │ exit code    │  │ exit code    │  │ exit code │
└─────────────┘  └──────────────┘  └──────────────┘  └──────────┘
       │                                                        │
       └───── Kitchen Opacity: black box, scored as unit ──────┘
```

## Module Structure

```
milliways/
├── cmd/
│   └── milliways/
│       └── main.go              # CLI entry point (cobra)
├── internal/
│   ├── maitre/                  # Maitre d' — top-level orchestrator
│   │   ├── maitre.go            # Dispatch loop
│   │   └── config.go            # Load carte.yaml
│   │
│   ├── sommelier/               # Routing intelligence
│   │   ├── sommelier.go         # Route(task) → kitchen
│   │   ├── keywords.go          # Keyword-based routing (fast path)
│   │   ├── pantry_signals.go    # Consult knowledge graphs
│   │   ├── learned.go           # Historical success-based routing
│   │   └── skills.go            # Skill catalog awareness (D22)
│   │
│   ├── kitchen/                 # Kitchen adapters
│   │   ├── kitchen.go           # Kitchen interface
│   │   ├── claude.go            # claude -p adapter
│   │   ├── opencode.go          # opencode run adapter
│   │   ├── gemini.go            # gemini adapter
│   │   ├── aider.go             # aider --message adapter
│   │   ├── goose.go             # goose adapter
│   │   └── cline.go             # cline -y --json adapter
│   │
│   ├── pantry/                  # Unified data layer (milliways.db)
│   │   ├── db.go                # PantryDB — single connection, typed accessors
│   │   ├── migrations/          # Embedded SQL migration files
│   │   │   └── 001_initial.sql
│   │   ├── ledger.go            # LedgerStore
│   │   ├── tickets.go           # TicketStore
│   │   ├── gitgraph.go          # GitGraphStore
│   │   ├── quality.go           # QualityStore
│   │   ├── deps.go              # DepStore
│   │   ├── routing.go           # RoutingStore
│   │   ├── quotas.go            # QuotaStore
│   │   ├── mempalace.go         # MCP client for MemPalace
│   │   └── codegraph.go         # MCP client for CodeGraph
│   │
│   ├── hooks/                   # Hook chain (6 events)
│   │   ├── chain.go             # Hook registry and dispatcher
│   │   ├── session.go           # SessionStart / SessionEnd
│   │   ├── route.go             # PreRoute / PostRoute
│   │   ├── dispatch.go          # PreDispatch / PostDispatch
│   │   ├── circuit.go           # Circuit breaker (mode-aware)
│   │   └── recovery.go          # Failure recovery strategies
│   │
│   ├── recipe/                  # Multi-course workflows
│   │   ├── engine.go            # Execute recipe steps
│   │   ├── context.go           # Context handoff between courses
│   │   └── builtin.go           # Built-in recipes
│   │
│   ├── dispatch/                # Dispatch mode management
│   │   ├── modes.go             # sync / async / detached / recipe
│   │   ├── tickets.go           # Ticket lifecycle for async mode
│   │   └── detached.go          # Detached process management
│   │
│   ├── quotas/                  # Resource quota enforcement
│   │   ├── enforcer.go          # PreDispatch quota check
│   │   └── queue.go             # Queue when at limit
│   │
│   └── tui/                     # Bubble Tea interactive mode
│       ├── app.go               # Main TUI model
│       ├── input.go             # Task input component
│       ├── output.go            # Streaming output viewport
│       ├── ledger_panel.go      # Live ledger display
│       ├── processmap.go        # Live process map (D12)
│       └── styles.go            # Lipgloss theme
│
├── plugins/
│   └── milliways.nvim/          # Neovim plugin (D20)
│       ├── lua/
│       │   └── milliways/
│       │       ├── init.lua      # Plugin entry, setup(), commands
│       │       ├── dispatch.lua  # jobstart() wrapper
│       │       ├── context.lua   # Visual selection, LSP, git diff injection
│       │       ├── window.lua    # Floating window with q/a/y/r actions
│       │       └── config.lua    # User-configurable keybindings
│       └── plugin/
│           └── milliways.vim     # Autoload shim
│
├── carte.yaml                   # Default kitchen configuration
├── go.mod
├── go.sum
└── README.md
```

## Design Decisions

### D1: Go with Bubble Tea (not Rust, not Python)

**Chosen**: Go + Bubble Tea + Lipgloss

Why:
- Single static binary, zero runtime dependencies
- Goroutines for concurrent kitchen streaming
- Bubble Tea handles both TUI and headless from same binary
- Lipgloss produces beautiful terminal output
- Fast startup (~5ms vs Python ~200ms)
- Cross-platform (macOS + Linux)

Trade-off: Rust would be faster but longer development cycle. Python would be quicker to prototype but distribution is painful.

### D2: Kitchens are logged-in CLIs, not API calls

**Chosen**: Shell out to CLI tools via `os/exec`. Each tool is already authenticated by the user independently.

```go
type Kitchen interface {
    Name() string
    Exec(ctx context.Context, task Task) (Result, error)
    Stations() []string
    CostTier() CostTier
    Available() bool  // is the binary installed and authenticated?
}
```

Each kitchen adapter builds a CLI command, executes it, captures stdout/stderr, and returns a Result. **Milliways never calls model APIs directly. Milliways never stores, manages, or reads API tokens.**

The user logs into each CLI tool independently, the way they normally would:
- `claude` — authenticated via Anthropic account (Max/Team/Enterprise subscription)
- `opencode` — uses local Ollama, no auth needed (or configured provider keys in opencode.json)
- `gemini` — authenticated via Google account (`gcloud auth login` or API key in env)
- `aider` — user sets `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` in their own shell environment
- `goose` — user configures provider in `~/.config/goose/`
- `cline` — user configures model/provider in `~/.cline/`

Milliways is **not a proxy and not a gateway**. It's a maitre d' that seats you at the right table — it doesn't cook, and it doesn't pay the bill.

Why:
- Each CLI brings its own auth, runtime, tools, MCP servers, hooks, context management
- No API key leakage risk — Milliways has zero access to credentials
- No need to replicate Claude Code's hook chain or OpenCode's plugin system
- CLIs evolve independently — Milliways just needs the exec interface
- Billing stays with each tool's own channel (subscription, usage-based, free tier)
- User can verify each kitchen works independently before Milliways orchestrates them

**Availability check**: Before routing to a kitchen, Milliways runs a quick probe:
```go
func (k *ClaudeKitchen) Available() bool {
    _, err := exec.LookPath("claude")
    return err == nil
}
```
If a kitchen isn't installed or isn't authenticated, the sommelier skips it and routes to the next best option. `milliways --explain` shows: "aider: unavailable (binary not found), falling back to opencode".

Trade-off: Less control over streaming granularity. Mitigated by reading stdout line-by-line via `bufio.Scanner`.

### D3: Sommelier uses three-tier routing (fast -> enriched -> learned)

```
Tier 1: Keyword match (< 1ms)
  "refactor" → aider
  "explain" → claude
  "search" → gemini

Tier 2: Pantry signals (< 50ms, if Tier 1 is ambiguous)
  CodeGraph: file complexity, call count
  GitGraph: churn, last author, stability
  QualityGraph: coverage, smells

Tier 3: Learned routing (< 10ms, SQLite lookup)
  RoutingLedger: "for this task type + file profile,
                  which kitchen succeeded most often?"
```

Why: Fast path handles 80% of tasks. Pantry consultation adds intelligence. Learned routing improves over time.

### D4: Context handoff between recipe courses uses temp files

**Chosen**: Structured JSON in `/tmp/milliways-{recipe-id}-{course-n}.json`

```json
{
  "course": 1,
  "kitchen": "claude",
  "task": "design JWT auth middleware",
  "output": "... claude's full response ...",
  "files_changed": [],
  "exit_code": 0
}
```

Next course receives: `--context /tmp/milliways-{id}-{n}.json` which the kitchen adapter injects into the prompt.

Why: Pipes (stdout->stdin) lose structure. Temp files are inspectable, debuggable, and survive kitchen crashes. Cleaned up after recipe completes (or `--keep-context` to preserve).

### D5: Pantry knowledge graphs are SQLite, not a graph DB

**Chosen**: Single unified SQLite database (`milliways.db`) with `mw_` prefixed tables (see D13).

Why:
- Zero external dependencies (no Neo4j, no Postgres)
- Fast enough for single-developer use (< 10ms queries)
- SQLite is already used by CodeGraph, TaskQueue, and MemPalace KG
- Portable (copy the file, done)
- `go-sqlite3` is battle-tested
- Single `*sql.DB` connection avoids file lock contention

### D6: Ledger is append-only ndjson + SQLite table

**Chosen**: Dual write — SQLite `mw_ledger` table is the source of truth for routing queries, ndjson kept as human-readable audit trail.

```
~/.config/milliways/ledger.ndjson    (append-only, human-readable, jq-friendly, never read by Milliways)
~/.config/milliways/milliways.db     (mw_ledger table — all routing queries hit this)
```

Ledger record:
```json
{
  "ts": "2026-04-13T14:23:00Z",
  "task_hash": "sha256:abc123",
  "task_type": "refactor",
  "kitchen": "aider",
  "station": "multi-file",
  "file": "chaosengine/store.py",
  "file_churn": 18,
  "file_complexity": 34,
  "duration_s": 8.1,
  "exit_code": 0,
  "lines_added": 42,
  "lines_removed": 17,
  "cost_est_usd": 0.000,
  "outcome": "success"
}
```

`outcome` is initially set from exit code. Future: user feedback (`milliways rate last good/bad`).

### D7: Configuration via carte.yaml, not environment variables

**Chosen**: `~/.config/milliways/carte.yaml`

```yaml
kitchens:
  claude:
    cmd: claude
    args: ["-p"]
    stations: [think, plan, review, explore, sign-off]
    cost_tier: cloud
    env:
      ANTHROPIC_MODEL: claude-sonnet-4-6

  opencode:
    cmd: opencode
    args: ["run"]
    stations: [code, test, refactor, lint, commit]
    cost_tier: local

  gemini:
    cmd: gemini
    args: []
    stations: [search, compare, docs, research]
    cost_tier: free

  aider:
    cmd: aider
    args: ["--message", "--yes-always", "--no-suggest-shell-commands"]
    stations: [multi-file, git-commit]
    cost_tier: cloud

  goose:
    cmd: goose
    args: []
    stations: [tools, database, api, mcp]
    cost_tier: local

routing:
  keywords:
    think: claude
    plan: claude
    explain: claude
    explore: claude
    review: claude
    code: opencode
    implement: opencode
    test: opencode
    build: opencode
    refactor: aider
    search: gemini
    research: gemini
    compare: gemini
    tools: goose
    database: goose

  default: claude
  budget_fallback: opencode

pantry:
  mempalace:
    type: mcp
    cmd: ["python3", "-m", "mempalace.mcp_server"]
  codegraph:
    type: mcp
    cmd: ["/opt/homebrew/bin/codegraph", "serve", "--mcp"]

ledger:
  ndjson: ~/.config/milliways/ledger.ndjson
  db: ~/.config/milliways/milliways.db

recipes:
  implement-feature:
    - { station: think, kitchen: claude }
    - { station: code, kitchen: opencode }
    - { station: test, kitchen: opencode }
    - { station: review, kitchen: claude }
    - { station: git-commit, kitchen: aider }

  fix-bug:
    - { station: research, kitchen: gemini }
    - { station: think, kitchen: claude }
    - { station: code, kitchen: opencode }
    - { station: test, kitchen: opencode }
    - { station: git-commit, kitchen: aider }

  security-audit:
    - { station: tools, kitchen: goose }
    - { station: review, kitchen: claude }
    - { station: code, kitchen: opencode }
    - { station: review, kitchen: claude }
```

### D8: MCP communication for MemPalace and CodeGraph

**Chosen**: Go MCP client (JSON-RPC over stdio)

Milliways spawns MCP servers as child processes and communicates via stdin/stdout JSON-RPC. Same protocol as Claude Code and OpenCode use.

```go
type MCPClient struct {
    cmd    *exec.Cmd
    stdin  io.Writer
    stdout *bufio.Scanner
}

func (c *MCPClient) Call(method string, params any) (json.RawMessage, error)
```

Why: Reuses existing MCP servers without modification. MemPalace and CodeGraph already expose MCP interfaces. No new server code needed.

### D9: Carte.md as new OpenSpec artifact

Every OpenSpec change gains an optional `carte.md` file:

```markdown
## Carte — http-fault-injection

| Task | Kitchen | Station | Context Injection |
|------|---------|---------|-------------------|
| APP-R1 | claude | review | CodeGraph: client.py symbols |
| APP1 | opencode | code | CodeGraph: chaosnetwork probe pattern |
| APP2 | opencode | code | CodeGraph: latency.py, partition.py |
| APP3 | opencode | code | Self-contained |
| APP4 | opencode | code | MemPalace: metadata enrichment schema |
| APP5 | claude+opencode | think+code | CodeGraph: requests.Session calls |
| HTTP-GATE1 | claude | sign-off | Full pantry context |
```

When `milliways --recipe opsx:apply` runs, it reads `carte.md` to route each task to the right kitchen.

### D10: Streaming output via bufio.Scanner

```go
func (k *ClaudeKitchen) Exec(ctx context.Context, task Task) (Result, error) {
    cmd := exec.CommandContext(ctx, "claude", "-p", task.Prompt)
    stdout, _ := cmd.StdoutPipe()
    cmd.Start()

    scanner := bufio.NewScanner(stdout)
    for scanner.Scan() {
        line := scanner.Text()
        task.OnLine(line)  // TUI update or headless print
    }

    cmd.Wait()
    return Result{ExitCode: cmd.ProcessState.ExitCode()}, nil
}
```

Each line is emitted as it arrives — no buffering the entire response. In TUI mode, lines update the viewport. In headless mode, lines print to stdout.

### D11: Kitchen Onboarding — Install, Authenticate, Continue (no restart)

Milliways detects missing or unauthenticated kitchens and offers to fix them inline.

**First run experience** (`milliways` with no carte.yaml):
```
┌─ Milliways — First Run ─────────────────────────────────────────┐
│                                                                   │
│  Welcome to Milliways. Let's see which kitchens are ready.       │
│                                                                   │
│  Kitchen          Status              Action                     │
│  ───────          ──────              ──────                     │
│  claude           ✓ installed, logged in                         │
│  opencode         ✗ not installed      [i] brew install opencode │
│  gemini           ✓ installed, not logged in  [a] gcloud auth    │
│  aider            ✗ not installed      [i] pip install aider     │
│  goose            ⊘ disabled           (carte.yaml: enabled=no)  │
│  cline            ✗ not installed      [i] npm install -g cline  │
│                                                                   │
│  Minimum: 1 kitchen. You have 1 ready (claude).                  │
│                                                                   │
│  [Enter] continue with available kitchens                        │
│  [i] install a kitchen now                                       │
│  [a] authenticate a kitchen now                                  │
│  [s] skip — I'll set up later                                    │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

**Three-level availability check per kitchen**:

```go
type KitchenStatus int
const (
    KitchenReady        KitchenStatus = iota  // installed + authenticated
    KitchenNeedsAuth                           // installed but not logged in
    KitchenNotInstalled                        // binary not found
    KitchenDisabled                            // user set enabled: false
)

func (k *ClaudeKitchen) Status() KitchenStatus {
    if !k.enabled { return KitchenDisabled }
    if _, err := exec.LookPath("claude"); err != nil {
        return KitchenNotInstalled
    }
    // Probe auth: claude --version returns 0 even without auth,
    // but claude -p "test" returns exit 1 with auth error
    if !k.probeAuth() { return KitchenNeedsAuth }
    return KitchenReady
}
```

**Installation helpers** (per kitchen):

| Kitchen | Install Command | Auth Command | Auth Type |
|---------|----------------|-------------|-----------|
| claude | `brew install claude` or `npm i -g @anthropic-ai/claude-code` | `claude` (interactive login, opens browser) | OAuth browser |
| opencode | `brew install opencode` | None (uses Ollama) | N/A |
| ollama | `brew install ollama && ollama serve` | None | N/A |
| gemini | `npm i -g @anthropic-ai/gemini-cli` | `gcloud auth login` (opens browser) | OAuth browser |
| aider | `pip install aider-chat` | Prompt for API key -> write to `~/.aider.conf.yml` | API key in config |
| goose | `brew install goose` | `goose configure` (interactive wizard) | Interactive |
| cline | `npm i -g @anthropic-ai/cline` | `cline --login` (opens browser) | OAuth browser |

**Live install flow** (no restart):
```
> milliways --setup opencode

  Installing opencode...
  $ brew install opencode
  ==> Downloading...
  ==> Installing opencode
  ✓ opencode installed (v1.4.2)

  opencode uses local models via Ollama.
  Checking Ollama... ✓ running on :11434
  Checking model... devstral:24b not found

  Pull devstral:24b? (~9.6 GB) [y/N] y
  $ ollama pull devstral:24b
  pulling manifest... ████████████████████ 100%

  ✓ opencode kitchen ready.
  Added to carte.yaml.
```

**Live auth flow** (no restart):
```
> milliways --setup gemini

  gemini is installed but not authenticated.
  Opening browser for Google authentication...
  $ gcloud auth login
  
  Waiting for browser... ✓ authenticated as morgan@example.com

  ✓ gemini kitchen ready.
```

**Key design point**: Because Milliways calls kitchens via `exec.Command()` (spawning fresh child processes), a newly installed or authenticated CLI is immediately usable. The parent Milliways process doesn't need to restart — the next dispatch will find the binary and use the fresh credentials.

**Graceful degradation** — Milliways adapts to what's available:

```yaml
# carte.yaml — user controls which kitchens are enabled
kitchens:
  claude:
    enabled: true       # ← user has subscription
    cmd: claude
    args: ["-p"]
    
  opencode:
    enabled: true       # ← free, local
    cmd: opencode
    args: ["run"]
    
  gemini:
    enabled: true       # ← free tier
    cmd: gemini
    args: []
    
  aider:
    enabled: false      # ← user doesn't use aider
    cmd: aider
    args: ["--message", "--yes-always"]
    
  goose:
    enabled: false      # ← user doesn't use goose
    cmd: goose
    args: []
    
  cline:
    enabled: false      # ← user doesn't use cline
    cmd: cline
    args: ["-y", "--json"]
```

**Sommelier adapts routing to available kitchens**:
- If only `opencode` is available -> everything routes there (single-kitchen mode)
- If `claude` + `opencode` -> classic two-tier (think cloud, code local)
- If `claude` + `opencode` + `gemini` -> add free research capability
- Full menu -> sommelier has maximum routing flexibility

**Minimum viable setup**: Just one kitchen. Milliways works (with reduced routing) even with a single local model via opencode. The value of Milliways scales with the number of available kitchens but never requires all of them.

**`milliways status`** — quick check at any time:
```
$ milliways status

  Kitchen     Status              Model              Cost
  ───────     ──────              ─────              ────
  claude      ✓ ready             sonnet-4.6         cloud
  opencode    ✓ ready             devstral:24b       local
  gemini      ✓ ready             gemini-3           free
  aider       ⊘ disabled          —                  —
  goose       ✗ not installed     —                  —
  cline       ✗ not installed     —                  —

  Pantry: MemPalace ✓ | CodeGraph ✓ | milliways.db ✓ (8 tables)
  Ledger: 142 entries | Last: 3m ago
  
  3/6 kitchens ready. Run 'milliways --setup <kitchen>' to add more.
```

### D12: 24 GB RAM constraint — only one kitchen active at a time

Default behavior: sequential execution. Only one kitchen subprocess at a time. When a recipe has multiple courses, each completes before the next starts.

Exception: `--parallel` flag for independent courses (e.g., gemini research + opencode code on different files). But parallel requires both kitchens to be non-overlapping on Ollama (one cloud + one local is fine; two local models would swap).

### D12b: Live Process Map in TUI (top-right corner)

The TUI always shows a minimap of the current state — what's happening, what's next, where we are in a recipe.

**Single dispatch view**:
```
┌─ Process Map ───────────┐
│                          │
│  Task: "refactor auth"   │
│                          │
│  Sommelier:              │
│    keywords → refactor   │
│    GitGraph → churn: 18  │
│    CodeGraph → cx: 34    │
│    Risk: HIGH            │
│    ▶ Kitchen: claude     │
│                          │
│  Status: ● streaming     │
│  Elapsed: 4.2s           │
└──────────────────────────┘
```

**Recipe view** (multi-course):
```
┌─ Process Map ───────────┐
│                          │
│  Recipe: implement-feat  │
│                          │
│  ✓ think    claude  2.1s │
│  ● code    opencode ...  │
│  ○ test    opencode      │
│  ○ review  claude        │
│  ○ commit  aider         │
│                          │
│  Course 2/5 | 12.4s      │
└──────────────────────────┘
```

**Symbols**: `✓` done, `●` active (pulsing), `○` pending, `✗` failed, `⊘` skipped (kitchen unavailable)

**Implementation**: A Bubble Tea component (`processmap.go`) that observes the dispatch state and renders accordingly. Updates on every tick (100ms) for elapsed time, on every state transition for course progress.

**Headless equivalent**: `--verbose` flag prints state transitions to stderr:
```
[sommelier] keywords=refactor gitgraph.churn=18 codegraph.cx=34 risk=HIGH → claude
[dispatch]  claude streaming...
[dispatch]  claude done (4.2s, exit=0)
```

### D13: Unified milliways.db (single SQLite, shared components)

**Chosen**: One SQLite file with WAL mode for all Milliways-owned state.

Previous design (D5, D6) used separate `.db` files per graph and a separate `ledger.db`. This consolidates everything into a single `~/.config/milliways/milliways.db` with `mw_` prefixed tables.

Why:
- Single `*sql.DB` connection = ~2 MB overhead (vs ~2 MB per separate DB)
- No file lock contention between components
- Cross-table JOINs enable the Knowledge Graph routing brain (D23)
- Migrations are atomic — one embedded migration set, applied on startup
- Portable — one file to backup, move, or inspect

**PantryDB struct with typed accessors**:
```go
type PantryDB struct {
    db *sql.DB
}

func Open(path string) (*PantryDB, error)  // opens + migrates
func (p *PantryDB) Ledger() *LedgerStore
func (p *PantryDB) Tickets() *TicketStore
func (p *PantryDB) GitGraph() *GitGraphStore
func (p *PantryDB) Quality() *QualityStore
func (p *PantryDB) Deps() *DepStore
func (p *PantryDB) Routing() *RoutingStore
func (p *PantryDB) Quotas() *QuotaStore
func (p *PantryDB) Close() error
```

**Migrations embedded via `go:embed`**, versioned, applied on startup:
```go
//go:embed migrations/*.sql
var migrationFS embed.FS
```

**MemPalace and CodeGraph stay external** — accessed via MCP (D8). They have their own data stores and their own lifecycle. Milliways does not own or manage their data.

**ndjson ledger kept as human-readable audit trail** — append-only, never read by Milliways at runtime. Exists purely for `jq` queries, debugging, and manual inspection.

**Full schema** (`001_initial.sql`):

```sql
-- milliways.db schema v1
-- WAL mode set programmatically on connection open

CREATE TABLE mw_schema (
    version     INTEGER PRIMARY KEY,
    applied_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE mw_ledger (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            TEXT NOT NULL,
    task_hash     TEXT NOT NULL,
    task_type     TEXT NOT NULL DEFAULT '',
    kitchen       TEXT NOT NULL,
    station       TEXT NOT NULL DEFAULT '',
    file          TEXT NOT NULL DEFAULT '',
    duration_s    REAL NOT NULL DEFAULT 0,
    exit_code     INTEGER NOT NULL DEFAULT 0,
    cost_est_usd  REAL NOT NULL DEFAULT 0,
    outcome       TEXT NOT NULL DEFAULT 'success',
    session_id    TEXT,
    parent_id     INTEGER,
    dispatch_mode TEXT DEFAULT 'sync'
);

CREATE TABLE mw_tickets (
    id            TEXT PRIMARY KEY,
    kitchen       TEXT NOT NULL,
    prompt        TEXT NOT NULL,
    mode          TEXT NOT NULL,
    pid           INTEGER,
    status        TEXT NOT NULL DEFAULT 'running',
    output_path   TEXT,
    started_at    TEXT NOT NULL,
    completed_at  TEXT,
    exit_code     INTEGER,
    ledger_id     INTEGER REFERENCES mw_ledger(id)
);

CREATE TABLE mw_gitgraph (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    repo          TEXT NOT NULL,
    file_path     TEXT NOT NULL,
    churn_30d     INTEGER NOT NULL DEFAULT 0,
    churn_90d     INTEGER NOT NULL DEFAULT 0,
    authors_30d   INTEGER NOT NULL DEFAULT 0,
    last_author   TEXT,
    last_changed  TEXT,
    stability     TEXT NOT NULL DEFAULT 'unknown',
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(repo, file_path)
);

CREATE TABLE mw_quality (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    repo                  TEXT NOT NULL,
    file_path             TEXT NOT NULL,
    function_name         TEXT,
    cyclomatic_complexity INTEGER,
    cognitive_complexity  INTEGER,
    coverage_pct          REAL,
    smell_count           INTEGER NOT NULL DEFAULT 0,
    updated_at            TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(repo, file_path, function_name)
);

CREATE TABLE mw_deps (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    repo            TEXT NOT NULL,
    package         TEXT NOT NULL,
    version         TEXT NOT NULL,
    latest_version  TEXT,
    cve_ids         TEXT,
    lock_file       TEXT,
    updated_at      TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(repo, package, lock_file)
);

CREATE TABLE mw_routing (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    task_type     TEXT NOT NULL,
    file_profile  TEXT NOT NULL DEFAULT '',
    kitchen       TEXT NOT NULL,
    success_count INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0,
    avg_duration  REAL NOT NULL DEFAULT 0,
    last_used     TEXT,
    UNIQUE(task_type, file_profile, kitchen)
);

CREATE TABLE mw_quotas (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kitchen     TEXT NOT NULL,
    date        TEXT NOT NULL,
    dispatches  INTEGER NOT NULL DEFAULT 0,
    total_sec   REAL NOT NULL DEFAULT 0,
    failures    INTEGER NOT NULL DEFAULT 0,
    UNIQUE(kitchen, date)
);

-- Indexes for common query patterns
CREATE INDEX idx_ledger_kitchen  ON mw_ledger(kitchen);
CREATE INDEX idx_ledger_outcome  ON mw_ledger(outcome);
CREATE INDEX idx_ledger_ts       ON mw_ledger(ts);
CREATE INDEX idx_gitgraph_stability ON mw_gitgraph(stability);
```

### D14: Hook Chain (6 events with failure recovery)

**Chosen**: Internal hook chain with 6 well-defined events. Not file-based hooks (like git hooks) — these are Go function chains registered at startup.

**Events**:

| Event | When | Responsibilities |
|-------|------|------------------|
| SessionStart | Binary starts | Read mode (`~/.claude/mode`), load config, diagnose kitchens, wake pantry (open DB) |
| PreRoute | Before sommelier runs | Circuit breaker check (D16), inject pantry signals (gitgraph, quality, deps) |
| PostRoute | After sommelier decides | Log routing decision, verify chosen kitchen is still available |
| PreDispatch | Before exec.Command | Inject mode context, set `--dir` restrictions (D16), enforce quotas (D17) |
| PostDispatch | After kitchen returns | Write ledger, update routing scores, check subdispatch log, validate output quality, execute recovery strategy on failure |
| SessionEnd | Binary exits | Flush DB, generate session summary, write ndjson |

**Recovery strategies** (configured per kitchen in `carte.yaml`):

```yaml
kitchens:
  opencode:
    recovery:
      strategy: retry
      max_retries: 1
  claude:
    recovery:
      strategy: fallback
      fallback_to: opencode
  gemini:
    recovery:
      strategy: save-partial
```

| Strategy | Behavior |
|----------|----------|
| `retry` | Same kitchen, up to `max_retries` (default 1). Reuses same prompt. |
| `fallback` | Route to `fallback_to` kitchen. Inject partial output as context so work isn't lost. |
| `save-partial` | Write whatever output was captured to a file. Notify user. Do not retry. |
| `abandon` | Log the failure, record in ledger, move on. Default for recipes (course failure stops recipe). |

**PostDispatch failure flow**:
```
kitchen exits non-zero
  → write ledger entry (outcome=failure)
  → check recovery strategy for this kitchen
  → retry?    re-dispatch to same kitchen (increment attempt counter)
  → fallback? re-route via sommelier with kitchen excluded, inject partial output
  → save?     write output to /tmp/milliways-partial-{id}.txt, print path
  → abandon?  log and return error to caller
```

### D15: Four Dispatch Modes

**Chosen**: Four modes that cover the full spectrum from interactive to fire-and-forget.

| Mode | Flag | Behavior |
|------|------|----------|
| `sync` | (default) | Milliways waits, streams output line-by-line, blocks terminal until kitchen exits |
| `async` | `--async` | Returns a ticket ID immediately. Kitchen runs in background. Check with `milliways ticket {id}` |
| `detached` | `--detach` | Process survives Milliways exit. Output goes to log file. Check with `milliways ticket {id}` |
| `recipe` | `--recipe {name}` | Multi-course sequential. Context handoff between courses via temp files (D4) |

**Async flow**:
```
$ milliways --async "refactor auth middleware"
Ticket: mw-a1b2c3
Kitchen: opencode
Status: running

$ milliways ticket mw-a1b2c3
Status: completed (14.2s)
Exit: 0
Output: ~/.config/milliways/tickets/mw-a1b2c3/output.txt
```

**Detached flow**:
```
$ milliways --detach "generate test suite for store.py"
Ticket: mw-d4e5f6 (detached, PID 42891)
Output: ~/.config/milliways/tickets/mw-d4e5f6/output.txt

# Milliways can exit — the kitchen process keeps running
# Later:
$ milliways ticket mw-d4e5f6
Status: completed (182.3s)
```

**Ticket storage**: `mw_tickets` table in milliways.db. Detached processes write a PID file so Milliways can check liveness on next invocation.

### D16: Circuit Breaker Integration

**Chosen**: Mode-aware path and kitchen restrictions, enforced at PreRoute and PreDispatch.

**Mode file**: `~/.claude/mode` contains either `company` or `private`. Read once at SessionStart.

| Mode | Available Kitchens | Allowed Paths |
|------|-------------------|---------------|
| `company` | Approved kitchens only (e.g., claude, opencode) | `~/work/`, `~/src/company/` |
| `private` | All kitchens | `~/personal/`, `~/src/private/` |

**PreRoute enforcement**:
- If task targets a file in a blocked path: **hard stop**. No retry, no fallback. Return error immediately.
- If task targets an allowed path but kitchen is not approved for current mode: sommelier excludes it from candidates.

**PreDispatch enforcement**:
- Inject `--dir` flag (or equivalent) into kitchen command to restrict filesystem access.
- Pass mode context to kitchen environment variable (`MILLIWAYS_MODE=company`) so kitchens with their own circuit breakers can align.

```go
func (h *CircuitHook) PreRoute(ctx context.Context, task *Task) error {
    mode := h.currentMode()  // read from ~/.claude/mode
    if !mode.AllowsPath(task.TargetPath) {
        return fmt.Errorf("circuit breaker: path %s blocked in %s mode", task.TargetPath, mode)
    }
    task.ExcludeKitchens = mode.BlockedKitchens()
    return nil
}
```

### D17: Resource Quotas

**Chosen**: Per-kitchen and global resource limits, enforced at PreDispatch.

```yaml
kitchens:
  openhands:
    quotas:
      max_concurrent: 1
      max_duration: 30m
      max_memory_mb: 2048
      max_cpu_pct: 50
      cooldown_sec: 30

quotas:
  max_total_concurrent: 2
  pause_if_memory_above: 85
```

**Enforcement** (PreDispatch hook):
- Check `mw_quotas` table for today's usage against configured limits.
- Check running ticket count against `max_concurrent`.
- Check system memory via `/proc/meminfo` (Linux) or `sysctl hw.memsize` (macOS) against `pause_if_memory_above`.
- If at limit: **queue**. Return a ticket in `queued` status. Dispatch when a slot opens.

**Docker memory enforcement** (for container-based kitchens like OpenHands):
```go
func (k *OpenHandsKitchen) Exec(ctx context.Context, task Task) (Result, error) {
    args := []string{"run", "--memory", fmt.Sprintf("%dm", k.quotas.MaxMemoryMB), ...}
    cmd := exec.CommandContext(ctx, "docker", args...)
    // ...
}
```

**Quota tracking**: `mw_quotas` table accumulates daily per-kitchen: dispatch count, total seconds, failure count. Queryable via `milliways report --quotas`.

### D18: Tiered-CLI Feedback Loop

**Chosen**: Continuous measurement of which kitchen performs best for which task type, building toward a proof metric that multi-CLI routing outperforms any single CLI.

**Task type classification** (by sommelier):
- `think` — design, architecture, planning
- `code` — implementation, feature work
- `refactor` — restructuring without behavior change
- `search` — information retrieval, research
- `review` — code review, security audit
- `test` — test generation, coverage improvement

**Quality score**:
- Initially: derived from `exit_code` (0 = success, non-zero = failure)
- Future: user feedback via `milliways rate last good/bad` writes to `mw_ledger.outcome`

**Routing accumulation** (`mw_routing` table):
- Keyed by `(task_type, file_profile, kitchen)`
- `file_profile` = stability bucket from gitgraph (stable/volatile/new)
- Tracks `success_count`, `failure_count`, `avg_duration`

**Tiered report** (`milliways report --tiered`):
```
Task Type    Best Kitchen    Success%    Avg Duration
─────────    ────────────    ────────    ────────────
think        claude          94%         3.2s
code         opencode        87%         12.1s
refactor     aider           91%         8.4s
search       gemini          96%         2.1s
review       claude          89%         5.7s
test         opencode        83%         9.8s

Composite (multi-CLI): 91.2% success, 6.8s avg
Best single CLI (claude): 78.4% success, 7.2s avg

Lift: +12.8% success rate via multi-CLI routing
```

**Proof metric**: Lift percentage = composite multi-CLI score vs best single-CLI score across all task types.

### D19: Kitchen Opacity Principle

**Chosen**: Kitchen is a black box. Milliways scores the kitchen, not the model inside.

**Core principle**: When claude delegates to haiku internally, or when opencode calls delegate.sh, or when goose spawns sub-agents — that is the kitchen's business. Milliways sees:
- Input: prompt sent to kitchen
- Output: stdout, exit code, duration

**Milliways does not**:
- Inspect which model a kitchen used internally
- Track token counts per sub-model
- Override a kitchen's internal delegation strategy

**Optional observation**: When kitchens participate in a tiered-agent-architecture (shared `subdispatch.ndjson` log), PostDispatch can read this log for richer scoring. But this is opt-in and non-essential.

**Future**: Structured delegation protocol where kitchens can call back into Milliways:
```
milliways delegate --parent {ticket-id} --task "run tests" --kitchen opencode
```
This would let a kitchen explicitly request Milliways to handle a subtask with a different kitchen. Not in v1.

### D20: Neovim Plugin Architecture

**Chosen**: `milliways.nvim` — a Lua plugin that calls the milliways binary via `vim.fn.jobstart()`. No persistent process, no daemon, no socket.

**Commands**:

| Command | Behavior |
|---------|----------|
| `:Milliways {prompt}` | Dispatch prompt, show result in floating window |
| `:MilliwaysExplain` | Send visual selection with "explain this" |
| `:MilliwaysKitchen {name}` | Force-route to specific kitchen |
| `:MilliwaysRecipe {name}` | Run a named recipe |
| `:MilliwaysStatus` | Show kitchen status (like `milliways status`) |
| `:MilliwaysDetached {prompt}` | Detached dispatch, show ticket ID |

**Context injection** (automatic, based on editor state):

| Source | Flag | When |
|--------|------|------|
| Visual selection | `--context-lines` | Text selected in visual mode |
| Current file | `--context-file` | Always (current buffer path) |
| LSP symbol | `--context-symbol` | Cursor on a function/class |
| Git diff | `--context-diff` | Uncommitted changes in current file |

**Floating window actions**:

| Key | Action |
|-----|--------|
| `q` | Close window |
| `a` | Apply changes (if output contains a diff) |
| `y` | Yank output to clipboard |
| `r` | Retry with same prompt |

**Keybindings** (configurable):
- `<leader>mm` — open prompt input
- `<leader>me` — explain selection
- `<leader>ms` — show status

**Memory constraint**: < 1 MB overhead. No persistent process. The plugin spawns `milliways` on command and reads stdout. When the floating window closes, nothing remains in memory.

### D21: Memory Budget

**Chosen**: Strict memory targets to respect the 24 GB constraint (D12).

| Component | Budget | Notes |
|-----------|--------|-------|
| Milliways binary (idle) | ~8 MB | Go runtime + loaded config |
| Milliways binary (active) | < 20 MB | Including bufio buffers, SQL connection |
| milliways.db connection | ~2 MB | Single `*sql.DB` with WAL mode |
| MCP server pipe (each) | ~1 MB | stdio JSON-RPC, no buffering |
| ndjson append | 0 MB | Write-only, never read into memory |
| Kitchen subprocess | Varies | Owned by kitchen, not by Milliways |

**Design rules from this budget**:
- No in-memory caches — all state lives in milliways.db, queried on demand
- No in-memory routing tables — sommelier queries `mw_routing` directly
- MCP servers accessed via stdio pipes, not HTTP (avoids connection pooling overhead)
- Ollama model state observed via API (`/api/tags`), not controlled
- OpenHands container limits enforced via Docker `--memory` flag from quotas (D17)

### D22: Skill Catalog Awareness

**Chosen**: On SessionStart, scan known skill directories and build a transient catalog of which kitchen has which skills.

**Scan paths**:
- `~/.claude/skills/` (Claude Code custom skills)
- `~/.config/opencode/plugins/` (OpenCode plugins)
- Kitchen-specific skill directories as configured in `carte.yaml`

**Catalog structure** (in-memory, not persisted):
```go
type SkillCatalog struct {
    skills map[string][]string  // skill name → list of kitchens that have it
}

func (c *SkillCatalog) KitchensForSkill(skill string) []string
func (c *SkillCatalog) SkillsForKitchen(kitchen string) []string
```

**Sommelier integration**: When a task matches a skill keyword (e.g., "security review"), the sommelier checks the catalog to prefer kitchens that have the matching skill installed.

Example: "security review" matches the `security-review` skill. Catalog shows claude has it, opencode does not. Sommelier routes to claude even if keyword routing would suggest opencode.

**Not persisted**: Catalog is rebuilt on every SessionStart. No stale data. No storage cost. Scan takes < 10ms (just `os.ReadDir` + filename matching).

### D23: Knowledge Graph as Routing Brain (4 layers)

**Chosen**: Four conceptual layers stored across existing `mw_*` tables. No separate graph database. Cross-table JOINs provide the graph traversal.

**Layer 1: Task Graph** (what was asked)
- Source: `mw_ledger` — `parent_id` links create depends_on / followed_by edges
- Example: recipe course 2 has `parent_id` pointing to course 1's ledger entry

**Layer 2: Outcome Graph** (what happened)
- Source: `mw_ledger` + `mw_routing` — dispatched_to, produced, scored edges
- Example: task X dispatched_to opencode, produced exit_code 0, scored success

**Layer 3: Context Graph** (links to code knowledge)
- Source: `mw_gitgraph` + `mw_quality` + `mw_deps` — touched files, complexity, churn, dependencies
- Example: file store.py has churn_30d=18, cyclomatic_complexity=34, 2 CVEs in deps

**Layer 4: Session Graph** (conversations across CLIs)
- Source: `mw_ledger.session_id` — groups dispatches within a session
- Edge types: context_for (output of dispatch A fed into dispatch B), invalidated_by (later dispatch superseded earlier)

**Query example** — sommelier asks "what's the best kitchen for refactoring a high-churn Python file?":
```sql
SELECT r.kitchen, r.success_count, r.avg_duration
FROM mw_routing r
JOIN mw_gitgraph g ON g.file_path = ?
WHERE r.task_type = 'refactor'
  AND r.file_profile = g.stability
ORDER BY r.success_count DESC, r.avg_duration ASC
LIMIT 1;
```

One query, two tables, complete routing decision. No graph traversal library needed — relational JOINs on well-indexed tables serve the same purpose at this scale.

## Dependency Graph

```
Service 1: Core + PantryDB + First Kitchen
  MW-1 (CLI skeleton) → MW-2 (kitchen interface) → MW-3 (claude adapter)
  → MW-4 (PantryDB + migrations) → MW-5 (LedgerStore) → MW-6 (keyword router)
  → MW-7 (hook chain skeleton — SessionStart/SessionEnd)
  → Palate Cleanser 1

Service 2: Hook Chain + Circuit Breaker + Dispatch Modes
  MW-8 (PreRoute/PostRoute hooks) → MW-9 (PreDispatch/PostDispatch hooks)
  → MW-10 (circuit breaker / mode reader) → MW-11 (recovery strategies)
  → MW-12 (async dispatch + TicketStore) → MW-13 (detached dispatch)
  → MW-14 (QuotaStore + enforcer)
  → Palate Cleanser 2

Service 3: Pantry Intelligence + Sommelier
  MW-15 (MCP client) → MW-16 (MemPalace integration) → MW-17 (CodeGraph integration)
  → MW-18 (GitGraphStore) → MW-19 (QualityStore) → MW-20 (DepStore)
  → MW-21 (enriched routing / pantry signals)
  → MW-22 (RoutingStore + learned routing)
  → MW-23 (skill catalog awareness)
  → Palate Cleanser 3

Service 4: All Kitchens + Recipes + Feedback
  MW-24 (opencode adapter) → MW-25 (gemini adapter)
  → MW-26 (aider adapter) → MW-27 (goose adapter) → MW-28 (cline adapter)
  → MW-29 (recipe engine) → MW-30 (context handoff)
  → MW-31 (tiered-CLI feedback loop + report)
  → Palate Cleanser 4

Service 5: TUI + Neovim Plugin
  MW-32 (Bubble Tea app) → MW-33 (input component) → MW-34 (output viewport)
  → MW-35 (ledger panel) → MW-36 (process map) → MW-37 (kitchen selector)
  → MW-38 (milliways.nvim plugin)
  → Palate Cleanser 5

Service 6: Full Pantry + Carte Integration + Knowledge Graph
  MW-39 (carte.md parser) → MW-40 (opsx:apply integration)
  → MW-41 (knowledge graph queries — 4 layers)
  → MW-42 (routing accuracy measurement)
  → MW-43 (tiered report + lift metric)
  → Grand Finale
```
