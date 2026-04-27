//! Pane-side reconnect state machine for agent panes (TASK-3.4).
//!
//! When an agent pane's UDS connection to milliwaysd drops, the pane
//! should:
//!
//!   1. Show a red banner (rendered as ANSI bytes injected into the
//!      pane) with a countdown.
//!   2. Auto-retry every `retry_interval` for up to `max_total`.
//!   3. On success: clear the banner, request `STREAM <id> <last_offset>`
//!      so the daemon's output ring replays missed bytes.
//!   4. On exhaustion: red error banner with "Press R to retry, X to
//!      close" and wait for user input.
//!
//! This module owns the FSM only. Wiring it into `AgentDomain`'s pane
//! lifecycle lands in TASK-3.4-deeper alongside the OSC / banner
//! rendering side.

use std::time::{Duration, Instant};

/// Default reconnect cadence per agent-domain/spec.md.
pub const DEFAULT_RETRY_INTERVAL: Duration = Duration::from_secs(2);
pub const DEFAULT_MAX_TOTAL: Duration = Duration::from_secs(30);

/// State of one pane's connection to its agent stream.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum State {
    /// Live: bytes flowing.
    Connected,
    /// Just dropped — first reconnect attempt scheduled.
    Disconnected { dropped_at: Instant },
    /// Reconnecting; `attempt` is 1-indexed.
    Reconnecting { dropped_at: Instant, attempt: u32 },
    /// Total budget exhausted — user must press R or X.
    GaveUp { dropped_at: Instant, attempts: u32 },
}

/// Outcome of a single tick of the FSM.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Action {
    /// Wait for the next tick; nothing to do.
    Idle,
    /// Render or update the reconnect banner with this seconds-remaining.
    UpdateBanner { seconds_remaining: u64, attempt: u32 },
    /// Try to reconnect now.
    AttemptReconnect { attempt: u32 },
    /// Render the GaveUp banner.
    RenderGaveUp { attempts: u32 },
    /// Hide any banner — connection is healthy again.
    ClearBanner,
}

/// The reconnect controller. Owns the State and policy.
#[derive(Debug, Clone)]
pub struct Reconnect {
    state: State,
    retry_interval: Duration,
    max_total: Duration,
    last_attempt: Option<Instant>,
}

impl Default for Reconnect {
    fn default() -> Self {
        Self::new(DEFAULT_RETRY_INTERVAL, DEFAULT_MAX_TOTAL)
    }
}

impl Reconnect {
    pub fn new(retry_interval: Duration, max_total: Duration) -> Self {
        Self {
            state: State::Connected,
            retry_interval,
            max_total,
            last_attempt: None,
        }
    }

    pub fn state(&self) -> &State {
        &self.state
    }

    /// Notify the FSM that the underlying connection dropped.
    pub fn on_disconnect(&mut self, now: Instant) {
        if let State::Connected = self.state {
            self.state = State::Disconnected { dropped_at: now };
            self.last_attempt = None;
        }
    }

    /// Notify the FSM that a reconnect attempt succeeded.
    pub fn on_reconnect_success(&mut self) {
        self.state = State::Connected;
        self.last_attempt = None;
    }

    /// User pressed R after GaveUp.
    pub fn user_retry(&mut self, now: Instant) {
        if matches!(self.state, State::GaveUp { .. }) {
            self.state = State::Disconnected { dropped_at: now };
            self.last_attempt = None;
        }
    }

    /// Tick the FSM. Call from a 250ms-1s timer. Returns the action the
    /// caller should take this tick.
    pub fn tick(&mut self, now: Instant) -> Action {
        match self.state {
            State::Connected => Action::Idle,
            State::Disconnected { dropped_at } => {
                if now.duration_since(dropped_at) >= self.max_total {
                    self.state = State::GaveUp { dropped_at, attempts: 0 };
                    return Action::RenderGaveUp { attempts: 0 };
                }
                self.state = State::Reconnecting { dropped_at, attempt: 1 };
                self.last_attempt = Some(now);
                Action::AttemptReconnect { attempt: 1 }
            }
            State::Reconnecting { dropped_at, attempt } => {
                let elapsed = now.duration_since(dropped_at);
                if elapsed >= self.max_total {
                    self.state = State::GaveUp { dropped_at, attempts: attempt };
                    return Action::RenderGaveUp { attempts: attempt };
                }
                let remaining = self.max_total.saturating_sub(elapsed);
                if let Some(last) = self.last_attempt {
                    if now.duration_since(last) >= self.retry_interval {
                        self.state = State::Reconnecting { dropped_at, attempt: attempt + 1 };
                        self.last_attempt = Some(now);
                        return Action::AttemptReconnect { attempt: attempt + 1 };
                    }
                }
                Action::UpdateBanner { seconds_remaining: remaining.as_secs(), attempt }
            }
            State::GaveUp { attempts, .. } => Action::RenderGaveUp { attempts },
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fresh_state_is_connected_and_idle() {
        let mut r = Reconnect::default();
        assert_eq!(r.state(), &State::Connected);
        assert_eq!(r.tick(Instant::now()), Action::Idle);
    }

    #[test]
    fn disconnect_then_first_tick_attempts_reconnect() {
        let mut r = Reconnect::default();
        let t0 = Instant::now();
        r.on_disconnect(t0);
        match r.tick(t0) {
            Action::AttemptReconnect { attempt } => assert_eq!(attempt, 1),
            other => panic!("expected AttemptReconnect, got {other:?}"),
        }
    }

    #[test]
    fn success_returns_to_connected() {
        let mut r = Reconnect::default();
        r.on_disconnect(Instant::now());
        let _ = r.tick(Instant::now());
        r.on_reconnect_success();
        assert_eq!(r.state(), &State::Connected);
    }

    #[test]
    fn after_30_seconds_gives_up() {
        let mut r = Reconnect::default();
        let t0 = Instant::now();
        r.on_disconnect(t0);
        let _ = r.tick(t0);
        let action = r.tick(t0 + Duration::from_secs(31));
        assert!(matches!(action, Action::RenderGaveUp { .. }));
        assert!(matches!(r.state(), State::GaveUp { .. }));
    }

    #[test]
    fn user_retry_from_gave_up_resets_to_disconnected() {
        let mut r = Reconnect::default();
        let t0 = Instant::now();
        r.on_disconnect(t0);
        let _ = r.tick(t0);
        let _ = r.tick(t0 + Duration::from_secs(31));
        assert!(matches!(r.state(), State::GaveUp { .. }));
        r.user_retry(t0 + Duration::from_secs(40));
        assert!(matches!(r.state(), State::Disconnected { .. }));
    }

    #[test]
    fn retry_interval_paces_attempts() {
        let mut r = Reconnect::default();
        let t0 = Instant::now();
        r.on_disconnect(t0);
        let _ = r.tick(t0);
        match r.tick(t0 + Duration::from_secs(1)) {
            Action::UpdateBanner { attempt, .. } => assert_eq!(attempt, 1),
            other => panic!("expected UpdateBanner, got {other:?}"),
        }
        match r.tick(t0 + Duration::from_secs(2)) {
            Action::AttemptReconnect { attempt } => assert_eq!(attempt, 2),
            other => panic!("expected AttemptReconnect, got {other:?}"),
        }
    }
}
