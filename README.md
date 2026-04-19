# Milliways

> The Restaurant at the End of the Universe — one CLI to route them all.

Milliways doesn't cook. It seats you at the right table and brings the best dish from whichever kitchen excels at making it.

```
$ milliways "explain the auth flow"       → routes to claude
$ milliways "code a rate limiter"         → routes to opencode (local, $0)
$ milliways "search for DORA regulations" → routes to gemini (free)
$ milliways --kitchen aider "refactor"    → forces aider
```

## Install

### Quick install (macOS/Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/mwigge/milliways/master/install.sh | sh
```

### From source

```bash
git clone git@github.com:mwigge/milliways.git
cd milliways
go build -o ~/.local/bin/milliways ./cmd/milliways/
```

### Go install

```bash
go install github.com/mwigge/milliways/cmd/milliways@latest
```

## How It Works

```
You type a task
     │
     ▼
┌─────────────┐
│  Sommelier  │  Three-tier routing:
│  (router)   │  1. Keyword match
│             │  2. Pantry signals (churn, complexity, coverage)
│             │  3. Learned history (which kitchen succeeded before)
└──────┬──────┘
       │
       ▼
┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│   claude    │  │  opencode   │  │   gemini    │
│  (cloud)    │  │  (local,$0) │  │   (free)    │
│  thinking   │  │  coding     │  │  searching  │
└─────────────┘  └─────────────┘  └─────────────┘
```

Each kitchen is a CLI tool you've already logged into. Milliways calls the binary — it never touches API keys or credentials.

## Kitchens

| Kitchen | CLI | Best At | Cost |
|---------|-----|---------|------|
| claude | `claude -p` | Thinking, planning, review | Cloud |
| opencode | `opencode run` | Coding, testing, refactoring | Local ($0) |
| gemini | `gemini` | Research, search, comparison | Free |
| aider | `aider --message` | Multi-file editing, git commits | Cloud/Local |
| goose | `goose` | MCP tools, databases, APIs | Local |
| cline | `cline -y --json` | Parallel fleet execution | Cloud |

## Commands

```bash
# Route a task to the best kitchen
milliways "explain the auth flow"

# Force a specific kitchen
milliways --kitchen opencode "add rate limiting"

# See routing decision without executing
milliways --explain "refactor store.py"

# Verbose: show sommelier reasoning
milliways --verbose "design JWT middleware"

# JSON output for scripting
milliways --json "explain this"

# Check which kitchens are available
milliways status

# Install and set up a kitchen
milliways setup gemini

# View routing statistics
milliways report

# View tiered-CLI performance analysis
milliways report --tiered
```

## Kitchen Switching

You can switch kitchens mid-conversation without losing the thread. Milliways carries conversation state forward in continuation payloads, so the next kitchen picks up with the existing context instead of starting over.

Every switch is reversible with `/back`, and sticky mode lets you temporarily opt out of automatic rerouting when you want to stay with the current kitchen.

- `/switch <kitchen>` — move the current conversation to a different kitchen.
- `/back` — undo the most recent switch and return to the previous kitchen.
- `/stick` — toggle sticky mode to prevent automatic kitchen switching.
- `/kitchens` — list available kitchens and show their current status.
- `--switch-to <kitchen>` — headless CLI flag to continue in a specific kitchen.

## Configuration

Milliways reads `~/.config/milliways/carte.yaml`:

```yaml
kitchens:
  claude:
    cmd: claude
    args: ["-p"]
    stations: [think, plan, review, explore]
    cost_tier: cloud

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
    enabled: false  # disable a kitchen

routing:
  keywords:
    explain: claude
    plan: claude
    review: claude
    code: opencode
    implement: opencode
    refactor: aider
    search: gemini
  default: claude
  budget_fallback: opencode
```

Without a config file, Milliways uses sensible defaults for all six kitchens.

## Intelligent Routing

The sommelier uses three tiers to pick the right kitchen:

**Tier 1 — Keywords**: Scans your prompt for keywords. Longest match wins. Deterministic.

**Tier 2 — Pantry signals**: Consults knowledge about the files involved:
- GitGraph: file churn, stability, last author
- QualityGraph: cyclomatic complexity, test coverage
- Risk scoring: HIGH risk overrides keyword routing → routes to claude for safety

**Tier 3 — Learned history**: After enough dispatches, learns which kitchen succeeds at which task type. Overrides keywords when data is sufficient (5+ data points).

```bash
$ milliways --explain --verbose "refactor store.py"
[mode] private
[pantry] learned: claude@94% for task_type=refactor
[sommelier] learned: claude succeeded 94% for refactor (stability=volatile churn90d=18 complexity=34)
Kitchen: claude
Reason:  learned: claude succeeded 94% for this task type
Tier:    learned
Risk:    high
```

## Project Memory (CodeGraph + MemPalace)

Milliways can optionally use CodeGraph (code structure search) and MemPalace (project memory) to inject relevant context before routing.

### Setup

**MemPalace** — project-specific memory store:

```bash
# Install mempalace CLI
pip install mempalace

