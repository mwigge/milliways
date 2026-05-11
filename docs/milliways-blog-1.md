# Milliways — one terminal, every runner, shared context

*The elevator pitch for why a multi-runner terminal with project memory, session history, and observability changes how you work.*

---

## The problem with seven great tools

Claude reasons deeply. Codex grinds through code. Copilot knows your GitHub repos. Gemini is fast and cheap for search and summarisation. MiniMax runs without quotas. Pool indexes large codebases and holds architectural context across turns. Local llama.cpp runs completely offline on your hardware.

Every one of these is excellent at something. The problem is that they live in separate terminals, separate sessions, and separate contexts. When you switch from Claude to Codex, you start over. When Claude hits its session limit mid-task, you lose the thread. When you want Gemini's speed for a quick lookup but Claude's reasoning for the follow-up, you're copying and pasting between windows.

**Milliways solves this by making all seven runners operate from one controlled surface.**

---

## Architecture: one daemon, any runner

The design is a local daemon that keeps runner state behind one Unix socket. Your terminal connects to the daemon, you switch runners with a slash command — `/claude`, `/codex`, `/gemini` — and the daemon routes your prompt to the right process. No new terminal. No repeated authentication ceremony. Context is carried through the active turn log, daemon history, and project memory when configured.

![Milliways architecture — three client binaries, the daemon, seven runners, and MemPalace](https://raw.githubusercontent.com/mwigge/milliways/master/docs/images/architecture.png)

The runners are their own CLIs — milliways wraps them rather than reimplementing them. Claude's tooling, Codex's sandbox, Copilot's GitHub awareness — all preserved exactly as the vendor ships them. Milliways adds the routing layer and the shared context layer on top, without touching what makes each runner good.

---

## One memory, every runner

The reason switching runners can feel seamless is shared project memory. When MemPalace is configured, milliways queries it before dispatch for memories relevant to what you're asking. Those memories are injected as a `<project_memory>` block the runner sees as part of its context.

The runner doesn't know the memories came from elsewhere. It just sees context that makes it immediately useful in *your* project, not a generic codebase it has never encountered.

![Shared memory flow — enrichWithPalace queries MemPalace and injects project context before every prompt](https://raw.githubusercontent.com/mwigge/milliways/master/docs/images/memory-flow.png)

**The practical effect is that every runner can start informed.** Switch from Claude to Codex mid-session and Codex can receive project structure, architectural decisions, and constraints already captured in project memory. You stop re-explaining the same background to every new tool.

Beyond project memory, milliways maintains a rolling turn log inside the active REPL process. When you switch runners in that process, the recent log is compiled into a structured briefing injected as the new runner's first message. Daemon event history is persisted per runner; the active REPL turn log is currently in-memory unless compacted, handed off, or written through the daemon path.

---

## The rotation ring — uninterrupted flow across session limits

Every runner has limits: context windows, daily quotas, session timeouts. The rotation ring turns those limits from blockers into controlled transitions.

Configure a priority order once — `/ring claude,codex,minimax` — and milliways handles the rest.

When the active runner exhausts — hitting a session limit, context window, or quota — milliways automatically rotates to the next runner in the ring and re-dispatches your original prompt with a structured briefing. You see the transition instead of losing the task.

The handoff is structured, not raw. Milliways builds a briefing from the turn log before rotating, and the incoming runner treats it as ground truth.

**Here's what that looks like in practice.** A code review of milliways can move across runners without copy-pasting the entire prior exchange:

![codex to gemini to pool: three-runner handoff with structured briefings](https://raw.githubusercontent.com/mwigge/milliways/master/docs/images/handoff-session.png)

The chain is codex → gemini → pool. Three runners, three completely different architectures (OpenAI CLI subprocess, Google CLI subprocess, Poolside ACP HTTP client), and the briefing carried the full review context across all of them. Gemini acknowledged the handoff from codex immediately. Pool narrated its own onboarding — it read the briefing, understood what was in progress, and correctly decided to wait for the next prompt.

No manual transcript copying. The user typed `/gemini`, then `/pool`. The active process carried the briefing.

---

## Local model behaviour steering

Local models — llama.cpp, Ollama, vLLM, LMStudio — are first-class runners in milliways, not an afterthought. The local runner speaks the OpenAI-compatible `/v1/chat/completions` API, which means any backend that implements it works out of the box.

### Two deployment modes

![Local runner architecture — single-server vs swap](https://raw.githubusercontent.com/mwigge/milliways/master/docs/images/local-architecture.png)

There are two ways to run local models:

**Single-server** (`/install-local-server`) — one `llama-server` instance, one model loaded, port 8765. Simple, low overhead. Switching models means restarting the server.

**Swap** (`/install-local-swap`) — a `llama-swap` proxy sits at port 8765 and routes requests by model id to individual `llama-server` instances on different ports. Multiple models can be registered; the proxy loads and evicts them on demand. Switch models live with `/model local <name>` — no restart, no reconfiguration.

The swap mode is what makes the local runner genuinely useful as part of the rotation ring. You can have a fast small model (Qwen-1.5b) for quick iterations and a larger reasoning model (DeepSeek-Coder-lite) for deeper analysis, both accessible in the same session.

### Temperature — the most useful control

Temperature is the one parameter that matters most for code work. Too high and the model invents APIs. Too low and it loops or refuses to paraphrase. The defaults are tuned for a developer workflow:

The key insight for local models: `0.2` is the right default for coding tasks. It keeps output deterministic enough to be reliable but avoids the edge cases some models exhibit at exactly `0.0`. Switch to `0.7` when you want the model to draft prose, write commit messages, or brainstorm — anything where variation is a feature rather than a bug.

Set it live, without restarting anything.

### All the runtime controls

The `/model local` command shows the current settings — endpoint, model, temperature, max tokens — so you always know the exact state.

The combination of a local runner with MemPalace and CodeGraph context is the useful end state: local inference, project memory, structural code context, and the same observable dispatch path as hosted runners.

---

## Observability — you can see everything that's happening

Most terminal runner workflows are opaque. Milliways instruments dispatch with OpenTelemetry and exposes a live metrics dashboard, so you can see what is running, what it costs, and how it behaves.

### Gen AI semantic spans

Every dispatch to a runner produces a structured OTel span following the Gen AI semantic conventions. The parent span covers the full dispatch — model, system (anthropic / openai / google / etc.), token counts, cost in USD. Each tool call the runner makes produces a child span.

This means every agent action — every file read, every shell command, every web fetch a runner executes — is a traceable, queryable event. When something goes wrong, you have the full trace, not just a response string.

### Live metrics dashboard

The `/metrics` command (or `milliwaysctl metrics --watch`) shows a rolling table of activity across all runners, updated every five seconds:

Five time windows — 1 min, 1 hour, 24 hours, 7 days, 30 days — backed by a SQLite store with tiered rollup (raw → hourly → daily → weekly → monthly). Metrics data is persisted locally. You can query spend across any window without waiting for a billing cycle.

### App cockpit as ambient signal

MilliWays.app keeps the client navigator in the upper-left pane, the compact observability cockpit in the lower-left pane, and the prompt on the right. The cockpit refreshes in place with the latest span, token totals, cost, time-to-limit when quota data exists, and recent latency.

The window title uses `MilliWays:<current path>`, so the OS window switcher shows the project rather than the control binary.

---

## What you get

- **Seven runners** — claude, codex, copilot, gemini, pool, minimax, local — all in the same terminal session
- **Shared project memory** — MemPalace context injected before prompts when configured
- **Automatic rotation ring** — session limits and quota exhaustion become explicit handoffs, not dead ends
- **Runtime local model steering** — temperature, token limits, and endpoint switchable live without restarts
- **Full observability** — OTel Gen AI spans per dispatch, per tool call; live `/metrics` dashboard across five time windows; cost and usage persisted locally
- **Ambient session state** — left-side navigator, lower-left observability, and right-side prompt stay visible together in MilliWays.app
- **Native Linux packages** — `.deb`, `.rpm`, `.pkg.tar.zst` on every release, one-liner installer that auto-detects your distro

The goal is a single surface where the question is never "which tool do I open" but only "what do I want to build."

---

*May 2026*

**[github.com/mwigge/milliways](https://github.com/mwigge/milliways)**
