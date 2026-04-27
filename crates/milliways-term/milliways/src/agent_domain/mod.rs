//! AgentDomain — wezterm Domain implementation that vends agent panes
//! backed by the milliwaysd RPC. See
//! `openspec/changes/milliways-emulator-fork/specs/agent-domain/spec.md`.
//!
//! How spawn_pane works (reverse of LocalDomain):
//!
//! 1. Read `MILLIWAYS_AGENT_ID` from the SpawnCommand env, or fall back to
//!    `_echo` (the demo agent).
//! 2. Dial milliwaysd over UDS, `agent.open({agent_id})`, get back a
//!    handle.
//! 3. Build a CommandBuilder for `milliwaysctl bridge --handle N --socket S`.
//!    The bridge subprocess is the pane shim — it bridges the slave PTY's
//!    stdio with `agent.send` / `agent.stream`.
//! 4. openpty + spawn_command → child Bridge process.
//! 5. Construct a LocalPane around the master PTY. Wezterm renders bytes
//!    flowing from the bridge's stdout (= slave PTY = our master).
//! 6. Spawn a per-pane reconnect watcher (see `watcher`). The watcher
//!    polls the bridge child for exit, drives the FSM in `reconnect`, and
//!    re-spawns the bridge subprocess after a successful re-dial. The
//!    `WatchedBridge` handle is parked on the AgentDomain so the watcher
//!    is dropped when the domain is torn down.
//!
//! All wezterm imports go through `wezterm_compat::*` (Decision 11).

mod banner;
mod bridge_child;
mod watcher;

use crate::wezterm_compat as wc;
use anyhow::{anyhow, Context as _};
use async_trait::async_trait;
use bridge_child::{BridgeChild, SharedChild};
use parking_lot::Mutex;
use std::path::Path;
use std::sync::Arc;
use watcher::{spawn_watcher, SharedSlave, SharedWriter, WatchedBridge, WatcherConfig};

/// Stable name visible to Lua, keybindings, and `wezterm.mux.get_domain`.
pub const AGENT_DOMAIN_NAME: &str = "agents";

/// Default agent id when SpawnCommand carries no MILLIWAYS_AGENT_ID env.
const DEFAULT_AGENT_ID: &str = "_echo";

/// Env-var name an upstream caller (Lua, keybinding, etc.) sets to direct
/// the spawn to a specific agent_id.
const AGENT_ID_ENV: &str = "MILLIWAYS_AGENT_ID";

pub struct AgentDomain {
    id: wc::DomainId,
    /// One `WatchedBridge` per spawned pane, keyed by pane id. Held here
    /// so the watcher thread is dropped when the domain itself is dropped
    /// (e.g. wezterm exit). Future tasks (key handler) will look these up
    /// by pane id to invoke `user_retry()`.
    watchers: Mutex<Vec<(wc::PaneId, Arc<WatchedBridge>)>>,
}

impl AgentDomain {
    pub fn new() -> Self {
        Self {
            id: wc::alloc_domain_id(),
            watchers: Mutex::new(Vec::new()),
        }
    }

    /// Look up the WatchedBridge for a pane, e.g. so a future key handler
    /// can call `user_retry()` after R is pressed in a GaveUp banner.
    #[must_use]
    pub fn watcher_for(&self, pane_id: wc::PaneId) -> Option<Arc<WatchedBridge>> {
        self.watchers
            .lock()
            .iter()
            .find(|(id, _)| *id == pane_id)
            .map(|(_, w)| Arc::clone(w))
    }
}

