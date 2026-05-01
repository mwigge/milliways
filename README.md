# Milliways

> The Restaurant at the End of the Universe — one CLI to route them all.

Milliways is an AI terminal for macOS and Linux — the restaurant at the end of the universe where Claude, Codex, Pool, Gemini, Copilot, and MiniMax all show up for dinner.

Open a tab and you're talking to an AI agent. Switch runners mid-session without losing context. Hit a quota limit and milliways rotates to the next one automatically, briefing it on exactly where things left off. Don't Panic — the full transcript is always on disk.

It wraps the CLIs and APIs you already have set up. It does not run models or manage credentials. Bring your own towel.

---

## Install

### One-liner (macOS / Linux)

```bash
curl -sSf https://raw.githubusercontent.com/mwigge/milliways/master/install.sh | sh
```

Downloads pre-built binaries (`milliways`, `milliwaysd`, `milliwaysctl`) for your platform, adds `~/.local/bin` to your shell profile, and on macOS installs **MilliWays.app** to `/Applications`.

**Installation binaries tested for:** Ubuntu 24.04 · Fedora 41 · Arch Linux · macOS (arm64 + amd64)

The installer has a source-build fallback: if no pre-built binary is available for your architecture, it clones the repo and compiles automatically (requires `git` and `go`).

### From source

```bash
git clone https://github.com/mwigge/milliways.git
cd milliways
./install.sh          # builds from the checkout, no network needed
```

### Go install (CLI only)

```bash
go install github.com/mwigge/milliways/cmd/milliways@latest
```

---

## MilliWays.app — Native Terminal (macOS)

MilliWays.app is a native macOS terminal built on a patched wezterm. Every new tab opens milliways instead of a plain shell. The status bar shows your active agent, working directory, and a live wake badge when the laptop resumes from sleep.

```
[⚡ woke 3m ago] [≈≈ MW v1.0.1] [~/project] [●claude] [1:C 2:X 3:G 4:M 5:L]
```

### Leader keybindings (`Ctrl+Space`)

| Key | Action |
|-----|--------|
| `a` | Open milliways pane split below |
| `1` / `2` / `3` / `4` | Switch to claude / codex / copilot / minimax |
| `r` | Resume modal — shows wake summary, re-opens last agent |
| `k` | Context overlay |
| `w` | Observability render overlay |
| `z` | Plain shell tab (escape hatch) |

### Sleep/wake awareness

When the laptop wakes from sleep, the status bar shows an orange **⚡ woke Xm ago** badge for 5 minutes. Press `Ctrl+Space r` to see a resume modal and optionally reopen the last agent session.

---

## AI Terminal

Start the AI terminal with `milliways` (default when no arguments are given). The launcher starts `milliwaysd` if needed, then execs `milliways-term`.

```text
milliways v1.0.1
  /login [client]  set up auth      /help  show all commands      /exit  quit
  /1 claude  /2 codex  /3 copilot  /4 minimax  /5 gemini  /6 local  /7 pool

▶ /claude
→ claude  model: claude-opus-4-5  (claude CLI)

[claude] ▶ explain the auth flow
...streaming...

  ($0.02 · 1.2k→0.8k tok)
```

Sessions are auto-saved per working directory and restored on the next `milliways` launch. Context fragments expand inline before dispatch: `@file`, `@git`, `@branch`, `@shell`.

### Commands

Tab completion is available for all commands. Type `/` and press Tab to see the full list; commands are filtered as you type.

**Routing**

| Command | Description |
|---------|-------------|
| `/claude` `/codex` `/copilot` `/minimax` `/gemini` `/local` `/pool` | Switch to a runner |
| `/1` … `/7` | Numeric shortcut for the runner list |
| `/switch <runner>` | Same as `/<runner>` |
| `/model` | Show active model and available choices (fetched live from the provider API) |
| `/model <name>` | Switch model for the active runner |
| `/agents` | Show all runners with auth status |

**Session context**

| Command | Description |
|---------|-------------|
| `/ring [r1,r2,...]` | Show or set the auto-rotation ring; `/ring off` disables |
| `/history` | Show the current turn log |
| `/cost` | Token usage per runner (last hour) |
| `/retry` | Re-send the last user prompt |
| `/undo` | Drop the last user + assistant turn pair |
| `/compact` | Summarise the session with the active runner, replace turn log with the summary |
| `/clear` | Wipe the local turn log |
| `/quota` | Raw quota snapshot from daemon |
| `/login [client]` | Auth setup — API key prompt or CLI steps |
| `/metrics` | Live rolling metrics table (1min / 1h / 24h / 7d / 30d) |

**Artifacts (all runners)**

| Command | Description |
|---------|-------------|
| `/pptx <topic>` | Ask the active runner to generate a python-pptx script; validated via Python AST before execution, saved to cwd |
| `/drawio <topic>` | Ask the active runner to generate draw.io XML, save `.drawio` to cwd |
| `/review [focus]` | Get `git diff HEAD` and ask the active runner to review it |

**Local-model bootstrap**

| Command | Description |
|---------|-------------|
| `/install-local-server` | Install llama.cpp + default coder model |
| `/install-local-swap` | Install llama-swap (hot model switching) |
| `/list-local-models` | Show models the backend serves |
| `/switch-local-server <kind>` | Switch backend: `llama-server` \| `llama-swap` \| `ollama` \| `vllm` \| `lmstudio` |
| `/download-local-model <repo>` | Fetch a GGUF from HuggingFace |
| `/setup-local-model <repo>` | Download + register in llama-swap.yaml |

