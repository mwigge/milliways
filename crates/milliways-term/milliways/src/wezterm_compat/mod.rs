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
pub use mux::localpane::LocalPane;
pub use mux::pane::{alloc_pane_id, Pane, PaneId};
pub use mux::tab::Tab;
pub use mux::window::WindowId;
pub use mux::{Mux, MuxNotification};

pub use portable_pty::{native_pty_system, CommandBuilder, PtySize};
pub use wezterm_term::{Terminal, TerminalSize};

/// terminal_size_to_pty_size is private in mux; inline our own conversion.
pub fn terminal_size_to_pty_size(size: TerminalSize) -> PtySize {
    PtySize {
        rows: size.rows as u16,
        cols: size.cols as u16,
        pixel_width: size.pixel_width as u16,
        pixel_height: size.pixel_height as u16,
    }
}
