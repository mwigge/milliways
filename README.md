# Milliways

> The Restaurant at the End of the Universe — one CLI to route them all.

Milliways is a terminal-first AI shell. It routes each prompt to the right AI tool — Claude, Codex, MiniMax, Copilot — through a single interface with persistent sessions, context injection, sleep/wake awareness, and a live status bar.

It calls the CLIs and APIs you already have set up. It does not manage credentials or run models itself.

---

## Install

### One-liner (macOS / Linux)

```bash
curl -sSf https://raw.githubusercontent.com/mwigge/milliways/master/install.sh | sh
```

This downloads pre-built binaries (`milliways`, `milliwaysd`, `milliwaysctl`) for your platform, adds `~/.local/bin` to your shell profile, and on macOS installs **MilliWays.app** to `/Applications`.

### From source

```bash
git clone https://github.com/mwigge/milliways.git
cd milliways
./install.sh          # builds from the checkout, no network needed
```

Requires Go 1.22+.

### Go install (CLI only)

```bash
go install github.com/mwigge/milliways/cmd/milliways@latest
```

---

## MilliWays.app — Native Terminal (macOS)

MilliWays.app is a native macOS terminal built on a patched wezterm. Every new tab opens the milliways REPL instead of a plain shell. The status bar shows your active agent, working directory, and a live wake badge when the laptop resumes from sleep.

```
[⚡ woke 3m ago] [≈≈ MW v0.4.8] [~/project] [●claude] [1:C 2:X 3:G 4:M 5:L]
```

### Leader keybindings (`Ctrl+Space`)

| Key | Action |
|-----|--------|
| `a` | Open milliways pane split below |
| `1` / `2` / `3` / `4` | Switch to claude / codex / copilot / minimax |
| `r` | Resume modal — shows wake summary, re-opens last agent |
| `k` | Cockpit context overlay |
| `w` | Observability render overlay |
| `z` | Plain shell tab (escape hatch) |

### Sleep/wake awareness

When the laptop wakes from sleep, the status bar shows an orange **⚡ woke Xm ago** badge for 5 minutes. Press `Ctrl+Space r` to see a resume modal and optionally reopen the last agent session.

---

## REPL

Start the REPL with `milliways` (default when no arguments are given).

```text
milliways 0.4.8
  REPL  |  type /help for commands
  runners: minimax | copilot | claude | codex

▶ /switch claude
Switched to claude

▶ explain the auth flow
[claude] ...streaming...
✓ claude  3.2s

 claude | mo:3 | 1.2k↑ 0.8k↓ | $0.02              ← persistent status bar
```

Sessions are auto-saved per working directory and restored on the next `milliways` launch. Context fragments expand inline before dispatch: `@file`, `@git`, `@branch`, `@shell`.

### REPL commands

**Routing**

| Command | Description |
|---------|-------------|
| `/switch <runner>` | Switch to a runner |
| `/claude` | Switch to claude (shorthand) |
| `/codex` | Switch to codex (shorthand) |
| `/minimax` | Switch to minimax (shorthand) |
| `/copilot` | Switch to copilot (shorthand) |
| `/stick` | Keep current runner until released |
| `/back` | Undo the most recent switch |
| `/model` | List models for the current runner |
| `/model <id>` | Set model for the current runner |

**Session**

| Command | Description |
|---------|-------------|
| `/session [name]` | Show or name the session |
| `/history` | Show command history (`!N` re-run, `!!` last, `ctrl-r` search) |
| `/summary` | Show session statistics |
| `/cost` | Show session cost |
| `/limit` | Show runner quotas |
| `/openspec` | Show current OpenSpec change |
| `/repo` | Show current git repository |

**OpenSpec**

| Command | Description |
|---------|-------------|
| `/opsx:list` | List active changes |
| `/opsx:status [name]` | Show task completion |
| `/opsx:show <name>` | Show change detail |
| `/opsx:apply <name>` | Fetch instructions and dispatch to current runner |
| `/opsx:explore [name]` | Investigate without implementing |
| `/opsx:archive <name>` | Archive a completed change |
| `/opsx:validate <name>` | Validate a change |

**Artifacts**

| Command | Description |
|---------|-------------|
| `/apply` | Write fenced code blocks from the last response to disk |
| `/image <path>` | Attach an image to the next prompt |
| `/image list` | List pending images |
| `/image clear` | Clear pending images |
| `/pptx <topic>` | Generate a PowerPoint presentation (saved to cwd) |
| `/drawio <topic>` | Generate a draw.io diagram XML (saved to cwd) |

**Claude**

| Command | Description |
|---------|-------------|
| `/claude-reasoning [off\|summary\|verbose]` | Set progress detail (default: verbose) |
| `/claude-model <model>` | Override model |

**MiniMax**

| Command | Description |
|---------|-------------|
| `/minimax-reasoning [off\|summary\|verbose]` | Set progress detail |
| `/minimax-model` | List all models (chat / image / music / lyrics) |
| `/minimax-model <model>` | Set model — routes to the correct endpoint automatically |

**Codex**

