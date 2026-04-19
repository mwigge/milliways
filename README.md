# Milliways

> The Restaurant at the End of the Universe — one CLI to route them all.

Milliways is a terminal-first AI routing layer for coding, research, and review workflows. Start the TUI with `milliways --tui`, type prompts, and let milliways route each request to the right installed kitchen while keeping a full ledger of what ran.

It does not run models itself or manage credentials. It sits in front of the tools you already use and orchestrates them through one interface.

## Install

### From source

```bash
git clone git@github.com:mwigge/milliways.git
cd milliways
go build -o ~/.local/bin/milliways ./cmd/milliways/
```

Verify it worked with `milliways --version` or `milliways status`.

### Go install

```bash
go install github.com/mwigge/milliways/cmd/milliways@latest
```

Requires: Go 1.21+

## TUI Mode

Start the terminal UI with `milliways --tui`.

The TUI is the primary milliways experience: a live workspace for routing prompts, watching streamed output, switching kitchens, and inspecting the ledger without leaving the terminal. Use it when you want an ongoing session instead of isolated one-off commands.

The left side shows the currently focused dispatch in full. The right side shows recent blocks and a lower panel that can swap between different views.

### How the TUI works

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ milliways --tui                                                              │
│ repo • branch • palace/codegraph • kitchen status                            │
├──────────────────────────────────────────────┬───────────────────────────────┤
│ Block Stack / Focused Dispatch               │ Block List                    │
│ (main output)                                │ [1 ●] [2 ◐] [3 ✓] [4 ✗]      │
│                                              ├───────────────────────────────┤
│ • prompt                                     │ Swappable panel               │
│ • selected kitchen                           │ ctrl+] / ctrl+[ cycles:       │
│ • streamed output                            │  - Ledger                     │
│ • runtime events                             │  - Jobs                       │
│ • inline confirms/questions                  │  - Cost                       │
│                                              │  - Routing                    │
│                                              │  - System                     │
│                                              │  - Snippets                   │
│                                              │  - Diff                       │
│                                              │  - Compare                    │
├──────────────────────────────────────────────┴───────────────────────────────┤
│ > your prompt here                                                          │
└──────────────────────────────────────────────────────────────────────────────┘
```

And here is the same idea as a more concrete screen sketch:

```text
┌──────────────────────────────────────────────────────┐
│  milliways --tui                                     │
│                                                      │
│  ┌────────────────────┐  ┌─────────────────┐         │
│  │  Block Stack       │  │ Block List      │         │
│  │  (main output)     │  │ [k1 ●] [k2 ◐]   │         │
│  │                    │  ├─────────────────┤         │
│  │                    │  │ Ledger          │ ← panel │
│  │                    │  │ 14:32 claude ✓  │   cycle │
│  └────────────────────┘  └─────────────────┘  ctrl+] │
│                          ┌─────────────────┐         │
│                          │ > your prompt   │         │
│                          └─────────────────┘         │
└──────────────────────────────────────────────────────┘
```

Approximate layout (your terminal size changes what fits):

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│ Milliways                                                                   │
│ repo • branch • palace/codegraph status • kitchen availability              │
├───────────────────────────────────────────────┬─────────────────────────────┤
│                                               │ Blocks                      │
│  Focused dispatch                             │ >⠿ add rate limiting  18s  │
│  ─────────────────────────                    │  ✓ fix tests            4s   │
│  Prompt, kitchen, timing, sticky state        │                             │
│  Streaming provider output                    │ Ledger                      │
│  Runtime events and system lines              │ 15:04 [claude] 3.2s ✓       │
│  Questions / confirms inline                  │ 14:58 [gemini] 1.1s ✗       │
│                                               │                             │
│                                               │ Activity                    │
│                                               │ 15:04:05 switch: ...        │
│                                               │                             │
│                                               │ Jobs                        │
│                                               │ milliways                   │
├───────────────────────────────────────────────┴─────────────────────────────┤
│ ▶ Type a task... (@kitchen to force, Ctrl+D to exit)                        │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Enter` | Dispatch the current prompt |
| `Ctrl+D` | Exit the TUI |
| `Ctrl+C` | Cancel the focused active block, or quit if nothing is running |
| `/` | Open the command palette |
| `Ctrl+R` | Fuzzy search completed dispatch history |
| `Ctrl+I` | Inject extra context into the focused streaming block |
| `Ctrl+F` | Rate the last completed dispatch |
| `Ctrl+S` | Show a session summary |
| `Ctrl+G` | Toggle rendered/raw output mode |
| `Ctrl+]` | Cycle to the next sidebar panel |
| `Ctrl+[` | Cycle to the previous sidebar panel |
| `Tab` | Cycle focus across blocks |
| `1`-`9` | Jump to a specific block |
| `c` | Collapse or expand the focused block |
| `PgUp` / `PgDn` | Scroll the focused block |
| `Esc` | Close the active overlay |

