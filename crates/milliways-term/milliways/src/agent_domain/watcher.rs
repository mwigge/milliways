//! Per-pane reconnect watcher.
//!
//! For every agent pane that `AgentDomain::spawn_pane` creates, we spin
//! up a single watcher thread that:
//!
//!   1. Polls the bridge subprocess via `Child::try_wait` every 250 ms.
//!      We *poll* rather than block on `Child::wait` because the same
//!      Child handle is shared with `LocalPane` (via `BridgeChild`), and
//!      a blocking `wait()` on one handle would prevent the other side
//!      from observing exit. Polling sidesteps that contention and keeps
//!      the watcher cooperative with the FSM tick cadence (also 250 ms).
//!
//!   2. On exit, calls `Reconnect::on_disconnect`.
//!
//!   3. Drives `Reconnect::tick` at the same 250 ms cadence. The FSM's
//!      output `Action` decides what we do:
//!      - `AttemptReconnect` — re-dial milliwaysd, call `agent.open`
//!        with the same `agent_id`, build a fresh bridge
//!        `CommandBuilder`, and `slave.spawn_command(cmd)`. On success
//!        we swap the new child into the shared `BridgeChild` slot,
//!        inject a "Reconnected" line into the pane's Terminal, and
//!        call `Reconnect::on_reconnect_success`.
//!      - `UpdateBanner` — re-render the red countdown banner.
//!      - `RenderGaveUp` — render the "Press R to retry" banner once
//!        and stop ticking until `WatchedBridge::user_retry()` is
//!        invoked from the key handler.
//!      - `Idle` / `ClearBanner` — sleep until the next tick.
//!
//! Banner injection: the watcher does NOT write banner bytes through the
//! master PTY writer — that stream is one-way (master→bridge stdin) and
//! never echoes back to the pane's display. Instead we hold a
//! `Weak<dyn Pane>`, parse banner bytes via `termwiz::escape::parser`
//! into `Vec<termwiz::escape::Action>`, and call
//! `Pane::perform_actions(actions)` which feeds the actions directly
//! into the LocalPane's wezterm_term::Terminal. We then notify the mux
//! via `MuxNotification::PaneOutput` so the GUI redraws the pane. This
//! is the same path `mux::localpane::emit_output_for_pane` uses for
//! out-of-band pane output.
//!
//! The watcher uses a *thread-local* tokio current-thread runtime for
//! the async RPC calls. wezterm's main async executor is `promise::spawn`
//! (an async-task pool), not tokio — so we cannot rely on a tokio
//! runtime existing in the calling context. Owning a small runtime per
//! pane is wasteful but trivially correct.

use crate::agent_domain::banner;
use crate::agent_domain::bridge_child::SharedChild;
use crate::reconnect::{Action, Reconnect};
use crate::wezterm_compat as wc;
use anyhow::{anyhow, Context as _};
use parking_lot::Mutex;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Weak};
use std::time::{Duration, Instant};

/// 250 ms tick — matches the FSM's expected cadence.
const TICK: Duration = Duration::from_millis(250);

/// Shared writer alias. The same `Box<dyn Write>` lives behind LocalPane's
/// `WriterPipe` and the watcher; both lock the parking_lot Mutex.
pub type SharedWriter = Arc<Mutex<Box<dyn std::io::Write + Send>>>;

/// Shared slave-pty alias. Held behind a Mutex so the watcher can call
/// `spawn_command` repeatedly across reconnects.
pub type SharedSlave = Arc<Mutex<Box<dyn portable_pty::SlavePty + Send>>>;

/// A handle to one pane's reconnect supervision. Stored alongside the
/// LocalPane via `AgentDomain` (see `mod.rs`). The `Drop` impl signals
/// the watcher thread to exit.
pub struct WatchedBridge {
    fsm: Arc<Mutex<Reconnect>>,
    /// Set to true on drop so the watcher exits its loop.
    stop: Arc<AtomicBool>,
}

impl WatchedBridge {
    /// User pressed R after GaveUp — push the FSM back to Disconnected so
    /// the watcher resumes attempting reconnects on its next tick.
    pub fn user_retry(&self) {
        self.fsm.lock().user_retry(Instant::now());
    }

    /// Snapshot the FSM state. Test/debug only.
    #[must_use]
    pub fn fsm_state(&self) -> crate::reconnect::State {
        self.fsm.lock().state().clone()
    }
}

impl Drop for WatchedBridge {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
    }
}

/// Configuration the watcher needs to reconstruct a bridge process after
/// the daemon comes back.
pub struct WatcherConfig {
    pub socket: PathBuf,
    pub agent_id: String,
}

/// Spawn the watcher background thread and return its handle.
///
/// `child` is the shared bridge-child slot (also handed to LocalPane).
/// `slave` is the shared SlavePty for re-spawning. `pane` is a Weak
/// reference to the LocalPane the watcher uses to inject banner bytes
/// directly into the Terminal display via `Pane::perform_actions`. We
/// hold a Weak rather than Arc so the watcher does not keep the pane
/// alive past tab/window teardown.
pub fn spawn_watcher(
    config: WatcherConfig,
    child: SharedChild,
    slave: SharedSlave,
    pane: Weak<dyn wc::Pane>,
    pane_id: wc::PaneId,
) -> WatchedBridge {
    let fsm = Arc::new(Mutex::new(Reconnect::default()));
    let stop = Arc::new(AtomicBool::new(false));

    let bridge = WatchedBridge {
        fsm: Arc::clone(&fsm),
        stop: Arc::clone(&stop),
    };

    // Use a dedicated OS thread + thread-local current-thread tokio
    // runtime: wezterm doesn't provide a tokio runtime, and our async
    // RPC client requires one for UnixStream.
    std::thread::Builder::new()
        .name("milliways-watcher".into())
        .spawn(move || {
            let rt = match tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
            {
                Ok(rt) => rt,
                Err(err) => {
                    log::error!("milliways: failed to build watcher runtime: {err}");
                    return;
                }
            };
            rt.block_on(watcher_loop(config, fsm, child, slave, pane, pane_id, stop));
        })
        .expect("spawn milliways-watcher thread");

    bridge
}

