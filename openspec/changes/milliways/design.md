# Design — Milliways

## Architecture

```
$ milliways "refactor auth middleware to use JWT"
       │
       ▼
┌──────────────────────────────────────────────────────────────┐
│                      MAÎTRE D' (Go binary)                    │
│                                                                │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐    │
│  │ CLI      │  │Sommelier │  │ Recipe   │  │ Ledger   │    │
│  │ Parser   │→ │ (router) │→ │ Engine   │→ │ Writer   │    │
│  └──────────┘  └────┬─────┘  └──────────┘  └──────────┘    │
│                      │                                        │
│              ┌───────▼────────┐                               │
│              │  PANTRY CLIENT │                               │
│              │  (MCP + SQLite)│                               │
│              └───────┬────────┘                               │
│                      │                                        │
│         ┌────────────┼────────────┬────────────┐             │
│         ▼            ▼            ▼            ▼             │
│    MemPalace    CodeGraph     GitGraph    QualityGraph       │
│    (MCP)        (MCP)        (SQLite)    (SQLite)           │
└──────────────────────────────────────────────────────────────┘
       │
       │  exec.Command() per kitchen
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
```

## Module Structure

```
milliways/
├── cmd/
│   └── milliways/
│       └── main.go              # CLI entry point (cobra)
├── internal/
│   ├── maitre/                  # Maître d' — top-level orchestrator
│   │   ├── maitre.go            # Dispatch loop
│   │   └── config.go            # Load carte.yaml
│   │
│   ├── sommelier/               # Routing intelligence
│   │   ├── sommelier.go         # Route(task) → kitchen
│   │   ├── keywords.go          # Keyword-based routing (fast path)
│   │   ├── pantry_signals.go    # Consult knowledge graphs
│   │   └── learned.go           # Historical success-based routing
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
│   ├── pantry/                  # Shared knowledge clients
│   │   ├── mempalace.go         # MCP client for MemPalace
│   │   ├── codegraph.go         # MCP client for CodeGraph
│   │   ├── gitgraph.go          # Direct SQLite for GitGraph
│   │   ├── qualitygraph.go      # Direct SQLite for QualityGraph
│   │   ├── depgraph.go          # Direct SQLite for DepGraph
│   │   └── topology.go          # Direct SQLite for TopologyGraph
│   │
│   ├── recipe/                  # Multi-course workflows
│   │   ├── engine.go            # Execute recipe steps
│   │   ├── context.go           # Context handoff between courses
│   │   └── builtin.go           # Built-in recipes
│   │
│   ├── ledger/                  # Routing feedback log
│   │   ├── writer.go            # Append ndjson
│   │   └── reader.go            # Query for learned routing
│   │
│   └── tui/                     # Bubble Tea interactive mode
│       ├── app.go               # Main TUI model
│       ├── input.go             # Task input component
│       ├── output.go            # Streaming output viewport
│       ├── ledger_panel.go      # Live ledger display
│       └── styles.go            # Lipgloss theme
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

Milliways is **not a proxy and not a gateway**. It's a maître d' that seats you at the right table — it doesn't cook, and it doesn't pay the bill.

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

### D3: Sommelier uses three-tier routing (fast → enriched → learned)

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

Why: Pipes (stdout→stdin) lose structure. Temp files are inspectable, debuggable, and survive kitchen crashes. Cleaned up after recipe completes (or `--keep-context` to preserve).

### D5: Pantry knowledge graphs are SQLite, not a graph DB

**Chosen**: SQLite per graph (gitgraph.db, qualitygraph.db, depgraph.db, routing-ledger.db)

Why:
- Zero external dependencies (no Neo4j, no Postgres)
- Fast enough for single-developer use (< 10ms queries)
- SQLite is already used by CodeGraph, TaskQueue, and MemPalace KG
- Portable (copy the file, done)
- `go-sqlite3` is battle-tested

### D6: Ledger is append-only ndjson + SQLite index

**Chosen**: Dual write

```
~/.config/milliways/ledger.ndjson    (append-only, human-readable, jq-friendly)
~/.config/milliways/ledger.db        (SQLite index for learned routing queries)
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
  gitgraph:
    type: sqlite
    path: ~/.config/milliways/gitgraph.db
  qualitygraph:
    type: sqlite
    path: ~/.config/milliways/qualitygraph.db
  depgraph:
    type: sqlite
    path: ~/.config/milliways/depgraph.db

ledger:
  ndjson: ~/.config/milliways/ledger.ndjson
  db: ~/.config/milliways/ledger.db

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
| aider | `pip install aider-chat` | Prompt for API key → write to `~/.aider.conf.yml` | API key in config |
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
- If only `opencode` is available → everything routes there (single-kitchen mode)
- If `claude` + `opencode` → classic two-tier (think cloud, code local)
- If `claude` + `opencode` + `gemini` → add free research capability
- Full menu → sommelier has maximum routing flexibility

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

  Pantry: MemPalace ✓ | CodeGraph ✓ | GitGraph ✓ | QualityGraph ○
  Ledger: 142 entries | Last: 3m ago
  
  3/6 kitchens ready. Run 'milliways --setup <kitchen>' to add more.
```

### D12: 24 GB RAM constraint — only one kitchen active at a time

Default behavior: sequential execution. Only one kitchen subprocess at a time. When a recipe has multiple courses, each completes before the next starts.

Exception: `--parallel` flag for independent courses (e.g., gemini research + opencode code on different files). But parallel requires both kitchens to be non-overlapping on Ollama (one cloud + one local is fine; two local models would swap).

### D12: Live Process Map in TUI (top-right corner)

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

## Dependency Graph

```
Service 1: Core + First Kitchen
  MW-1 (CLI skeleton) → MW-2 (kitchen interface) → MW-3 (claude adapter)
  → MW-4 (ledger) → MW-5 (keyword router)
  → 🍋 Palate Cleanser 1

Service 2: Pantry + Sommelier
  MW-6 (MCP client) → MW-7 (MemPalace integration) → MW-8 (CodeGraph integration)
  → MW-9 (GitGraph) → MW-10 (QualityGraph)
  → MW-11 (enriched routing) → MW-12 (learned routing)
  → 🍋 Palate Cleanser 2

Service 3: All Kitchens + Recipes
  MW-13 (opencode adapter) → MW-14 (gemini adapter)
  → MW-15 (aider adapter) → MW-16 (goose adapter) → MW-17 (cline adapter)
  → MW-18 (recipe engine) → MW-19 (context handoff)
  → 🍋 Palate Cleanser 3

Service 4: TUI
  MW-20 (Bubble Tea app) → MW-21 (input component) → MW-22 (output viewport)
  → MW-23 (ledger panel) → MW-24 (kitchen selector)
  → 🍋 Palate Cleanser 4

Service 5: Full Pantry + Carte Integration
  MW-25 (DepGraph) → MW-26 (TopologyGraph)
  → MW-27 (carte.md parser) → MW-28 (opsx:apply integration)
  → MW-29 (routing accuracy measurement)
  → 🍋 Grand Finale
```