### TUI Panels

**Focused dispatch (left)** — the main viewport shows the selected block in full:
- Prompt, kitchen badge, elapsed time, and state
- Streaming provider output and code blocks
- Runtime/system events such as routing, switching, and injected context
- Inline questions and confirmations from the active kitchen
- Completed blocks auto-collapse when there are multiple active entries

**Blocks (top-right)** — a compact list of recent blocks:
- Focus marker (`>`) for the selected block
- State icons for routing, streaming, success, failure, and cancellation
- Prompt preview and elapsed time
- Queue preview when max concurrency is exceeded

**Swappable lower-right panel** — the bottom of the sidebar rotates through eight views:
- **Ledger** — recent completed dispatches
- **Jobs** — background work from milliways tickets
- **Cost** — usage and spend-oriented detail when available
- **Routing** — why the sommelier chose the current kitchen
- **System** — runtime/system status lines
- **Snippets** — captured snippets for quick reuse
- **Diff** — change preview for relevant outputs
- **Compare** — side-by-side comparison views

Cycle these panels with `Ctrl+]` and `Ctrl+[`.

**Ledger** — recent completed dispatches:
- Last 8 completed blocks, newest first
- Timestamp, kitchen badge, duration, and status icon

**Activity (inside Ledger)** — recent structured runtime activity:
- Switch events and other non-output runtime events for the focused conversation
- Truncated to the latest 6 events

**Jobs (inside Ledger)** — background work from milliways tickets:
- **milliways** tickets from `pantry.TicketStore` (`mw_tickets` in `~/.config/milliways/milliways.db`)
  - Shows status icon, truncated prompt, and kitchen
  - Polls every 5 seconds

**Project header / status bar (top)** — current repo plus kitchen availability:
- Active repo, branch, palace/codegraph state, and access rules
- Kitchen readiness and quota warnings inline

### Overlays

**Run In chooser** — opens when you press `Enter` without an `@kitchen` prefix:
- `Auto` lets Milliways route normally
- Kitchen-specific entries allow manual override
- Ready, warning, exhausted, needs-auth, disabled, and not-installed states are shown inline

**Command palette** — opens when you type `/` in the input box:
- `project`, `palace`, `codegraph`, `login`
- `switch`, `back`, `stick`, `kitchens`, `repos`
- `status`, `report`, `cancel`
- `collapse`, `expand`, `collapse all`, `expand all`
- `history`, `session save`, `session load`, `summary`

**History search** (`Ctrl+R`) — fuzzy search over completed blocks and prompt history.

**Feedback** (`Ctrl+F`) — rate the last completed dispatch as good, bad, or skipped.

**Session summary** (`Ctrl+S`) — totals by kitchen, duration, success count, and cost when available.

### TUI Commands

```bash
milliways --tui                    # Start the TUI
milliways --tui --kitchen claude  # Start the TUI with a kitchen forced
milliways --tui --resume          # Resume the last saved TUI session
milliways --tui --session demo    # Use a named TUI session
```

