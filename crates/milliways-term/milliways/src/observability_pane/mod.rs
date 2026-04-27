//! Observability cockpit pane — Phase 6.
//!
//! The pane is reserved on the AgentDomain side via the agent_id
//! `_observability`. `AgentDomain::spawn_pane` special-cases that id
//! and spawns `milliwaysctl observe-render` instead of the standard
//! agent bridge — the renderer subscribes to `observability.subscribe`
//! and writes a text frame to stdout at 1 Hz.
//!
//! The plotters renderer (sparklines, percentile bars, line chart)
//! lands in a follow-up task. For now the pane shows a textual span
//! tail plus summary stats (total spans, error rate, p50/p99 latency).

/// Reserved agent_id used by the keybinding (`Cmd+Shift+O`) and by
/// `AgentDomain::spawn_pane` to dispatch into the observability
/// renderer subprocess. Externalised here so other crates can refer to
/// the same constant rather than hard-coding the underscore-prefixed
/// string.
pub const RESERVED_AGENT_ID: &str = "_observability";
