//! Status-bar helpers — Rust-side facade for the wezterm Lua status
//! bar. The functional code lives in `etc/milliways.lua`; this module
//! exists primarily to document the contract between the two and to
//! expose a couple of tiny helpers that future Rust code can reuse
//! (default socket discovery, the canonical format template, the
//! 200ms deadline budget).
//!
//! Architecture (Phase 4 of the milliways-emulator-fork change):
//!
//!   wezterm UI
//!     │ update-right-status (1 Hz)
//!     ▼
//!   etc/milliways.lua      ← formatting, caching, theme
//!     │ wezterm.run_child_process
//!     ▼
//!   milliwaysctl status --json
//!     │ JSON-RPC over UDS
//!     ▼
//!   milliwaysd
//!
//! Rust does NOT call `milliwaysctl` from this module. The Lua hook
//! drives the cadence; `milliwaysctl` already speaks JSON-RPC to the
//! daemon. Keeping the wiring on the Lua side means users can tweak
//! the format / colours / cadence without recompiling wezterm.

use std::path::PathBuf;

/// Default soft deadline for the synchronous `milliwaysctl status
/// --json` call inside the Lua `update-right-status` hook. Calls that
/// overrun this budget are treated as stale: the previous cached
/// status is rendered with a `…` suffix, per the
/// `status-bar/spec.md` "Status bar is non-blocking" requirement.
///
/// Mirrors the `FETCH_DEADLINE_S` constant in `etc/milliways.lua`.
/// Kept here so future Rust callers (e.g., a native helper that
/// replaces the run_child_process call) inherit the same budget.
pub const FETCH_DEADLINE_MS: u64 = 200;

/// Canonical right-status format template. The Lua formatter renders
/// the same shape with semantic colour applied via `milliways.theme`.
/// Exported here so any Rust-side renderer (tests, alternative
/// formatters) stays in lock-step with the spec.
///
/// Placeholders mirror the JSON keys returned by
/// `milliwaysctl status --json`:
///   {agent}      → active_agent (or "-")
///   {turn}       → turns
///   {tokens_in}  → tokens_in
///   {tokens_out} → tokens_out
///   {cost}       → cost_usd (formatted "$%.2f")
///   {quota_pct}  → quota.limit / quota.used → percentage
///   {errors}     → errors_5m
pub const STATUS_FORMAT_TEMPLATE: &str =
    "{agent} | turn:{turn} | {tokens_in}↑/{tokens_out}↓ tok | {cost} | quota: {quota_pct}% | err:{errors}";

/// Default refresh interval for the wezterm `update-right-status`
/// hook, in milliseconds. 1 Hz matches the spec's default cadence.
/// Sub-second updates during a dispatch are the watch sidecar's job
/// (see `milliwaysctl status --watch`), not this hook.
pub const STATUS_REFRESH_MS: u64 = 1000;

/// Resolve the default UDS path used by `milliwaysctl`. Wraps
/// [`crate::rpc::default_socket_path`] so a future Lua FFI integration
/// can call into Rust to discover the socket without re-implementing
/// the XDG fallback in Lua.
///
/// TODO(lua-ffi): expose this via a wezterm-Lua bridge once the
/// `wezterm_compat` shim grows a Lua surface. Today the Lua side
/// trusts `milliwaysctl` to discover the socket itself (the daemon's
/// own `--socket` resolution mirrors this function).
#[must_use]
pub fn default_socket_path() -> Option<PathBuf> {
    crate::rpc::default_socket_path()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn template_contains_all_placeholders() {
        for placeholder in [
            "{agent}",
            "{turn}",
            "{tokens_in}",
            "{tokens_out}",
            "{cost}",
            "{quota_pct}",
            "{errors}",
        ] {
            assert!(
                STATUS_FORMAT_TEMPLATE.contains(placeholder),
                "template missing placeholder {placeholder}"
            );
        }
    }

    #[test]
    fn deadline_within_one_second() {
        // Sanity: the soft deadline must fit comfortably inside the
        // 1 Hz refresh interval to leave room for rendering.
        assert!(FETCH_DEADLINE_MS < STATUS_REFRESH_MS);
    }
}