### Recipes

Recipes are multi-course meal plans defined in `~/.config/milliways/carte.yaml` and executed sequentially across kitchens.

For example, you can review a pull request in one kitchen and then hand the fix-up pass to another.

```yaml
recipes:
  review-pr:
    - station: review
      kitchen: claude
      prompt: "Review {{ .Prompt }} for security issues"
    - station: refactor
      kitchen: aider
      prompt: "Apply the suggested fixes"
```

Run one with `milliways --recipe review-pr "https://github.com/org/repo/pull/123"`.

### Async Dispatch

Dispatch without waiting for completion:

```bash
milliways --async "run the full test suite"
```

Async tickets appear in the Jobs panel in the TUI and can also be inspected from the CLI:

```bash
milliways tickets
milliways ticket <id>
```

`--detach` is reserved for detached execution, but right now it returns a not-yet-implemented error.

## Neovim Plugin

The Neovim plugin lives in [nvim-plugin/README.md](nvim-plugin/README.md).

```lua
-- lazy.nvim
{
  "mwigge/milliways",
  config = function()
    require("milliways").setup({
      bin = "milliways",       -- path to binary (must be on PATH)
      keybindings = true,      -- register default keybindings
      leader = "<leader>m",    -- keybinding prefix
      float_width = 0.8,       -- floating window dimensions
      float_height = 0.8,
    })
  end,
}
```

Requires: Neovim 0.10+, plus the `milliways` binary on your `PATH`.

Commands: `:Milliways`, `:MilliwaysExplain`, `:MilliwaysKitchen`, `:MilliwaysRecipe`, `:MilliwaysStatus`, `:MilliwaysSwitch`, `:MilliwaysStick`, `:MilliwaysBack`, `:MilliwaysKitchens`

Keybindings: `<leader>mm` dispatch, `<leader>me` explain, `<leader>ms` status, `<leader>mk` kitchen, `<leader>mK` telescope picker, `<leader>m.` reroute

Features: L2 context hydration (git branch, LSP diagnostics, cursor position, quickfix), visual selection as context, floating window output with yank support.

## CLI Mode

For quick one-off requests without the TUI, use CLI mode:

```bash
$ milliways "explain the auth flow"        → routes to claude
$ milliways "code a rate limiter"          → routes to opencode (local, $0)
$ milliways "search for DORA regulations"  → routes to gemini (free)
$ milliways --kitchen aider "refactor"     → forces aider
```

## How the Neovim plugin works

The Neovim plugin is a thin client. It collects editor context, sends a request to the milliways daemon or CLI transport, and streams the answer back into Neovim.

```text
┌─────────────────────────────────────────────────────────────┐
│  Neovim + milliways.nvim                                   │
│                                                            │
│  ┌──────────────┐      ┌──────────────────┐                │
│  │ :Milliways   │ ───▶ │ milliways client │                │
│  │ (command)    │      │ (Lua, in plugin) │                │
│  └──────────────┘      └────────┬─────────┘                │
│                                  │                         │
│                         stdin/stdout or IPC                │
│                                  │                         │
│                                  ▼                         │
│                         ┌──────────────────┐               │
│                         │ milliways daemon │ (always on)   │
│                         │                  │               │
│                         │  ┌────────────┐  │               │
│                         │  │ orchestrator│  │               │
│                         │  │ sommelier   │  │               │
│                         │  │ kitchen(s)  │  │               │
│                         │  └────────────┘  │               │
│                         └──────────────────┘               │
│                                    │                       │
│                           events stream                    │
│                                    ▼                       │
│                         ┌──────────────────┐               │
│                         │ TUI or --json    │               │
│                         │ output           │               │
│                         └──────────────────┘               │
└─────────────────────────────────────────────────────────────┘
```

### L2 context hydration

"L2 context hydration" means the plugin sends more than just the sentence you typed. It also bundles useful nearby code and repo details so the model sees the request in context.

