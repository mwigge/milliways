# Milliways update: memory, persistence, and observability

*May 2026 update: the platform is now easier to explain because the runtime behavior, persisted state, and operational evidence are visible in one place.*

---

## What changed

Milliways now has a clearer split between three things that used to be described too loosely:

- **Live session context**: the active REPL turn log used for same-window runner switching.
- **Persisted runtime state**: daemon history, metrics, config, and local feature state that survives process restart.
- **Project memory**: MemPalace and CodeGraph context used when configured.

That distinction matters. It lets the product claim what is already true without overselling automatic restore. The daemon persists history and metrics today. Full automatic REPL restoration by working directory is still a separate product capability.

![Session memory flow](https://raw.githubusercontent.com/mwigge/milliways/master/docs/images/milliways-memory-session-flow.svg)

---

## Evidence from the Linux smoke

The Linux smoke validates the boring but important parts: binaries install, the daemon starts, the control CLI can talk to it, metrics storage exists, structured telemetry is emitted, feature dependencies are available, artifact commands still work, and takeover works through the local-server path.

![Linux package smoke evidence](https://raw.githubusercontent.com/mwigge/milliways/master/docs/images/milliways-evidence-linux-smoke.svg)

Evidence captured in this iteration:

| Check | Result |
|---|---|
| Ubuntu native package install | Passed |
| Fedora native package install | Passed |
| Arch native package install | Passed |
| `milliways`, `milliwaysd`, `milliwaysctl` installed | Passed |
| Version reports `terminal-ui-smoke` | Passed |
| Daemon socket created | Passed |
| `milliwaysctl ping`, `status`, `agents`, `metrics` | Passed |
| Metrics SQLite DB created | Passed |
| Structured telemetry logs present | Passed |
| MemPalace importable from feature Python | Passed |
| CodeGraph command available | Passed |
| `/pptx` validator, `/review`, `/drawio` checks | Passed |
| Ubuntu local server plus two-CLI takeover | Passed |
| Fedora local server plus two-CLI takeover | Passed |
| Arch local server plus two-CLI takeover | Passed |

The smoke run uses native package installation in distro containers. Full source fallback is skipped on an arm64 Docker host for amd64 fallback scenarios, which is expected for this local machine.

---

## Observability is now a visible cockpit

The app layout now keeps observability visible instead of hiding it behind a command. The left side is split into a top client navigator and a bottom observability cockpit. The right side remains the prompt and stream area.

![Observability cockpit](https://raw.githubusercontent.com/mwigge/milliways/master/docs/images/milliways-observability-cockpit.svg)

The cockpit focuses on the signals that help during a live session:

| Signal | Why it matters |
|---|---|
| Latest span | Confirms the daemon and control plane are alive |
| Error rate | Shows whether failures are systemic or isolated |
| P50/P99 latency | Shows normal and tail behavior |
| Tokens in/out | Shows recent context and generation volume |
| Cost | Shows spend without opening a separate dashboard |
| Time to limit | Shows projected quota pressure when quota caps exist |

This is backed by the same metrics and telemetry path used by `milliwaysctl metrics`, so the panel is not a decorative status block. It is a compact view over operational data.

---

## Persistence map

The persistence story is now explicit:

![Persistence map](https://raw.githubusercontent.com/mwigge/milliways/master/docs/images/milliways-persistence-map.svg)

| State | Location | Status |
|---|---|---|
| Current REPL turn log | Process memory | Live only |
| Daemon event history | `~/.local/state/milliways/history/*.ndjson` | Persisted with bounded retention |
| Metrics and cost | `~/.local/state/milliways/metrics.db` | Persisted |
| Local settings | `~/.config/milliways/local.env` | Persisted |
| Project memory | `~/.mempalace` | Persisted when configured |
| Code context | `.codegraph` workspace | Persisted when indexed |

That means the product can safely say: daemon history, metrics, cost, config, and project memory persist. It should not yet say the full interactive transcript is always restored automatically on next launch.

---

## Takeover and handoff

Same-window runner switching uses the active turn log. Cross-pane takeover adds MemPalace when configured.

![Takeover flow](https://raw.githubusercontent.com/mwigge/milliways/master/docs/images/milliways-takeover-flow.svg)

The flow is:

1. The active runner has the current task and recent turn log.
2. Milliways builds a structured briefing with intent, decisions, and the next step.
3. For same-window switching, that briefing is injected directly into the target runner.
4. For cross-pane takeover, the daemon writes a handoff fact through MemPalace when configured.
5. The target runner reads project memory and the handoff briefing before continuing.

If MemPalace is unavailable, takeover still works inside the active process, but cross-pane continuity falls back to the local context available at that moment.

---

## What this makes easier to explain

The updated architecture can now be described without mixing live context and durable memory:

- **During a session**, the active turn log gives fast switching.
- **Across daemon operations**, history and metrics are persisted locally.
- **Across workstreams**, MemPalace provides durable project memory and handoff facts.
- **Across codebases**, CodeGraph provides structural context after indexing.
- **Across platforms**, Linux package smoke validates the same daemon, control CLI, metrics path, and feature setup used by the app surface.

The important product line is simple:

> Milliways is a local runner workspace with visible operations, durable project memory, and explicit handoff boundaries.

That is stronger than claiming magic context continuity everywhere, because it is testable.
