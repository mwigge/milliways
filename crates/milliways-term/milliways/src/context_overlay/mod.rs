//! `/context` cockpit — reserved agent_id constants.
//!
//! Per Decision 4 of `openspec/changes/milliways-emulator-fork/specs/
//! context-cockpit/spec.md`, the cockpit lands as a *pane* (rather than a
//! wezterm overlay) so it works regardless of TASK-0.3's overlay-vs-pane
//! spike outcome. The pane is read-only: AgentDomain.spawn_pane sees these
//! reserved ids and bypasses the agent-session machinery, instead spawning
//! `milliwaysctl context-render` directly. The renderer subscribes to
//! `context.subscribe` over JSON-RPC and prints text frames at 2 Hz, with
//! each frame prefixed by `\x1b[2J\x1b[H` so the pane redraws in place.
//!
//! Charts (donut, sparkline, routing strip) land in a follow-up alongside
//! the plotters chart renderer in `crates/milliways-term/milliways/src/
//! charts/`.
//!
//! There is no Rust-side rendering or state machine in this module — the
//! daemon owns snapshot composition and `milliwaysctl` owns presentation.
//! That keeps the Rust side dependency-free for /context and means the
//! cockpit is unit-testable end-to-end via Go tests against the daemon.

/// Reserved agent_id for a per-agent /context pane. The pane is scoped to
/// one agent via the `MILLIWAYS_FOCUSED_AGENT` env on the SpawnCommand;
/// when absent the pane falls back to `_all`.
pub const RESERVED_AGENT_ID: &str = "_context";

/// Reserved agent_id for the aggregated /context pane. Always renders
/// `context.get_all` regardless of focused agent.
pub const RESERVED_AGENT_ID_ALL: &str = "_context_all";