```text
┌─────────────────────────────────────────────────────────────┐
│  L2 Context Hydration (what happens when you press Enter)  │
│                                                            │
│  1. Neovim → :Milliways "explain this function"           │
│  2. milliways.nvim collects:                               │
│     • file under cursor (via treesitter or buffer info)    │
│     • surrounding code (上下 30 lines)                     │
│     • repo context (project name, language)                │
│  3. All of the above bundled into the prompt               │
│  4. Sent to milliways daemon                               │
│  5. Response streamed back into a Neovim buffer            │
│                                                            │
│  The model sees your code IN CONTEXT,                      │
│  not just the function name.                               │
└─────────────────────────────────────────────────────────────┘
```

Tiny example: if your cursor is on `validateToken()`, the plugin can send that function, the surrounding 30 lines, the file path, and the current repo language along with your prompt. That gives the model enough context to explain the actual code instead of guessing from the function name alone.

## How request routing works

Milliways uses a router called the **sommelier**. Its job is simple: inspect the request, check the project context, and pick the kitchen most likely to do well.

```text
You type a prompt
       │
       ▼
┌──────────────────┐
│ sommelier        │  (the router)
│ "which kitchen   │
│  should handle   │
│  this?"         │
└────────┬─────────┘
         │
    inspects:
    • keywords ("fix bug" → claude-ha)
    • file types (.py → python-kitchen)
    • repo context
    • load balancing
         │
         ▼
┌──────────────────┐
│ Decision:        │
│  kitchen: claude │
│  tier: keyword   │
│  reason: found   │
│  'pytest' in     │
│  prompt          │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ orchestrator     │
│ dispatches to    │
│ selected kitchen │
└──────────────────┘
```

The router uses three tiers:

```text
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

In practice, that means something like this:
- `"explain this design"` usually lands in a thinking-oriented kitchen
- `"refactor these tests"` usually lands in a coding-oriented kitchen
- `"search docs for X"` usually lands in a research-oriented kitchen

Each kitchen is a CLI tool you've already logged into. Milliways calls the binary directly — it never touches API keys or credentials.

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

### CLI mode

```bash
milliways "explain the auth flow"            # Route a task to the best kitchen
milliways --kitchen opencode "refactor"      # Force a specific kitchen
milliways --explain "refactor store.py"      # See routing decision without executing
milliways --verbose "design JWT middleware"  # Show sommelier reasoning
milliways --json "explain this"              # JSON output for scripting
milliways --tui                               # Start the TUI
milliways --tui --kitchen claude              # TUI with kitchen forced
milliways --async "long-running job"         # Async dispatch, return a ticket ID
milliways --detach "long-running job"        # Reserved detached mode (currently not implemented)
milliways ticket <id>                         # Show one async/detached ticket
milliways tickets                             # List async/detached tickets
milliways status                              # Check which kitchens are available
milliways setup gemini                        # Install and set up a kitchen
milliways login --list                        # Show auth status for kitchens
milliways login claude                        # Authenticate to a kitchen
milliways report                              # View routing statistics
milliways report --tiered                     # View tiered-CLI performance analysis
milliways --recipe <name> "prompt"           # Run a named recipe
```

### TUI commands

```text
/project         Show active repo, CodeGraph, palace, and access rules
/palace          Show palace status
/codegraph       Show CodeGraph status
/login           Show kitchen auth status
/login <kitchen> Start kitchen login flow
/switch <kitchen> Move the current conversation to a different kitchen
/back            Return to the previous kitchen after a switch
/stick           Toggle sticky mode for the focused conversation
/kitchens        List kitchens and their current status
/repos           List repos accessed in this session
/status          Show kitchen availability
/report          Show routing statistics placeholder output
/cancel          Cancel the focused active block
/collapse        Collapse the focused block
/expand          Expand the focused block
/collapse all    Collapse all blocks
/expand all      Expand all blocks
/history         Open fuzzy history search
/session save    Save the current session
/session load    Load the last saved session
/summary         Show the session summary overlay
```

### Recipes

Recipes are named multi-course plans configured in `~/.config/milliways/carte.yaml`.

```yaml
recipes:
  review-pr:
    - station: review
      kitchen: claude
      prompt: "Review {{ .Prompt }} for security issues"
    - station: refactor
      kitchen: aider
      prompt: "Apply the suggested fixes"