**OpenSpec**

| Command | Description |
|---------|-------------|
| `/opsx-list` | List OpenSpec changes |
| `/opsx-status <change>` | Show change progress |
| `/opsx-show <change>` | Show full change detail |
| `/opsx-archive <change>` | Archive a completed change |
| `/opsx-validate <change>` | Validate a change |

**Client-native commands**

Some runners expose their own slash commands that milliways passes through directly. These appear in tab completion when the runner is active.

| Runner | Native commands |
|--------|----------------|
| copilot | `/diff` `/pr` `/review` `/plan` `/delegate` `/research` `/resume` `/compact` `/share` |
| pool | `/mode` |
| claude | `/compact` `/clear` |
| codex | `/compact` |
| gemini | `/clear` `/chat` |

**System**

| Command | Description |
|---------|-------------|
| `/help` | Show all commands |
| `/exit` | Exit (Ctrl+D also works) |
| `!<cmd>` | Run a shell command inline |

**Install clients**

```bash
/install claude        # install claude CLI
/install codex         # install codex CLI
/install copilot       # install copilot CLI
/install gemini        # install gemini CLI
/install local         # install local model server
```

---

## CLI mode

The primary milliways UX is **milliways-term** (a wezterm-based terminal where every tab opens milliways). Slash commands inside any tab dispatch via `milliwaysctl` — see `cmd/milliwaysctl/README.wezterm.md` for the `Leader + /` palette. The CLI surface below exists for **scripting, one-shot prompts, and CI** — when you don't want to open the AI terminal.

```bash
# One-shot prompts
milliways "explain the auth flow"             # route to best kitchen via sommelier
milliways -k claude "review this"             # force a specific kitchen (--kitchen)
milliways -e "refactor store.py"              # show routing decision; do not execute (--explain)
milliways -v "design JWT middleware"          # print sommelier reasoning to stderr (--verbose)
milliways -j "explain this"                   # structured JSON output for scripting (--json)
milliways -r <recipe> "prompt"                # run a named multi-course recipe (--recipe)
milliways --async  "long-running job"         # dispatch in background; returns a ticket ID
milliways --detach "long-running job"         # dispatch detached so it survives shell exit
milliways --session <name> "follow-up"        # resume a named session
milliways --switch-to claude --session foo    # rebind a session to a different kitchen
milliways --timeout 10m "long task"           # override the 5m default dispatch timeout

# Editor / IDE integration
milliways --context-stdin "..."               # read editor context bundle JSON from stdin
milliways --context-file ctx.json "..."       # read editor context bundle JSON from a file
milliways --context-json '{...}' "..."        # pass the bundle directly on the CLI

# Subcommands
milliways status                              # kitchen availability + pantry health + ledger stats
milliways setup <kitchen>                     # install + authenticate a kitchen
milliways login <kitchen>                     # authenticate to a kitchen
milliways login --list                        # show auth status for every kitchen
milliways report                              # ledger routing statistics
milliways report --tiered                     # tiered-CLI performance analysis
milliways init                                # initialise a new milliways config
milliways mode                                # show / set the current routing mode
milliways trace                               # OTel trace inspection
milliways pantry ...                          # pantry (quota / ledger) management
milliways ticket <id>                         # inspect an async-dispatch ticket
milliways tickets                             # list all async tickets
milliways rate                                # rate limit inspection
```

See `milliways --help` for the canonical authoritative flag/subcommand list. The legacy `--repl` built-in line-reader was removed in this release; the milliways-term path is now the only interactive surface.

---

## Runners

**Agent runners** (used in the AI terminal with `/switch` or shorthand `/claude`, `/codex` etc.):

| Runner | Color | Best At | Cost |
|--------|-------|---------|------|
| claude | green | Thinking, planning, code review | Cloud |
| codex | amber | Agentic coding, tool use | Cloud |
| minimax | purple | Reasoning, image/music/lyrics generation | Cloud |
| copilot | red | GitHub Copilot chat | Subscription |
| pool | cyan | Large codebase navigation, ACP agent | Cloud |
| gemini | blue | Research, web search, 1M-token context | Free tier |
| local | green | Offline coding on your laptop (Qwen, DeepSeek, …) | Free, runs locally |

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

## AI clients

Milliways wraps each AI CLI as a first-class runner. They all speak the same interface internally — you switch between them with `/claude`, `/codex`, `/pool` etc. and milliways handles context injection, history, and output streaming the same way for all of them.

```
  you                 milliways              runner (claude / codex / pool / …)
   │                      │                          │
   │   "explain auth"     │                          │
   ├─────────────────────>│                          │
   │                      │   inject history         │
   │                      │   + rules + context      │
   │                      ├─────────────────────────>│
   │                      │                          │  ● Read  auth/middleware.go
   │   streamed output    │   stream tokens           │  ● Bash  go test ./...
   │<─────────────────────┤<─────────────────────────│
   │                      │                          │
   │                      │   session limit?         │
   │                      │   ──────────────         │
   │                      │   /ring active?          │
   │                      │   yes → rotate to next   │
   │                      ├─────────────────────────>│ (next runner)
```

When a runner hits a context or quota limit, milliways rotates to the next one in your ring and re-dispatches the original prompt — the new runner gets a structured briefing so it knows exactly where things left off.

### Claude Code