# Initialize a palace in your project (creates .mempalace/)
cd ~/dev/src/projects/myproject
mempalace init .mempalace

# Mine project files into the palace
mempalace mine .

# Search your palace
mempalace search "why did we switch to GraphQL"
```

**CodeGraph** — semantic code search (optional):

```bash
# Install codegraph CLI
npm install -g @opencode/codegraph

# Initialize in your project
cd ~/dev/src/projects/myproject
codegraph init
```

### Environment Variables

When MemPalace and/or CodeGraph are available in your project, set the MCP server commands:

```bash
export MILLIWAYS_MEMPALACE_MCP_CMD="python3 -m mempalace.mcp_server"
export MILLIWAYS_MEMPALACE_MCP_ARGS="--palace /path/to/project/.mempalace"
export MILLIWAYS_CODEGRAPH_MCP_CMD="codegraph"
export MILLIWAYS_CODEGRAPH_MCP_ARGS="mcp"
```

Or put them in your shell profile (`~/.zshrc`, `~/.bashrc`) for persistence.

### How It Works

With project memory enabled:
1. Milliways detects `.mempalace/` and `.codegraph/` in your repo root
2. Startup outside a git repo works normally; startup inside a repo without a palace degrades gracefully
3. If CodeGraph is still being created, the TUI shows `indexing...`
4. If no palace exists yet, the TUI shows `(none — run /palace init)`
5. On each turn, relevant memories are injected into the context bundle
6. Citations to project facts are tracked per-turn and stored with the conversation
7. `/project`, `/repos`, `/palace`, `/codegraph` commands show project state

Without these directories, milliways operates without project context (graceful degradation).

### Project registry: `~/.milliways/projects.yaml`

Use the optional registry to control cross-palace read/write access:

```yaml
projects:
  default:
    access:
      read: all
      write: project

  shared-libs:
    paths:
      - ~/dev/src/pprojects/shared-lib
      - ~/dev/src/pprojects/design-system
    access:
      read: all
      write: none

  client-work:
    paths:
      - ~/dev/src/pprojects/client-a
    access:
      read: project
      write: project
```

Schema:

- `projects.<name>.paths`: repo roots matched against palace paths
- `projects.<name>.access.read`: `all`, `project`, or `none`
- `projects.<name>.access.write`: `project` or `none`
- `projects.default.access`: fallback rules when no explicit project matches

### Project commands

Inside the TUI:

- `/project` — show active repo, CodeGraph, palace, and access rules
- `/repos` — list repos accessed in the current session
- `/palace` — show palace status
- `/palace init` — reserved for palace bootstrap wiring
- `/palace search <query>` — reserved for palace search wiring
- `/codegraph` or `/codegraph status` — show CodeGraph status
- `/codegraph reindex` — reserved for reindex wiring
- `/codegraph search <query>` — reserved for CodeGraph search wiring

## Circuit Breaker

Milliways respects the company/private mode from `~/.claude/mode`:

- **Company mode**: Only approved kitchens, only company paths writable
- **Private mode**: All kitchens available, only private paths writable
- **Neutral paths**: `~/.claude/`, `~/.config/`, `ai_local/` always accessible

## Data Storage

All state in a single SQLite file: `~/.config/milliways/milliways.db` (~2 MB).

| Table | What It Stores |
|-------|---------------|
| mw_ledger | Every dispatch: kitchen, duration, outcome, task type |
| mw_routing | Learned preferences: which kitchen succeeds at what |
| mw_quotas | Daily usage per kitchen |
| mw_gitgraph | File churn, blame, stability |
| mw_quality | Cyclomatic complexity, test coverage |
| mw_deps | Package versions, CVEs |
| mw_tickets | Async/detached dispatch tracking |

Plus `~/.config/milliways/ledger.ndjson` as a human-readable audit trail (for `jq`).

## Architecture

Milliways is ~8 MB in memory. It never loads models, never stores credentials, never runs in the background. It spawns a kitchen CLI, streams the output, logs the result, and exits.

```
milliways (Go binary, ~8 MB)
  ├── sommelier (3-tier routing)
  ├── pantry (SQLite + MCP clients for MemPalace/CodeGraph)
  ├── kitchen adapters (exec.Command per CLI tool)
  └── ledger (ndjson + SQLite dual write)
```

## License

Private repository. Not yet licensed for distribution.
