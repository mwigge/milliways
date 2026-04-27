//! ANSI banner rendering for the reconnect FSM.
//!
//! Writes a 3-line red banner showing the reconnect countdown. The byte
//! stream is consumed by `watcher::watcher_loop`, which parses it via
//! `termwiz::escape::parser::Parser` and feeds the resulting actions into
//! the pane's Terminal display through `Pane::perform_actions` (NOT the
//! master PTY writer — that's one-way bridge stdin and doesn't echo to
//! the visible pane).
//!
//! Format (ANSI; \x1b[31m red, \x1b[0m reset):
//!
//! ```text
//! ┌──────────────────────────────────────────┐
//! │ Reconnecting… attempt N (Ms left)        │
//! └──────────────────────────────────────────┘
//! ```
//!
//! The `render_gave_up_banner` variant emits the GaveUp prompt instead.

use std::io::{self, Write};

/// Minimum inner width of the banner box (cells); the box auto-expands
/// to fit longer content.
const MIN_INNER_WIDTH: usize = 42;

/// Render the reconnect-in-progress banner. `seconds_remaining` is the
/// total budget remaining (per `Reconnect::max_total`); `attempt` is
/// 1-indexed.
pub fn render_reconnect_banner(
    writer: &mut dyn Write,
    seconds_remaining: u64,
    attempt: u32,
) -> io::Result<()> {
    let line = format!(" Reconnecting attempt {attempt} ({seconds_remaining}s left)");
    write_box(writer, &line)
}

/// Render the GaveUp banner with the user-action hint.
pub fn render_gave_up_banner(writer: &mut dyn Write, attempts: u32) -> io::Result<()> {
    let line = format!(" Disconnected after {attempts} attempts - Press R to retry, X to close");
    write_box(writer, &line)
}

/// Render the "we just reconnected" status line that clears the banner.
/// One line, green→reset, terminated by `\r\n`.
pub fn render_reconnected_line(writer: &mut dyn Write, attempt: u32) -> io::Result<()> {
    write!(
        writer,
        "\x1b[32mReconnected on attempt {attempt}\x1b[0m\r\n"
    )?;
    writer.flush()
}

/// Convenience: render the reconnect banner into a byte buffer.
#[must_use]
pub fn reconnect_banner_bytes(seconds_remaining: u64, attempt: u32) -> Vec<u8> {
    let mut buf = Vec::with_capacity(256);
    // Writes to a Vec are infallible.
    let _ = render_reconnect_banner(&mut buf, seconds_remaining, attempt);
    buf
}

/// Convenience: render the gave-up banner into a byte buffer.
#[must_use]
pub fn gave_up_banner_bytes(attempts: u32) -> Vec<u8> {
    let mut buf = Vec::with_capacity(256);
    let _ = render_gave_up_banner(&mut buf, attempts);
    buf
}

/// Convenience: render the reconnected status line into a byte buffer.
#[must_use]
pub fn reconnected_line_bytes(attempt: u32) -> Vec<u8> {
    let mut buf = Vec::with_capacity(64);
    let _ = render_reconnected_line(&mut buf, attempt);
    buf
}

fn write_box(writer: &mut dyn Write, content_line: &str) -> io::Result<()> {
    // Auto-size: inner width is at least MIN_INNER_WIDTH but expands to
    // fit the content line. We assume ASCII content — the ANSI banner is
    // intentionally narrow so multi-byte characters don't appear here.
    let content_cells = content_line.chars().count();
    let inner_width = content_cells.max(MIN_INNER_WIDTH);

    // Pad with trailing spaces to align the right border.
    let mut padded = String::with_capacity(inner_width);
    padded.push_str(content_line);
    while padded.chars().count() < inner_width {
        padded.push(' ');
    }

    let top: String = std::iter::once('┌')
        .chain(std::iter::repeat_n('─', inner_width))
        .chain(std::iter::once('┐'))
        .collect();
    let bottom: String = std::iter::once('└')
        .chain(std::iter::repeat_n('─', inner_width))
        .chain(std::iter::once('┘'))
        .collect();

    // \x1b[31m = red foreground; \x1b[0m = reset.
    write!(writer, "\x1b[31m{top}\r\n│{padded}│\r\n{bottom}\x1b[0m\r\n")?;
    writer.flush()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reconnect_banner_contains_attempt_and_seconds() {
        let mut buf = Vec::new();
        render_reconnect_banner(&mut buf, 12, 3).unwrap();
        let s = String::from_utf8(buf).unwrap();
        assert!(s.contains("attempt 3"), "got {s:?}");
        assert!(s.contains("12s left"), "got {s:?}");
        assert!(s.contains("\x1b[31m"), "expected red ANSI: {s:?}");
        assert!(s.contains("\x1b[0m"), "expected reset ANSI: {s:?}");
    }

    #[test]
    fn gave_up_banner_contains_user_hints() {
        let mut buf = Vec::new();
        render_gave_up_banner(&mut buf, 7).unwrap();
        let s = String::from_utf8(buf).unwrap();
        assert!(s.contains("7 attempts"), "got {s:?}");
        assert!(s.contains("Press R"), "got {s:?}");
        assert!(s.contains("X to close"), "got {s:?}");
    }

    #[test]
    fn reconnected_line_is_green_and_oneline() {
        let mut buf = Vec::new();
        render_reconnected_line(&mut buf, 4).unwrap();
        let s = String::from_utf8(buf).unwrap();
        assert!(s.contains("attempt 4"));
        assert!(s.contains("\x1b[32m"));
        assert_eq!(s.matches('\n').count(), 1);
    }
}