```

Run them with `milliways --recipe review-pr "https://github.com/org/repo/pull/123"`.

### Quotas

Set daily limits per kitchen to control spend:

```yaml
quotas:
  claude:
    daily_limit: 50
  gemini:
    daily_limit: 200
```

When a quota is exhausted, Milliways falls back to the `budget_fallback` kitchen.

## Kitchen Switching

You can switch kitchens in the middle of a conversation without losing the thread. Milliways carries the conversation state forward in continuation payloads, so the next kitchen picks up with the current context instead of starting from zero.

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

**Tier 1 — Keywords**: Scan the prompt for keywords. Longest match wins. Deterministic.

**Tier 2 — Pantry signals**: Consult knowledge about the files involved:
- GitGraph: file churn, stability, last author
- QualityGraph: cyclomatic complexity, test coverage
- Risk scoring: HIGH risk overrides keyword routing and sends the request to claude for safety

**Tier 3 — Learned history**: After enough dispatches, learn which kitchen succeeds at which task type. This can override keywords when there is enough data (5+ data points).

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

Milliways can optionally use CodeGraph (semantic code structure search) and MemPalace (project memory) to inject relevant context before routing.

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

Or put them in your shell profile (`~/.zshrc`, `~/.bashrc`) if you want them available all the time.

Milliways kitchen parity requires the `mempalace-milliways` fork at commit `e5e705ea43bfab283fd9c16eedec1f5068d10f44` or later so the conversation MCP tools and checkpoint/resume schema are available.

### How It Works

With project memory enabled:
1. Milliways detects `.mempalace/` and `.codegraph/` in your repo root
2. Startup outside a git repo works normally; startup inside a repo without a palace degrades gracefully
3. If CodeGraph is still being created, the TUI shows `indexing...`
4. If no palace exists yet, the TUI shows `(none — run /palace init)`
5. On each turn, relevant memories are injected into the context bundle
6. Citations to project facts are tracked per-turn and stored with the conversation
7. `/project`, `/repos`, `/palace`, `/codegraph` commands show project state

Without these directories, milliways still works; it just runs without project context.

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

- **Company mode**: only approved kitchens, only company paths writable
- **Private mode**: all kitchens available, only private paths writable
- **Neutral paths**: `~/.claude/`, `~/.config/`, `ai_local/` always accessible

## Data Storage

All state lives in one SQLite file: `~/.config/milliways/milliways.db` (~2 MB).

| Table | What It Stores |
|-------|---------------|
| mw_ledger | Every dispatch: kitchen, duration, outcome, task type |
| mw_routing | Learned preferences: which kitchen succeeds at what |
| mw_quotas | Daily usage per kitchen |
| mw_gitgraph | File churn, blame, stability |
| mw_quality | Cyclomatic complexity, test coverage |
| mw_deps | Package versions, CVEs |
| mw_tickets | Async/detached dispatch tracking |

Related files:

- `~/.config/milliways/ledger.ndjson` — human-readable audit trail for dispatch history (`jq`-friendly)

## Architecture

Milliways is about 8 MB in memory. It does not load models, store credentials, or sit in the background doing hidden work. It spawns a kitchen CLI, streams output, logs the result, and exits.

```text
milliways (Go binary, ~8 MB)
  ├── sommelier (3-tier routing)
  ├── pantry (SQLite + MCP clients for MemPalace/CodeGraph)
  ├── kitchen adapters (exec.Command per CLI tool)
  └── ledger (ndjson + SQLite dual write)
```

## License

Private repository. Not yet licensed for distribution.