impl Default for AgentDomain {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait(?Send)]
impl wc::Domain for AgentDomain {
    async fn spawn_pane(
        &self,
        size: wc::TerminalSize,
        command: Option<wc::CommandBuilder>,
        _command_dir: Option<String>,
    ) -> anyhow::Result<Arc<dyn wc::Pane>> {
        let agent_id = extract_agent_id(command.as_ref());
        let socket = crate::rpc::default_socket_path()
            .ok_or_else(|| anyhow!("no socket path; set XDG_RUNTIME_DIR or HOME"))?;

        // 1. agent.open over RPC.
        let handle = open_agent(&socket, &agent_id)
            .await
            .context("dial milliwaysd / agent.open")?;

        // 2. Build the bridge CommandBuilder.
        let bridge_cmd = build_bridge_command(&socket, handle)?;
        let command_description = format!("milliways agent={agent_id} handle={handle}");

        // 3. openpty + spawn the bridge as the slave's command. The
        //    slave is moved into a Mutex so the watcher can call
        //    spawn_command again after a reconnect.
        let pty_system = wc::native_pty_system();
        let pty_size = wc::terminal_size_to_pty_size(size);
        let pair = pty_system.openpty(pty_size)?;
        let initial_child: Box<dyn portable_pty::Child + Send> =
            pair.slave.spawn_command(bridge_cmd)?;
        let shared_child = SharedChild::new(initial_child);
        let shared_slave: SharedSlave = Arc::new(Mutex::new(pair.slave));

        // 4. Take the master writer once and wrap in an Arc<Mutex<>> so
        //    both LocalPane (for user input) and the watcher (for banner
        //    rendering) can lock it.
        let raw_writer = pair.master.take_writer()?;
        let shared_writer: SharedWriter = Arc::new(Mutex::new(raw_writer));

        // 5. Construct a Terminal + LocalPane around the master.
        // The Terminal needs ITS OWN writer for OSC replies. We pass a
        // sink — agent panes don't currently exchange terminal-level
        // queries (cursor position, color reports). If a real runner
        // emits DCS queries we'll revisit (tracked under TASK-3.2-deeper).
        let terminal = wc::Terminal::new(
            size,
            Arc::new(config::TermConfig::new()),
            "milliways",
            "0.1",
            Box::new(std::io::sink()),
        );

        let pane_id = wc::alloc_pane_id();
        let pane: Arc<dyn wc::Pane> = Arc::new(wc::LocalPane::new(
            pane_id,
            terminal,
            Box::new(BridgeChild::new(shared_child.clone())),
            pair.master,
            Box::new(WriterPipe(Arc::clone(&shared_writer))),
            self.id,
            command_description,
        ));

        // 6. Spawn the watcher. It owns clones of the child / slave /
        //    writer arcs and drives the reconnect FSM.
        let watched = Arc::new(spawn_watcher(
            WatcherConfig {
                socket: socket.clone(),
                agent_id: agent_id.clone(),
            },
            shared_child,
            shared_slave,
            shared_writer,
        ));
        self.watchers.lock().push((pane_id, watched));

        Ok(pane)
    }

    fn detachable(&self) -> bool {
        false
    }

    fn domain_id(&self) -> wc::DomainId {
        self.id
    }

    fn domain_name(&self) -> &str {
        AGENT_DOMAIN_NAME
    }

    async fn attach(&self, _window_id: Option<wc::WindowId>) -> anyhow::Result<()> {
        Ok(())
    }

    fn detach(&self) -> anyhow::Result<()> {
        Ok(())
    }

    fn state(&self) -> wc::DomainState {
        wc::DomainState::Attached
    }
}

fn extract_agent_id(command: Option<&wc::CommandBuilder>) -> String {
    if let Some(cmd) = command {
        for (k, v) in cmd.iter_full_env_as_str() {
            if k == AGENT_ID_ENV {
                return v.to_string();
            }
        }
    }
    DEFAULT_AGENT_ID.to_string()
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

/// WriterPipe wraps the shared master writer so it can be cloned-by-Arc
/// and given to LocalPane as a `Box<dyn Write>`. Both LocalPane and the
/// reconnect watcher hold the same Arc; the parking_lot Mutex serialises
/// writes.
struct WriterPipe(Arc<parking_lot::Mutex<Box<dyn std::io::Write + Send>>>);

impl std::io::Write for WriterPipe {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        self.0.lock().write(buf)
    }
    fn flush(&mut self) -> std::io::Result<()> {
        self.0.lock().flush()
    }
}
