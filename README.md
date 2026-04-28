# Milliways

> The Restaurant at the End of the Universe — one CLI to route them all.

Milliways is a terminal-first AI cockpit. The default `milliways` launch starts the daemon-backed native terminal (`milliways-term`) so Claude, Codex, MiniMax, and Copilot run in first-class terminal panes with shared sessions, context injection, sleep/wake awareness, and a live status bar.

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

MilliWays.app is a native macOS terminal built on a patched wezterm. Every new tab opens milliways instead of a plain shell. The status bar shows your active agent, working directory, and a live wake badge when the laptop resumes from sleep.

```
[⚡ woke 3m ago] [≈≈ MW v0.4.12] [~/project] [●claude] [1:C 2:X 3:G 4:M 5:L]
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

The legacy built-in terminal mode is still available as `milliways --repl` for fallback and migration only. It is deprecated and scheduled for removal after cockpit parity.

```text
milliways 0.4.12
  type /help for commands
  runners: minimax | copilot | claude | codex

▶ /claude
Switched to claude

▶ explain the auth flow
[claude] ...streaming...
✓ claude  3.2s

 claude | mo:3 | 1.2k↑ 0.8k↓ | $0.02
```

Sessions are auto-saved per working directory and restored on the next `milliways` launch. Context fragments expand inline before dispatch: `@file`, `@git`, `@branch`, `@shell`.

### Commands

**Routing**

| Command | Description |
|---------|-------------|
| `/switch <runner>` | Switch to a runner |
| `/claude` | Switch to claude |
| `/codex` | Switch to codex |
| `/minimax` | Switch to minimax |
| `/copilot` | Switch to copilot |
| `/local` | Switch to local |
| `/stick` | Keep current runner until released |
| `/back` | Undo the most recent switch |
| `/model` | Interactive model picker (arrow keys) or list |
| `/model <id>` | Set model for the current runner |
| `/takeover [runner]` | Hand off to another runner with full context briefing |
| `/takeover-ring <r1,r2,...>` | Configure auto-rotation ring (cycles on session limit) |

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
| `/exit` | Exit |
| `!<cmd>` | Run a shell command |

---

## CLI mode

For one-off requests without opening the AI terminal:

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

**Agent runners** (used in the AI terminal, and in legacy fallback mode with `/switch` or shorthand `/claude`, `/codex` etc.):

| Runner | Color | Best At | Cost |
|--------|-------|---------|------|
| claude | green | Thinking, planning, code review | Cloud |
| codex | amber | Agentic coding, tool use | Cloud |
| minimax | purple | Reasoning, image/music/lyrics generation | Cloud |
| copilot | red | GitHub Copilot chat | Subscription |

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

## Session rotation and runner takeover

When a runner hits its session limit (context window, daily quota, rate limit), milliways can automatically rotate to the next runner in a priority ring and re-dispatch the original prompt — so work continues without interruption.

### Automatic rotation

```bash
▶ /takeover-ring claude,codex,minimax
Rotation ring set: claude → codex → minimax → claude
```

The status bar shows your position in the ring: `●claude 1/3`. When claude hits its limit, milliways auto-rotates to codex and prints:

```
[auto-takeover] claude session limit — continuing on codex
```

The new runner receives a structured briefing so it knows what was being worked on:

```
[TAKEOVER from claude → codex]
## Current task
Implement the auth middleware
## Progress
- Added JWT validation to the middleware chain
- Wrote unit tests for token expiry
## Files changed
internal/auth/middleware.go
internal/auth/middleware_test.go
## Next step
Wire the middleware into the router in cmd/server/main.go
```

### Manual takeover

```bash
▶ /takeover codex        # hand off to a specific runner
▶ /takeover              # use ring-next when a ring is active
```

Without an active ring, `/takeover` requires an explicit target runner.

### Context fidelity — TTY transcript

milliways writes a full ANSI-stripped transcript of every token printed to the terminal to a stable per-working-directory `.log` file in the session store. The briefing generator reads this transcript rather than the 20-turn ring buffer, so **the new runner gets complete context back to the first prompt** — not just the last 20 turns.

### Ring commands

| Command | Description |
|---------|-------------|
| `/takeover-ring claude,codex,minimax` | Set rotation ring |
| `/takeover-ring` | Show current ring |
| `/takeover-ring off` | Clear ring |
| `/takeover [runner]` | Manual handoff with briefing |

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

## License

Apache License 2.0 — see [LICENSE](LICENSE) for the full text.

The `crates/milliways-term` directory contains a modified fork of
[WezTerm](https://github.com/wez/wezterm) (MIT licensed). See
[MILLIWAYS_NOTICE.md](MILLIWAYS_NOTICE.md) and [NOTICE](NOTICE) for
attribution details.
