//! milliways — additions to wezterm: agent panes, `/context` cockpit,
//! observability cockpit, status helpers, RPC client to milliwaysd.
//!
//! Module map (all currently placeholders; populated per Phase):
//!
//! - `rpc` — JSON-RPC 2.0 client over UDS to milliwaysd. Types generated
//!   from `proto/milliways.json` via typify (Phase 1).
//! - `agent_domain` — wezterm `Domain` impl that vends agent panes.
//!   Spawns the cat-spike via `wezterm_compat` shim (Phase 3, gated by
//!   TASK-0.4 outcome).
//! - `wezterm_compat` — thin adapter layer absorbing upstream `Domain`
//!   trait churn (Decision 11).
//! - `context_overlay` — `/context` overlay (or pane fallback per
//!   TASK-0.3 outcome). Renders charts via `charts` (Phase 5).
//! - `observability_pane` — `_observability` agent pane. Renders charts
//!   via `charts` and reads from `metrics.rollup.get` + span streams
//!   (Phase 6).
//! - `charts` — Rust-side chart renderer (donut, sparkline, bars, line)
//!   using the `plotters` crate. Decision 12 (Phase 5).
//! - `status_helpers` — Lua-callable helpers for the status bar
//!   (Phase 4).
//! - `reconnect` — pane-side reconnect state machine (Phase 3,
//!   TASK-3.4).

pub mod rpc;
pub mod agent_domain;
pub mod wezterm_compat;
pub mod context_overlay;
pub mod observability_pane;
pub mod charts;
pub mod status_helpers;
pub mod reconnect;

/// Initialise milliways inside `wezterm-gui::main`. The wezterm-gui patch
/// (TASK-2.4) calls this once at startup. Currently a no-op until each
/// Phase populates its module.
pub fn init() {
    // intentionally empty
}
