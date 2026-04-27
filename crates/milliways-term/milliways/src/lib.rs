//! milliways — additions to wezterm: agent panes, `/context` cockpit,
//! observability cockpit, status helpers, RPC client to milliwaysd.
//!
//! Module map:
//!
//! - `rpc` — JSON-RPC 2.0 client over UDS to milliwaysd. Types generated
//!   from `proto/milliways.json` via typify in `build.rs`.
//! - `agent_domain` — wezterm `Domain` impl that vends agent panes.
//!   Currently a stub; TASK-1.4 + TASK-3.2 wire real spawning.
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

pub mod agent_domain;
pub mod charts;
pub mod context_overlay;
pub mod observability_pane;
pub mod reconnect;
pub mod rpc;
pub mod status_helpers;
pub mod wezterm_compat;

/// Apache-2.0 §4(d) attribution payload: this fork's NOTICE describing
/// the relationship to upstream wezterm and the pinned commit.
/// Embedded at compile-time and shown by `milliways-term --notice`.
pub const NOTICE_TEXT: &str = include_str!("../../../../MILLIWAYS_NOTICE.md");

/// Upstream wezterm Apache License 2.0 text. Embedded at compile-time
/// from `crates/milliways-term/LICENSE.md` (upstream's own license file)
/// and shown alongside NOTICE_TEXT by `milliways-term --notice`.
/// Upstream wezterm does not ship a separate NOTICE file, so we only
/// embed LICENSE + MILLIWAYS_NOTICE.
pub const UPSTREAM_LICENSE: &str = include_str!("../../LICENSE.md");

/// Print the bundled Apache-2.0 attribution payload to stdout. Called
/// from `wezterm-gui::main` when `--notice` is the first CLI argument,
/// before any wezterm initialisation.
pub fn print_notice() {
    println!("{}", NOTICE_TEXT);
    println!("---- Upstream wezterm LICENSE ----");
    println!("{}", UPSTREAM_LICENSE);
}

use std::sync::Arc;

/// Initialise milliways inside `wezterm-gui::main`. Registers the agent
/// Domain with the global mux so it's reachable via Lua, keybindings,
/// and `wezterm.mux.get_domain('agents')`.
pub fn init() {
    register_agent_domain();
    log::info!("milliways: initialised");
}

fn register_agent_domain() {
    let mux = wezterm_compat::Mux::get();
    let domain: Arc<dyn wezterm_compat::Domain> = Arc::new(agent_domain::AgentDomain::new());
    mux.add_domain(&domain);
}
