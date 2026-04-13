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
| Palate cleanser (🍋) | Hard stop, verify, advance | GATE (★) |
| Kitchen | A CLI tool with its own models and strengths | Agent / tier |
| Station | A capability within a kitchen | Skill / agent role |
| Carte | Which kitchen cooks each task | menu.md (new artifact) |
| Pantry | Shared knowledge all kitchens consume | MCP servers + knowledge graphs |
| Recipe | A fixed multi-course workflow | Pipeline / workflow |
| Maître d' | Orchestrates timing, doesn't cook | Milliways CLI itself |
| Sommelier | Picks the right kitchen for each task | Router intelligence |
| Ledger | What was served, cost, duration, outcome | Routing feedback DB |
| Mise en place | Context prepared before cooking | Knowledge injection per dispatch |
| Prep list | Granular checkboxes | tasks.md |
| Chef's concept | Why this meal exists | proposal.md |
| Recipe book | How to make it, decisions | design.md |
| Service plan | Orchestration rules, sequence, gates | delivery.md |

## Kitchens (initial menu)

| Kitchen | CLI | Stations | Cost Tier | Strength |
|---------|-----|----------|-----------|----------|
| claude | `claude -p` | think, plan, review, explore, sign-off | cloud ($3-75/M) | Deep reasoning, 1M context, adversarial review |
| opencode | `opencode run` | code, test, refactor, lint, commit | local ($0) | devstral:24b, scoped --dir, plugins |
| gemini | `gemini` | search, compare, docs, research | free (1k/day) | Google Search grounding, 1M context |
| aider | `aider --message` | multi-file, git-commit | cloud/local | Git-first auto-commits, multi-file edits |
| goose | `goose` | tools, database, api, mcp | local/cloud | 70+ MCP extensions, provider-agnostic |
| cline | `cline -y --json` | fleet, parallel | cloud | Parallel subagent execution, JSON output |

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
| GitGraph | SQLite | Hotspots, churn, blame, change coupling | high_churn → claude-kitchen |
| QualityGraph | SQLite (extend CodeGraph) | Cyclomatic/cognitive complexity, coverage, smells | high_complexity + low_coverage → careful kitchen |
| DepGraph | SQLite | Packages, versions, CVEs, consumers | CVE-exposed → security-first routing |
| RoutingLedger | SQLite (ndjson) | Task type, kitchen, outcome, cost, duration | Learned preference from history |
| TopologyGraph | SQLite (from simulator) | Services, dependencies, blast radius | High-fanout → escalate review |

## Recipes (multi-course workflows)

| Recipe | Courses | Flow |
|--------|---------|------|
| implement-feature | think → code → test → review → commit | claude → opencode → opencode → claude → aider |
| fix-bug | research → diagnose → fix → test → commit | gemini → claude → opencode → opencode → aider |
| security-audit | scan → review → fix → verify | goose → claude → opencode → claude |
| explore-idea | research → think → design → propose | gemini → claude → claude → claude |
| refactor-module | analyze → plan → refactor → review | claude → claude → aider → claude |

## Scope

### In scope
- Go CLI binary (Bubble Tea TUI + headless mode)
- Kitchen adapters (claude, opencode, gemini, aider, goose, cline)
- Sommelier router (keyword → kitchen, with pantry consultation)
- Pantry integration (MCP clients for MemPalace, CodeGraph, TaskQueue)
- New knowledge graphs (GitGraph, QualityGraph, DepGraph, RoutingLedger)
- Recipe engine (multi-course sequential execution with context handoff)
- Configuration via `~/.config/milliways/carte.yaml`
- Routing ledger with `--json` output for analysis
- Carte.md as new OpenSpec artifact type

### Out of scope
- Web UI (CLI/TUI only)
- Model hosting (kitchens bring their own models)
- Replacing any kitchen (Milliways orchestrates, never cooks)
- Cloud deployment (local-first, single developer tool)
- Multi-user / team features (single-seat tool)

## Acceptance Criteria

### Service 1 — Core + First Kitchen (MVP)
- `milliways "explain the auth flow"` routes to claude, streams response, logs to ledger
- `milliways --kitchen opencode "add rate limiting"` forces a specific kitchen
- `milliways --json "task"` outputs structured JSON result
- Ledger records: ts, task hash, kitchen, duration, exit code

### Service 2 — Pantry + Sommelier
- Sommelier consults CodeGraph + GitGraph before routing
- High-churn + high-complexity file → routes to claude automatically
- `milliways --explain "task"` shows routing reasoning without executing

### Service 3 — Recipes + Multi-Course
- `milliways --recipe implement-feature "add JWT auth"` executes full think→code→test→review→commit
- Context passes between courses (stdout → next course stdin or temp file)
- Each course logged separately in ledger

### Service 4 — TUI
- `milliways --tui` opens interactive mode with Bubble Tea
- Split panes: input, output stream, ledger
- Kitchen selector with tab completion

### Service 5 — Full Pantry
- All 5 new knowledge graphs populated and consulted
- Routing accuracy measurably better than keyword-only (A/B test via ledger)

## Core Principle: CLI-native, Zero Credentials

Milliways never touches API keys, tokens, or credentials. Each kitchen is a CLI tool the user has already logged into independently. Milliways calls the binary — same as if the user typed the command themselves.

```
What Milliways does:           What Milliways does NOT do:
─────────────────────          ─────────────────────────────
✓ exec claude -p "..."         ✗ call api.anthropic.com
✓ exec opencode run "..."      ✗ read ANTHROPIC_API_KEY
✓ exec gemini "..."            ✗ store any credential
✓ check if binary exists       ✗ manage authentication
✓ stream stdout line by line   ✗ proxy HTTP requests
✓ capture exit code            ✗ intercept model responses
```

If a kitchen isn't installed or authenticated, Milliways skips it and routes to the next available kitchen. `milliways --explain` shows which kitchens are available and which are missing.

## Non-Goals
- API key management or credential storage
- Token-level cost optimization (kitchens handle their own billing)
- Acting as a proxy or gateway between user and model providers
- Model fine-tuning or training
- Replacing OpenSpec (Milliways consumes OpenSpec, doesn't replace it)
- Replacing any kitchen CLI (Milliways orchestrates, never cooks)
- IDE integration (terminal-first)
