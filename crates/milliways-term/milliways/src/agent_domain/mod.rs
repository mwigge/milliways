//! AgentDomain — wezterm Domain implementation that vends agent panes
//! backed by the milliwaysd RPC. See
//! `openspec/changes/milliways-emulator-fork/specs/agent-domain/spec.md`.
//!
//! Current state: stub. `spawn_pane` returns NotImplemented until TASK-1.4
//! lifts the runner code into the daemon and we wire `agent.open / send /
//! stream` here. The trait surface is registered with the wezterm Mux so
//! Lua bindings (Cmd+Shift+A) can already see the domain.
//!
//! All milliways code goes through `wezterm_compat::*` rather than direct
//! `mux::*` imports, per Decision 11 (shim absorbs upstream Domain trait
//! churn).

use crate::wezterm_compat as wc;
use anyhow::anyhow;
use async_trait::async_trait;
use std::sync::Arc;

/// Stable name visible to Lua, keybindings, and `wezterm.mux.get_domain`.
pub const AGENT_DOMAIN_NAME: &str = "agents";

/// AgentDomain is wezterm's view of milliways agent panes. One Domain per
/// running milliways-term; `spawn_pane` opens a pane against the daemon
/// RPC for the requested agent_id.
pub struct AgentDomain {
    id: wc::DomainId,
}

impl AgentDomain {
    pub fn new() -> Self {
        Self {
            id: wc::alloc_domain_id(),
        }
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
        _size: wc::TerminalSize,
        _command: Option<wc::CommandBuilder>,
        _command_dir: Option<String>,
    ) -> anyhow::Result<Arc<dyn wc::Pane>> {
        // Stubbed pending TASK-1.4 + TASK-3.2:
        // 1. Read agent_id from command (passed via SpawnCommand env or args).
        // 2. Dial milliwaysd UDS, call agent.open({agent_id}).
        // 3. Allocate a virtual PTY pair locally.
        // 4. Call agent.stream({handle}), open NDJSON sidecar.
        // 5. Pump NDJSON -> PTY master writes; PTY slave reads -> agent.send.
        // 6. Return a Pane wrapping the PTY master.
        Err(anyhow!(
            "AgentDomain::spawn_pane is not yet wired — TASK-1.4 (runner lift) and TASK-3.2 (PTY plumbing) pending"
        ))
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