**Website:** [claude.ai/code](https://claude.ai/code)

Claude Code is probably the strongest all-rounder in the lineup. It has the deepest tool-use loop (Bash, file read/write, MCP servers, computer use), a thinking mode for hard problems, and three reasoning levels you can dial up or down. If you're doing architecture reviews, debugging gnarly issues, or anything that needs genuine reasoning over a lot of context — this is your runner.

milliways feeds it history and rules as synthetic input turns over `--input-format stream-json`, and parses `--output-format stream-json` for progress lines and cost tracking.

```bash
▶ /claude
▶ /model claude-opus-4-7   # switch model live
```

### Codex

**Website:** [github.com/openai/codex](https://github.com/openai/codex)

Codex is OpenAI's open-source agentic coding CLI. Its standout feature is the sandbox: every shell command and file edit runs inside a configurable approval policy, which you can set to fully autonomous (`auto-edit` or `none`) for unattended runs. It emits structured JSON events that milliways parses for the same `● ToolName  detail` progress display used for Claude.

Good pick for: autonomous coding tasks where you want tight sandboxing control.

```bash
▶ /codex
▶ /model o4-mini           # switch model live; list fetched from OpenAI API
```

### MiniMax

**Website:** [minimaxi.com](https://www.minimaxi.com)

MiniMax is the odd one out in a good way — it's the only runner that does text, image, music, lyrics, and speech generation all through the same API. The M2.7 model handles reasoning and code fine, but the real reason you'd reach for it is when a task crosses into creative or multimodal territory that the other runners can't touch.

milliways connects to the MiniMax HTTP API directly (no CLI wrapper), so you configure it in `carte.yaml` rather than pointing at a binary.

```yaml
kitchens:
  minimax:
    http_client:
      base_url: https://api.minimax.io/v1
      auth_key: MINIMAX_API_KEY
      model: MiniMax-M2.7
```

```bash
▶ /minimax
▶ /model MiniMax-M2.7      # switch model live; list fetched from MiniMax API
```

### GitHub Copilot

**Website:** [github.com/features/copilot](https://github.com/features/copilot)

Copilot's edge is GitHub integration. It runs agentic sessions with native awareness of pull requests, issues, and repository metadata — which makes it unusually useful for tasks like "summarise what changed in this PR" or "write a release note from these commits". Requires a Copilot subscription; scoped to repos the authenticated GitHub account can access.

milliways runs `copilot -p <prompt> --allow-all-tools --add-dir <cwd>`, with the working directory pinned to prevent it from wandering into system paths.

```bash
▶ /copilot
milliways login copilot   # auth via GitHub
```

### Pool

**Website:** [poolside.ai](https://www.poolside.ai)

Pool is Poolside's coding agent, built on ACP (Agent Communication Protocol) — an open standard for agentic clients that is also used by Gemini. Pool is tuned for large, complex codebases: it indexes your project at session start and keeps a structural understanding of it across turns. The non-interactive `pool exec` mode supports model and session-mode selection.

milliways runs `pool exec -p <prompt> --unsafe-auto-allow` with optional `--model` and `--mode`.

```bash
▶ /pool
▶ /mode plan             # plan mode — read-only, no writes (forwarded to pool CLI)
pool login               # auth (run once)
```

### Gemini CLI

**Website:** [github.com/google-gemini/gemini-cli](https://github.com/google-gemini/gemini-cli)

Gemini's headline number is its context window — 1 million tokens, the largest of any runner milliways supports. That means you can point it at a big codebase or document set and it can read the whole thing in one shot. It also has native Google Search integration, which makes it a natural first pick for research-heavy prompts. The free tier is generous enough that many workloads run at zero cost.

milliways runs `gemini -p <prompt> -y` (`-y` auto-approves all tool actions — equivalent to other runners' yolo/unsafe modes).

```bash
▶ /gemini
▶ /model gemini-2.5-pro    # switch model live; list fetched from Google API
gcloud auth login          # auth (run once)
```

### Local (llama.cpp + Unsloth)

**Website:** [github.com/ggml-org/llama.cpp](https://github.com/ggml-org/llama.cpp) · [unsloth.ai](https://unsloth.ai/)

The `local` runner is for when the wifi is down, the bill is up, or you just want to know what these things actually do without a credit card in the loop. It talks to any OpenAI-compatible endpoint — by default `llama-server` from llama.cpp on `http://localhost:8765/v1`, but the same code works with llama-swap, vLLM, and LMStudio.

```bash
▶ /local
▶ /list-local-models                         # list models the backend serves
▶ /model qwen2.5-coder-1.5b                  # switch model live
```

There's a full chapter further down — see **[Local models](#local-models)** for architecture, hot-swap setup, memory budgeting, and troubleshooting.

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

## Runner switching and takeover

Type `/<runner>` or `/switch <runner>` to move to a different runner mid-session. Milliways builds a structured briefing from the recent turn log and injects it as the new runner's first prompt, so it picks up exactly where the previous one left off.

```
[briefing from claude → codex]
Recent exchange:
  user: implement the auth middleware
  claude: Added JWT validation...wrote unit tests...

Continue from here. The user's next prompt follows.
```

### Automatic rotation ring

Configure a priority ring and milliways auto-rotates when a runner hits its session limit, quota, or context window — for all seven runners:

```bash
▶ /ring claude,codex,minimax,pool   # set rotation order
▶ /ring                             # show ring + which runners are exhausted
▶ /ring off                         # disable auto-rotation
```

When claude hits its limit the terminal shows:

```
⚑ claude session limit — rotating to codex
→ codex  model: o4-mini  (codex CLI)
[codex] ▶ ▌
```

The new runner receives the turn-log briefing as its first prompt. Exhausted runners are skipped automatically. The exhausted set clears on each new user prompt so runners become available again after a cooling period.

For minimax and local (HTTP runners with a 100-turn agentic loop), hitting the limit triggers a structured summarisation before rotation:

```
────────────────────────────────────────
 ⚑  Reached the 100-turn agentic limit.
────────────────────────────────────────
[summarisation streams here]
⚑ minimax session limit — rotating to local
```

### Manual switch with context

```bash
▶ /switch codex          # switch and carry briefing
▶ /compact               # summarise + shrink the turn log first
▶ /clear                 # wipe log for a clean start
```

### Terminal tab and window title

milliways keeps the terminal tab and title bar in sync with what it's doing.

| State | Tab | Window title bar |
|---|---|---|
| Switch to runner | `● claude · sonnet-4-6` | `milliways · claude` |
| Prompt sent | unchanged | `milliways · claude · thinking…` |
| First token | unchanged | `milliways · claude · streaming…` |
| Response done | unchanged | `milliways · claude · $0.0218 session · 1200→340 tok` |
| Ring rotation | `↻ codex` | `milliways · rotating → codex` |
| Runner error | unchanged | `milliways · claude` (resets to ready) |
| Exit | `milliways` | `milliways` |

The window title shows **cumulative session cost** rather than per-response cost, so a glance at the title bar answers "how much have I spent this session?" The inline hint line `($0.0041 · 1200→340 tok)` under each response still shows the per-turn breakdown.

---

## Observability

### Live metrics dashboard

`/metrics` (in the REPL) or `milliwaysctl metrics` (CLI) shows a live rolling table of tokens, cost, and errors across five time windows for every runner:

```
milliways metrics  (refreshes every 5s — Ctrl+C to exit)

runner     │  1 min          1 hour          24 hours        7 days          30 days
───────────┼──────────────────────────────────────────────────────────────────────────
claude     │  2.1k/0.8k $0.02  18k/6k $0.18  42k/14k $0.41  210k/70k $2.05  …
codex      │  —              4.2k/1.1k $0.04  —              8k/2k $0.07     …
minimax    │  8k/3k $0.01    —               31k/11k $0.28   …               …
gemini     │  —              —               —               —                …
local      │  1.2k/0.4k —   4k/1.5k —       —               —                …
pool       │  —              —               —               —                …
copilot    │  —              —               —               —                …
───────────┼──────────────────────────────────────────────────────────────────────────
total      │  11k/4k $0.03   26k/9k $0.22   73k/25k $0.69   218k/72k $2.12  …

columns: tokens_in / tokens_out  cost_usd   (— = no activity in window)
```

```bash
▶ /metrics                    # in the REPL (one-shot table)
milliwaysctl metrics           # same, one-shot
milliwaysctl metrics --watch   # live refresh every 5s
milliwaysctl metrics --agent claude --tier hourly   # filter to one runner + tier
```

Time windows map to the metrics store tiers:

| Column | Tier | Range |
|--------|------|-------|
| 1 min | raw | last 60 seconds |
| 1 hour | raw | last 60 minutes (rolling, not wall-clock) |
| 24 hours | hourly | last 24 hourly buckets |
| 7 days | daily | last 7 daily buckets |
| 30 days | daily | last 30 daily buckets |

`/quota` shows a compact per-agent summary (last hour) without the full table.

### Gen AI OpenTelemetry instrumentation

Milliways follows the [OpenTelemetry Semantic Conventions for Generative AI](https://opentelemetry.io/docs/specs/semconv/gen-ai/) on all runners.

**Span hierarchy:**

```
gen_ai.client.operation (per dispatch)
  gen_ai.system = "anthropic" | "openai" | "google" | "minimax" | "poolside" | "local"
  gen_ai.operation.name = "chat"
  gen_ai.request.model = "claude-opus-4-5"
  gen_ai.usage.input_tokens = 1200
  gen_ai.usage.output_tokens = 450
  gen_ai.response.finish_reasons = ["stop"]
  │
  └── gen_ai.execute_tool (per tool call, HTTP runners only)
        gen_ai.tool.name = "Bash" | "Read" | "Edit" | "WebFetch" | …
        gen_ai.tool.type = "function"
        milliways.tool.blocked = false
        milliways.tool.duration_ms = 142
```

CLI runners (claude, codex, copilot, gemini, pool) emit one span per subprocess invocation. HTTP runners (minimax, local) emit one parent span per dispatch plus one child span per tool call in the agentic loop.

**Configure the OTel export:**

```bash
# Jaeger / Honeycomb / any OTLP collector
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317

# Stdout (default when no endpoint is set)
export OTEL_TRACES_EXPORTER=console

# Service name shown in traces
export OTEL_SERVICE_NAME=milliways
```

Without `OTEL_EXPORTER_OTLP_ENDPOINT` set, traces are written to stdout in JSON — useful for local debugging with `milliways 2>&1 | jq 'select(.Name)'`.

### Metrics store

The daemon stores all metrics in a SQLite database (`~/.local/state/milliways/metrics.db`) with automatic rollup from raw 1-second rows into hourly, daily, weekly, and monthly aggregates. Query it directly:

```bash
milliwaysctl metrics --metric tokens_in --tier daily --range -30d
milliwaysctl metrics --metric cost_usd  --tier hourly --agent claude
milliwaysctl metrics --metric error_count --tier raw --range -1h
```

Available metrics: `tokens_in`, `tokens_out`, `cost_usd`, `error_count`, `dispatch_count`, `dispatch_latency_ms`.

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

## Local models

The `local` runner exists for offline work, privacy-sensitive prompts, and the simple case of "I just want a coder to autocomplete this". It speaks the OpenAI-compatible Chat Completions protocol so any backend that does the same — llama.cpp's `llama-server`, llama-swap, vLLM, LMStudio, even Ollama's `/v1` shim — drops in without code changes.

We default to **Unsloth dynamic quants** because they consistently produce better quality-per-byte than vanilla GGUF (15–30% faster generation, noticeably better code output, especially on smaller models where every bit matters).

### Architecture

There are two deployment shapes, picked by which installer you ran:

```
                       ┌─────────────────────────────────────┐
                       │   milliways  (/local runner)        │
                       │   POST /v1/chat/completions         │
                       └──────────────────┬──────────────────┘
                                          │
                       MILLIWAYS_LOCAL_ENDPOINT (default :8765)
                                          │
                  ┌───────────────────────┴────────────────────────┐
                  │                                                │
   single-server (install_local.sh)             swap (install_local_swap.sh)
                  │                                                │
        ┌─────────▼──────────┐                          ┌──────────▼──────────┐
        │  llama-server      │                          │  llama-swap proxy   │
        │  one model loaded  │                          │  routes by model id │
        │  port 8765         │                          │  port 8765          │
        └────────────────────┘                          └────┬───────┬───────┘
                                                             │       │
                                                ┌────────────▼───┐ ┌─▼──────────────┐
                                                │ llama-server   │ │ llama-server   │
                                                │ qwen-1.5b      │ │ deepseek-lite  │
                                                │ :9100          │ │ :9101          │
                                                └────────────────┘ └────────────────┘
```

### Single-server vs swap — pick one

| | `install_local.sh` | `install_local_swap.sh` (standby) | `install_local_swap.sh` (HOT_MODE=1) |
|---|---|---|---|
| Models served | one | many | many |
| Switch latency | restart server (~10s + load) | first hit cold (~5–15s), then warm | sub-second always |
| RAM at rest | one model | none after TTL expires | every model resident |
| Best for | one workload, lowest moving parts | mixed workloads on a memory-tight box | mixed workloads on a roomy box |

### Pick the right model for your machine

| RAM | Recommended `MODEL_REPO` | Loaded size (Q4_K_M) |
|----|---|---|
| 8 GB | `unsloth/Qwen2.5-Coder-0.5B-Instruct-GGUF` | ~400 MB |
| 16 GB | `unsloth/Qwen2.5-Coder-1.5B-Instruct-GGUF` (default) | ~1.0 GB |
| 24 GB | `unsloth/Qwen2.5-Coder-7B-Instruct-GGUF` | ~4.7 GB |
| 24 GB+ | `unsloth/DeepSeek-Coder-V2-Lite-Instruct-GGUF` | ~10 GB |
| 32 GB+ | `unsloth/Qwen2.5-Coder-14B-Instruct-GGUF` | ~9 GB |

In hot mode you need RAM for the **sum** of every model you want resident, plus ~4 GB for the OS and your other tabs. So 24 GB will comfortably keep the 1.5B + 7B both warm; trying to add v2-lite on top will start to swap.

### Setup

The fastest path uses `milliwaysctl local` from any milliways-term tab — no leaving the terminal, no hand-editing yaml.

**Single model (simplest):**

```bash
milliwaysctl local install-server          # llama.cpp + qwen2.5-coder-1.5b (default)
milliways                                  # /local is now ready to use
```

In milliways-term, the same flow is available via the `Leader + /` palette: press `Ctrl+Space` then `/`, pick `local install-server`, hit Enter. A new tab spawns the install with output streaming inline.

**Hot-swap between several models:**

```bash
milliwaysctl local install-server                              # first model
milliwaysctl local download-model unsloth/Qwen2.5-Coder-7B-Instruct-GGUF
milliwaysctl local install-swap --hot                          # warm every model at startup
# Or for memory-safe (unload after idle):
milliwaysctl local install-swap                                # standby
```

`milliwaysctl local setup-model <repo>` composes the download + llama-swap config registration + verification in one shot — useful for adding a new model to an already-running swap proxy.

`milliwaysctl local switch-server <kind>` writes `~/.config/milliways/local.env` to point milliways at `llama-server`, `llama-swap`, `ollama`, `vllm`, or `lmstudio` without re-installing anything.

`milliwaysctl local list-models` prints what the active backend is currently serving (handy after a `setup-model` to confirm registration took).

The installer drops a launchd plist (macOS) or systemd-user unit (Linux) bound to port 8765, so the swap proxy comes back up after reboot.

**Fallback (the old script-direct flow):** the underlying scripts are still callable for CI or air-gapped setups: `./scripts/install_local.sh` and `./scripts/install_local_swap.sh`. The `milliwaysctl local` verbs are thin wrappers over them.

### Bootstrap commands

These dispatch to `milliwaysctl local <verb>` via the milliways-term `Leader + /` palette (Ctrl+Space then `/`). Pick from the list, fuzzy-filter by typing, hit Enter — or invoke `milliwaysctl local <verb>` directly in any tab. Adding a new ctl subcommand surfaces it in the palette automatically.

| Command | Underlying ctl | Action |
|---|---|---|
| `/install-local-server` | `milliwaysctl local install-server` | install llama.cpp + the default coder model (qwen2.5-coder-1.5b) |
| `/install-local-swap` | `milliwaysctl local install-swap` | install llama-swap (memory-safe, unloads on TTL); add `--hot` to warm every model at startup |
| `/list-local-models` | `milliwaysctl local list-models` | list models the active backend serves (hits `/v1/models`) |
| `/switch-local-server <kind>` | `milliwaysctl local switch-server <kind>` | rebind milliways to `llama-server` / `llama-swap` / `ollama` / `vllm` / `lmstudio` |
| `/download-local-model <repo>` | `milliwaysctl local download-model <repo>` | curl a GGUF from HuggingFace into `$MODEL_DIR` |
| `/setup-local-model <repo>` | `milliwaysctl local setup-model <repo>` | download → idempotent insert into `~/.config/milliways/llama-swap.yaml` → verify |

### In-session commands

All standard milliways commands work with `local`. The runner-prefixed forms are still available for muscle memory; the contextual forms work across every runner.

| Command | Action |
|---|---|
| `/local` | switch active runner to local |
| `/models` | list models the backend serves (contextual; same as `/list-local-models`) |
| `/model <alias>` | switch model (contextual) |
| `/local-endpoint <url>` | point at a different OpenAI-compatible backend (persists across daemon restarts) |
| `/local-temp <0.0–2.0\|default>` | sampling temperature; `default` lets the server choose |
| `/local-max-tokens <N\|off>` | cap reply length; `off` means unlimited |
| `/local-hot on\|off` | warm every advertised model (`on`) or let llama-swap evict on TTL (`off`) |
| `/ring claude,local,gemini` | set the auto-rotation ring — local works just like cloud runners |

### Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `MILLIWAYS_LOCAL_ENDPOINT` | `http://localhost:8765/v1` | Where the OpenAI-compatible API lives |
| `MILLIWAYS_LOCAL_MODEL` | `qwen2.5-coder-1.5b` | Initial model id sent in every request |
| `MILLIWAYS_LOCAL_API_KEY` | — | Sent as `Authorization: Bearer …` (for llama-server `--api-key`, vLLM strict mode) |
| `MILLIWAYS_LOCAL_TEMP` | `default` (server picks) | Sampling temperature — set via `/local-temp`; `default` omits the field |
| `MILLIWAYS_LOCAL_MAX_TOKENS` | `off` (unlimited) | Cap reply length — set via `/local-max-tokens`; `off` omits the field |

### Temperature

Temperature controls how random the model's next-token pick is. The model assigns a probability to every possible next word; temperature divides those scores before sampling — lower temperature sharpens the distribution toward the most-likely token, higher temperature flattens it so unlikely words can win.

| Temperature | Behaviour | When to use |
|---|---|---|
| `0.0` | Always picks the most-likely token | Deterministic test fixtures; some local models loop at exactly 0 — pick `0.1` if so |
| `0.2` | Almost always the top pick, with rare deviations | Coding, refactoring, anything where correctness > creativity |
| `0.7` | Balanced — varied but coherent | Chat, summarisation, commit messages |
| `1.0+` | More creative, more error-prone | Brainstorming, drafting prose |
| `1.5+` | Often incoherent | Rarely useful |

Switch at runtime with `/local-temp 0.7` or `/local-temp default` (lets the server pick). The current value shows up in `/switch local`'s settings dump, so you can always check what's active.

### Troubleshooting

**`HTTP 401: Authorization header missing or malformed`** — something else (often a corp SSH tunnel or Spring Boot dev service) is bound to port 8765 and milliways is talking to it. Find it with `lsof -i :8765`, kill it, and restart `milliways-local-server`. The installer auto-shifts to a free port if 8765 is busy at install-time, but a tunnel started afterwards won't be detected.

**Slow first prompt after idle (standby mode)** — this is by design. llama-swap evicts the model after `TTL_SECONDS` of no traffic; the next request pays a 5–15s cold-load. Switch to hot mode (`HOT_MODE=1 ./scripts/install_local_swap.sh`) or run `/local-hot on` to keep them resident.

**`failed to download model from Hugging Face` / Zscaler block page** — corporate proxies that categorise HF as "Generative AI" often intercept the API endpoints (`api.huggingface.co`) but leave the CDN (`cas-bridge.xethub.hf.co`) alone. The installer goes straight to the CDN with `curl`, so this usually just works. If not, request HF be allowlisted under "Developer Tools".

**`/model X` says "X not in backend models"** — with single-server (`install_local.sh`), the model field in the API request is ignored — only the `-m` GGUF actually loaded is served. Restart the server with a different `-m`, or run `install_local_swap.sh` to get real per-request model routing.

**Memory pressure / OOM** — drop to a smaller model (`MODEL_REPO=unsloth/Qwen2.5-Coder-0.5B-Instruct-GGUF`), or stay in standby mode (default for swap). `top` / `Activity Monitor` will show you which `llama-server` child is the heavyweight.

**llama-server died at startup** — `tail ~/.local/share/milliways/local/server.err`. Most common cause: a GGUF that didn't download fully (verify size matches what HuggingFace shows) or a context size larger than the model supports (set `CTX_SIZE=4096` and re-run the installer).

---

## Tool security

The HTTP-based runners (minimax, local) drive an agentic tool loop that lets the model invoke `bash`, file `read`/`write`/`edit`, `grep`/`glob`, and `web_fetch` on your machine. milliways applies guardrails by default; the bars can be raised but should not be lowered for shared / multi-tenant deployments.

| Constraint | Default | Override | Why |
|---|---|---|---|
| **Workspace root** | process cwd | `MILLIWAYS_WORKSPACE_ROOT=<dir>` | File `read`/`write`/`edit`/`grep`/`glob` and `bash`'s cwd are jailed inside this directory. Paths outside the root are refused. |
| **Credential denylist** | hardcoded | not overridable | Even inside the workspace, `~/.ssh/`, `~/.aws/`, `~/.gnupg/`, `~/.kube/`, `~/.netrc/`, `~/.docker/config.json`, `~/.config/milliways/local.env`, `~/.config/anthropic/auth.json`, `~/.config/gh/hosts.yml` cannot be read or written. |
| **WebFetch SSRF block** | on | `MILLIWAYS_TOOLS_ALLOW_LOOPBACK=1` | Loopback / RFC1918 / link-local hosts and cloud-metadata IPs (`169.254.169.254`, `metadata.google.internal`) are rejected. Redirects are re-validated. The opt-in env var allows loopback only — cloud-metadata blocking is unconditional. |
| **Bash command logging** | length + sha256 prefix | not overridable | Model-generated commands can contain secrets via env-var interpolation; the full command is intentionally dropped from the daemon log. |
| **Tool result wrapping** | `<tool_result>...</tool_result>` markers | not overridable | Tool output is wrapped + system prompt declares it untrusted, mitigating prompt-injection via tool fold-back (a model `Read`-ing an attacker-planted file can't smuggle directives). |
| **Subprocess env** | safelisted (PATH/HOME/USER/SHELL/TERM/LANG/LC_*/TMPDIR/XDG_* + per-CLI auth keys) | edit `safeRunnerEnvKeys` in the source | claude/codex/copilot/gemini/pool subprocesses do not inherit the daemon's full env. Closes the codex-printenv exfil path. |
| **Codex sandbox** | `--sandbox workspace-write --ask-for-approval never` | per-kitchen `cfg.Args` overrides win | Without these, codex `exec --json` silently refuses tool execution. The trade-off is documented in `SECURITY.md`. |
| **Disable tools entirely** | n/a | `MINIMAX_TOOLS=off`, `MILLIWAYS_LOCAL_TOOLS=off` | Chat-only mode for the HTTP runners (debugging, comparison testing). |
| **Agentic loop turn cap** | 10 | n/a | Hard upper bound on assistant→tool→assistant cycles per dispatch; `chunk_end` carries `max_turns_hit:true` when reached. |

CLI-based runners (claude/codex/copilot/gemini/pool) inherit tool execution from their underlying CLI process. Codex's sandbox applies via the kitchen adapter / daemon-side defaults; the others manage their own filesystem/network access.

## Agent toolkit

milliways can load agent role definitions and skill modules from a directory you control:

```bash
export MILLIWAYS_AGENTS_DIR=~/path/to/your/agents
```

[agent-toolkit-bundle](https://github.com/mwigge/agent-toolkit-bundle) is a ready-made toolkit with 20 agents and 50+ skills for Claude Code, OpenCode, and Codex.

### Install

```bash
git clone https://github.com/mwigge/agent-toolkit-bundle ~/agent-toolkit-bundle
```

Add to your shell profile (`~/.zshrc`, `~/.bashrc`, etc.):

```bash
export MILLIWAYS_AGENTS_DIR=~/agent-toolkit-bundle
```

Restart your shell or `source ~/.zshrc`. milliways will pick up agents and skills on the next launch.

### How it works

milliways reads from `$MILLIWAYS_AGENTS_DIR`:

| Path | Purpose |
|------|---------|
| `agents/opencode/*.md` | Agent role definitions (frontmatter + system prompt) |
| `skills/<name>/SKILL.md` | Skill content injected before dispatch |
| `skill-rules.json` | Regex rules that map prompt keywords to skills |
| `AGENTS.md` / `CLAUDE.md` | Global conventions prepended to every context |

Skills are auto-matched against your prompt before each dispatch. Matching skill content is injected into the context so the active runner has domain-specific guidance without you needing to invoke it explicitly.

### Agents

Invoke via `@agent-name` (Claude Code / OpenCode). All agents are leaf agents — they do not spawn sub-agents.

| Agent | Role |
|-------|------|
| `@architect` | Architecture design, ADRs, interface specs |
| `@ai-developer` | LLM integration, RAG, MCP servers, evals |
| `@api` | REST/OpenAPI 3.1 design and review |
| `@coder-go` | Go feature implementation (strict TDD) |
| `@coder-python` | Python feature implementation (strict TDD) |
| `@coder-rust` | Rust feature implementation (strict TDD) |
| `@coder-sql` | SQL migrations, schema design, query optimisation |
| `@coder-tdd` | TDD red phase — failing tests before implementation |
| `@coder-typescript` | TypeScript/React implementation (strict TDD, Vitest) |
| `@data-analyst` | Data analysis, statistical testing, visualisation |
| `@data-engineer` | Pipelines, dbt, Airflow, Spark, Snowflake |
| `@jira-story` | Jira story creation with acceptance criteria |
| `@observability` | OpenTelemetry instrumentation review |
| `@opsx` | OpenSpec workflow — propose, apply, archive changes |
| `@product-owner` | User stories, backlog prioritisation, OKRs |
| `@refactor` | Refactoring — extract, rename, simplify, pay debt |
| `@reviewer` | Adversarial 4-lens code review |
| `@security` | Security review — OWASP, secrets, auth, deps |
| `@sre` | Deployment safety, SLOs, runbooks, rollback |
| `@tester` | Test strategy, coverage analysis, TDD red phase |

### Skills

Skills are auto-activated by keyword matching (`skill-rules.json`). You can also invoke them explicitly with `/skill-name` in Claude Code or OpenCode.

**Languages & runtimes**

| Skill | Keywords |
|-------|----------|
| `python` | Python, pytest, FastAPI, Django, Flask |
| `typescript` | TypeScript, React, Next.js, Vitest, Vite |
| `golang` | Go, goroutine, channel, Go module |
| `golang-patterns` | Idiomatic Go, Go best practices |
| `rust` | Rust, Cargo, lifetime, borrow |
| `nodejs` | Node.js, Express, Fastify, NestJS, npm |

**Databases**

| Skill | Keywords |
|-------|----------|
| `database` | SQL, Postgres, MySQL, migration, schema |

**DevOps & infrastructure**

| Skill | Keywords |
|-------|----------|
| `ci-cd` | CI/CD, GitHub Actions, GitLab CI, pipeline, deployment |
| `docker-expert` | Docker, container, Dockerfile, Compose |
| `kubernetes-patterns` | Kubernetes, k8s, Helm, kubectl |
| `iac-patterns` | Terraform, Pulumi, IaC, CDK |

**Observability & reliability**

| Skill | Keywords |
|-------|----------|
| `observability` | OTel, OpenTelemetry, tracing, span, Jaeger |
| `sre` | SRE, SLO, SLA, error budget, toil |
| `incident-response` | Incident, postmortem, oncall, runbook |
| `chaos-engineer` | Chaos, fault injection, resilience, circuit breaker |

**Architecture & design**

| Skill | Keywords |
|-------|----------|
| `microservices-architect` | Microservice, service mesh, gRPC, protobuf |
| `multi-tenancy` | Multi-tenant, tenant isolation |
| `performance-engineer` | Performance, latency, throughput, profiling |
| `api-designer` | REST API, OpenAPI, endpoint, Swagger |
| `web-design-guidelines` | CSS, UI, design system, Tailwind, accessibility |

**Quality**

| Skill | Keywords |
|-------|----------|
| `tdd-workflow` | TDD, test-driven, red-green, failing test |
| `verification-loop` | Verify, validate, smoke test, integration test |
| `refactoring-specialist` | Refactor, clean up, extract method, tech debt |
| `pr-review` | PR review, code review, pull request |

**Security & compliance**

| Skill | Keywords |
|-------|----------|
| `security-review` | Security, OWASP, vulnerability, injection, XSS |
| `oauth` | OAuth, OIDC, JWT, authentication, SSO |
| `compliance` | Compliance, audit, GDPR, PCI-DSS, SOC2 |

**Data**

| Skill | Keywords |
|-------|----------|
| `data-analyst` | Data analysis, exploratory, statistics |
| `data-engineer` | Data pipeline, dbt, Airflow, Spark |
| `statistical-analysis` | Statistical, regression, hypothesis, p-value |
| `time-series` | Time series, forecast, seasonality, ARIMA |
| `data-visualisation` | Visualisation, chart, plot, matplotlib, Plotly |

**AI & LLM**

| Skill | Keywords |
|-------|----------|
| `ai-developer` | LLM, RAG, MCP, evals, prompt engineering, embeddings |
| `prompt-engineer` | Prompt design, system prompt, few-shot, chain-of-thought |

**Productivity & communication**

| Skill | Keywords |
|-------|----------|
| `documentation` | Documentation, README, changelog, wiki |
| `presentation` | Presentation, slide, deck, pitch |
| `product-owner` | Product owner, user story, sprint, OKR, roadmap |
| `confluence` | Confluence, Atlassian |

**Memory & code intelligence**

| Skill | Keywords |
|-------|----------|
| `mempalace` | MemPalace, palace, memory store |
| `codegraph` | CodeGraph, call graph, blast radius, symbol search |

**OpenSpec workflow**

| Skill | Keywords |
|-------|----------|
| `openspec-apply-change` | OpenSpec, `/opsx` |
| `openspec-explore` | OpenSpec explore |
| `openspec-propose` | OpenSpec propose |
| `openspec-archive-change` | OpenSpec archive |

**Specialised**

| Skill | Keywords |
|-------|----------|
| `caveman` | Caveman — token-compressed output (~75% savings) |
| `pdm-expert` | PDM, Python dependency management |

### Customising skill routing

`skill-rules.json` at the bundle root maps regex patterns to skill names. You can add your own rules or override existing ones:

```json
[
  { "pattern": "\\bdjango\\b|\\bdrf\\b", "skill": "python" },
  { "pattern": "\\bmy-company-framework\\b", "skill": "my-custom-skill" }
]
```

Rules are matched in order; the first match for a skill name wins. milliways also checks `$MILLIWAYS_AGENTS_DIR/.claude/skill-rules.json` as a fallback.

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

---

> *"A towel is about the most massively useful thing an interstellar hitchhiker can have."*
> A context window is a close second. milliways keeps track of both.

---

## License

Apache License 2.0 — see [LICENSE](LICENSE) for the full text.

The `crates/milliways-term` directory contains a modified fork of
[WezTerm](https://github.com/wez/wezterm) (MIT licensed). See
[MILLIWAYS_NOTICE.md](MILLIWAYS_NOTICE.md) and [NOTICE](NOTICE) for
attribution details.