/// Parse a banner byte stream into termwiz Actions and inject them into
/// the pane's Terminal via `Pane::perform_actions`. Notifies the mux so
/// the GUI redraws. Silently no-ops if the pane has been dropped.
fn inject_pane_bytes(pane: &Weak<dyn wc::Pane>, pane_id: wc::PaneId, bytes: &[u8]) {
    let Some(pane) = pane.upgrade() else {
        return;
    };
    let mut parser = termwiz::escape::parser::Parser::new();
    let mut actions = Vec::new();
    parser.parse(bytes, |action| actions.push(action));
    if actions.is_empty() {
        return;
    }
    pane.perform_actions(actions);
    wc::Mux::notify_from_any_thread(wc::MuxNotification::PaneOutput(pane_id));
}

async fn watcher_loop(
    config: WatcherConfig,
    fsm: Arc<Mutex<Reconnect>>,
    child: SharedChild,
    slave: SharedSlave,
    pane: Weak<dyn wc::Pane>,
    pane_id: wc::PaneId,
    stop: Arc<AtomicBool>,
) {
    let mut tick = tokio::time::interval(TICK);
    // Skip the initial immediate tick; we want to wait one cadence
    // before checking child state.
    tick.tick().await;

    let mut gave_up_rendered = false;

    loop {
        if stop.load(Ordering::Relaxed) {
            return;
        }

        tick.tick().await;

        // 1. Detect bridge subprocess exit. We poll try_wait rather than
        //    block on wait() because the Child handle is shared with
        //    LocalPane and a blocking wait would deadlock the other
        //    consumer.
        let exited = child.try_wait_exited();
        if exited {
            let mut f = fsm.lock();
            f.on_disconnect(Instant::now());
            drop(f);
        }

        // 2. Tick the FSM and act on its output.
        let action = fsm.lock().tick(Instant::now());

        match action {
            Action::Idle | Action::ClearBanner => {
                // Nothing to do this tick.
            }
            Action::UpdateBanner {
                seconds_remaining,
                attempt,
            } => {
                let bytes = banner::reconnect_banner_bytes(seconds_remaining, attempt);
                inject_pane_bytes(&pane, pane_id, &bytes);
            }
            Action::AttemptReconnect { attempt } => {
                match attempt_reconnect(&config, &slave, &child).await {
                    Ok(()) => {
                        fsm.lock().on_reconnect_success();
                        let bytes = banner::reconnected_line_bytes(attempt);
                        inject_pane_bytes(&pane, pane_id, &bytes);
                        gave_up_rendered = false;
                    }
                    Err(err) => {
                        log::debug!("milliways: reconnect attempt {attempt} failed: {err:#}");
                        // FSM stays in Reconnecting; next tick will
                        // either retry or transition to GaveUp.
                    }
                }
            }
            Action::RenderGaveUp { attempts } => {
                if !gave_up_rendered {
                    let bytes = banner::gave_up_banner_bytes(attempts);
                    inject_pane_bytes(&pane, pane_id, &bytes);
                    gave_up_rendered = true;
                }
                // Don't re-render every tick. user_retry() will reset
                // the FSM to Disconnected; we detect that by observing a
                // state change away from GaveUp.
                if !matches!(fsm.lock().state(), crate::reconnect::State::GaveUp { .. }) {
                    gave_up_rendered = false;
                }
            }
        }
    }
}

/// Re-dial milliwaysd, call `agent.open`, build a fresh bridge command,
/// spawn it on the slave, and swap the new child into the shared slot.
async fn attempt_reconnect(
    config: &WatcherConfig,
    slave: &SharedSlave,
    child: &SharedChild,
) -> anyhow::Result<()> {
    let handle = open_agent(&config.socket, &config.agent_id)
        .await
        .context("agent.open during reconnect")?;
    let cmd = build_bridge_command(&config.socket, handle)?;
    let new_child = slave
        .lock()
        .spawn_command(cmd)
        .map_err(|e| anyhow!("slave.spawn_command during reconnect: {e}"))?;
    child.replace(new_child);
    Ok(())
}

async fn open_agent(socket: &Path, agent_id: &str) -> anyhow::Result<i64> {
    use serde_json::json;
    let mut client = crate::rpc::Client::dial(socket).await?;
    let resp: serde_json::Value = client
        .call("agent.open", json!({"agent_id": agent_id}))
        .await?;
    resp.get("handle")
        .and_then(|v| v.as_i64())
        .ok_or_else(|| anyhow!("agent.open returned no handle"))
}

fn build_bridge_command(socket: &Path, handle: i64) -> anyhow::Result<wc::CommandBuilder> {
    let mut cmd = wc::CommandBuilder::new("milliwaysctl");
    cmd.arg("bridge");
    cmd.arg("--handle");
    cmd.arg(handle.to_string());
    cmd.arg("--socket");
    cmd.arg(
        socket
            .to_str()
            .ok_or_else(|| anyhow!("non-utf8 socket path"))?,
    );
    Ok(cmd)
}