| Command | Description |
|---------|-------------|
| `/codex-reasoning [off\|summary\|verbose]` | Set progress detail |
| `/codex-model <model>` | Override model |
| `/codex-profile <name>` | Set config profile |
| `/codex-sandbox <mode>` | Set sandbox policy |
| `/codex-approval <mode>` | Set approval policy |
| `/codex-search <on\|off>` | Toggle web search |
| `/codex-image add\|clear\|list [path]` | Attach images |
| `/codex-review [args]` | Run codex review (default: `--uncommitted`) |
| `/codex-resume [args]` | Resume Codex session |
| `/codex-fork [args]` | Fork Codex session |
| `/codex-cloud [args]` | Codex Cloud command |
| `/codex-apply <task-id>` | Apply a Codex task diff |
| `/codex-mcp [args]` | Manage Codex MCP servers |
| `/codex-features [args]` | Inspect Codex features |

**Observability**

| Command | Description |
|---------|-------------|
| `/metrics` | Per-runner cost and token usage |
| `/logs [N]` | Last N log entries (default 50) |
| `/events` | Conversation events this session |

**Auth / system**

| Command | Description |
|---------|-------------|
| `/login` | Login to current runner |
| `/logout` | Logout from current runner |
| `/auth` | Show auth status for all runners |
| `/review-all [branch]` | Review branch across all authenticated runners |
| `/help` | Show all commands |
| `/exit` | Exit the REPL |
| `!<cmd>` | Run a shell command |

---

## CLI mode

For one-off requests without the REPL:

```bash
milliways "explain the auth flow"            # route to best kitchen
milliways --kitchen opencode "refactor"      # force a specific kitchen
milliways --explain "refactor store.py"      # see routing decision without executing
milliways --verbose "design JWT middleware"  # show sommelier reasoning
milliways --json "explain this"              # JSON output for scripting
milliways --async "long-running job"         # async dispatch, returns ticket ID
milliways status                             # check kitchen availability
milliways login claude                       # authenticate to a kitchen
milliways login --list                       # show all auth status
milliways report                             # routing statistics
milliways report --tiered                    # tiered-CLI performance analysis
milliways --recipe <name> "prompt"           # run a named recipe
```

---

## Runners

**REPL runners** (used with `/switch`):

| Runner | Best At | Cost |
|--------|---------|------|
| claude | Thinking, planning, code review | Cloud |
| codex | Agentic coding, tool use | Cloud |
| minimax | Reasoning, image/music/lyrics generation | Cloud |
| copilot | GitHub Copilot chat | Subscription |

**CLI kitchens** (routed by the sommelier in headless mode):

| Kitchen | CLI | Best At | Cost |
|---------|-----|---------|------|
| claude | `claude` | Thinking, planning, review | Cloud |
| opencode | `opencode run` | Coding, testing | Local ($0) |
| gemini | `gemini` | Research, search | Free |
| aider | `aider --message` | Multi-file editing | Cloud/Local |
| goose | `goose` | MCP tools, databases | Local |
| cline | `cline -y --json` | Parallel fleet | Cloud |

---

## Configuration

`~/.config/milliways/carte.yaml`:

```yaml
kitchens:
  claude:
    cmd: claude
    args: ["-p"]
    stations: [think, plan, review, explore]
    cost_tier: cloud
    daily_limit: 50
    five_hour_limit: 15   # rolling 5-hour window
    weekly_limit: 200

  minimax:
    http_client:
      base_url: https://api.minimax.io/v1
      auth_key: MINIMAX_API_KEY   # env var name or literal key
      model: MiniMax-M2.7
    daily_limit: 100

routing:
  keywords:
    explain: claude
    code: opencode
    search: gemini
  default: claude
  budget_fallback: opencode
```

Without a config file, milliways uses sensible defaults for all kitchens.

---

## How routing works

The **sommelier** picks the kitchen using three tiers:

1. **Keywords** — longest keyword match in the prompt. Deterministic.
2. **Pantry signals** — file churn (GitGraph), complexity (QualityGraph), risk score. HIGH-risk prompts always land in claude.
3. **Learned history** — after 5+ data points, learned success rates override keywords.

```bash
$ milliways --explain --verbose "refactor store.py"
[sommelier] learned: claude succeeded 94% for refactor
Kitchen: claude  Tier: learned  Risk: high
```

---

## Project memory (MemPalace + CodeGraph)

```bash
export MILLIWAYS_MEMPALACE_MCP_CMD="python3 -m mempalace.mcp_server"
export MILLIWAYS_MEMPALACE_MCP_ARGS="--palace /path/to/.mempalace"
export MILLIWAYS_CODEGRAPH_MCP_CMD="codegraph"
export MILLIWAYS_CODEGRAPH_MCP_ARGS="mcp"
```

Milliways injects relevant memories and code context before each dispatch when these are set.

---

## Neovim plugin

```lua
-- lazy.nvim
{
  "mwigge/milliways",
  config = function()
    require("milliways").setup({
      bin = "milliways",
      keybindings = true,
      leader = "<leader>m",
    })
  end,
}
```

Commands: `:Milliways`, `:MilliwaysExplain`, `:MilliwaysKitchen`, `:MilliwaysStatus`

Keybindings: `<leader>mm` dispatch, `<leader>me` explain, `<leader>ms` status, `<leader>mk` kitchen picker

---

## Data storage

`~/.config/milliways/milliways.db` (SQLite, ~2 MB):

| Table | Contents |
|-------|----------|
| `mw_ledger` | Every dispatch: kitchen, duration, outcome |
| `mw_routing` | Learned kitchen preferences |
| `mw_quotas` | Daily usage per kitchen |
| `mw_gitgraph` | File churn and stability |
| `mw_tickets` | Async dispatch tracking |

Human-readable audit trail: `~/.config/milliways/ledger.ndjson`

---

## License

Private repository. Not yet licensed for distribution.
