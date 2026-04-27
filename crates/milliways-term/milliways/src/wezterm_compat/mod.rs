//! wezterm_compat — thin re-exports of wezterm-internal types that the
//! milliways crate depends on. Per Decision 11 in `design.md`, all
//! milliways code goes through this shim rather than importing directly
//! from `mux::`, `config::`, etc. — that lets us absorb upstream API
//! changes in one place when wezterm's monthly merge lands.
//!
//! Inventory: every wezterm type listed here SHOULD be reviewed against
//! `PATCHES.md`'s "Tracked upstream Domain trait surface" table at each
//! sync. CI fails if any tracked signature changes between the pinned
//! commit and the candidate sync target.

pub use async_trait::async_trait;

pub use mux::domain::{alloc_domain_id, Domain, DomainId, DomainState};
pub use mux::pane::Pane;
pub use mux::tab::Tab;
pub use mux::window::WindowId;
pub use mux::Mux;

pub use portable_pty::CommandBuilder;
pub use wezterm_term::TerminalSize;
