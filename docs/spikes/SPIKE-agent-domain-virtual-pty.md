# SPIKE: AgentDomain over a virtual PTY (cat-backed)

**Status**: NOT YET RUN
**Owner**: _unassigned_
**Blocks**: Phase 3 (`AgentDomain` MVP) of the `milliways-emulator-fork` change.
**Time estimate**: 2–3 days for a contributor who has not worked on wezterm internals; ~1 day if you have. If it stretches to a week, that *is* the answer — it tells us the patch budget needs to grow significantly.

## The question

Does wezterm's `Domain` trait survive a no-op `AgentDomain` implementation that spawns `cat` over a virtual PTY?

Wezterm's pane lifecycle (resize, copy mode, search, scrollback, splits, focus events, mouse selection) was designed against `LocalDomain` (real shell over a real PTY). Production `AgentDomain` will look similar but with bytes coming from the milliways daemon, not a child shell. Before we fork wezterm and commit to the design, we need to know whether all those pane features survive when the `Domain` is something other than `LocalDomain`.

## Why we run it now

The architect review (REVIEW.md, risk 3) flagged the `Domain` trait stability and the real-PTY-vs-virtual-PTY asymmetry. If features break, the patch budget grows beyond the proposed <500 LoC, and we learn that AgentDomain needs a `wezterm_compat` shim before Phase 3, not after.

## Setup

```bash
# 1. Clone upstream wezterm (NOT subtree-import — we are not forking yet):
git clone --depth=1 https://github.com/wez/wezterm.git ~/dev/wezterm-spike
cd ~/dev/wezterm-spike

# 2. Find the latest release tag and check it out:
git fetch --tags
LATEST=$(git tag --sort=-creatordate | head -1)
echo "latest tag: $LATEST"
git checkout "$LATEST"

# 3. Bootstrap the build (first time only — pulls ~600 crates, takes 10–20 min):
cargo build --release -p wezterm-gui
~/dev/wezterm-spike/target/release/wezterm-gui --version
```

## Architecture of the spike

Goal: a new `AgentDomain` trait impl that registers itself at startup and exposes one pane type. That pane spawns `cat` with its stdin/stdout connected to a freshly-allocated PTY pair.

Files to add (under `~/dev/wezterm-spike/`):

- `mux/src/agent_domain.rs` — the new domain.
- `mux/src/lib.rs` — register the domain in `Mux::new`.

`Domain` trait reference: `mux/src/domain.rs` in upstream. Methods to implement (from the upstream version at fork time):

```rust
pub trait Domain: DomainState + Send + Sync {
    fn domain_id(&self) -> DomainId;
    fn domain_name(&self) -> &str;
    fn domain_label(&self) -> &str { self.domain_name() }
    async fn spawnable(&self) -> bool { true }
    async fn spawn_pane(
        &self,
        size: TerminalSize,
        command: SpawnCommand,
        command_dir: Option<String>,
    ) -> Result<Arc<dyn Pane>>;
    async fn split_pane(...) -> Result<Arc<dyn Pane>>;
    async fn attach(&self, _window_id: Option<MuxWindowId>) -> Result<()> { Ok(()) }
    async fn detach(&self) -> Result<()> { Ok(()) }
    fn local_pane_id(&self, _pane_id: PaneId) -> Option<PaneId> { None }
    fn state(&self) -> DomainState { DomainState::Attached }
    // ... and several more depending on the pinned tag
}
```

The `spawn_pane` implementation is the load-bearing part:

```rust
// pseudocode — verify against the actual trait at the pinned tag
async fn spawn_pane(
    &self,
    size: TerminalSize,
    _command: SpawnCommand,
    _command_dir: Option<String>,
) -> Result<Arc<dyn Pane>> {
    // 1. Allocate a PTY pair via portable-pty (same crate wezterm uses).
    let pty_system = portable_pty::native_pty_system();
    let pair = pty_system.openpty(size.into())?;

    // 2. Spawn `cat` connected to the slave side.
    let mut cmd = portable_pty::CommandBuilder::new("cat");
    let child = pair.slave.spawn_command(cmd)?;

    // 3. Wrap the master end as a wezterm Pane.
    //    LocalPane is the example to copy from in mux/src/localpane.rs.
    let pane = LocalPane::new(
        self.next_pane_id(),
        pair.master,
        child,
        // ... whatever else LocalPane wants
    );

    Ok(Arc::new(pane))
}
```

The point is **not** to reimplement LocalPane — for the spike, copying its construction logic is fine. The point is that we own a `Domain` named `agent` that is not `LocalDomain`, and it produces panes that wezterm has to render. If wezterm's pane code has any "if this is LocalDomain..." special cases, the spike will hit them.

## Test matrix

After the spike build runs, open wezterm and switch to the agent domain:

```
# wezterm config snippet:
config.default_domain = "agent"
# or: bind a key to wezterm.action.AttachDomain "agent"
```

Open a pane in the agent domain. Type something — `cat` echoes it back. Now exercise every pane feature and record results.

| # | Feature | How to trigger | Outcome (PASS / PARTIAL / FAIL) | Notes |
|---|---------|----------------|--------------------------------|-------|
| 1 | Type and see echo | type "hello", press Enter | _TBD_ | |
| 2 | Resize | drag terminal window edge | _TBD_ | does cat see SIGWINCH? |
| 3 | Vertical split | `Cmd+Shift+D` (or default split) | _TBD_ | does the new pane also use AgentDomain? |
| 4 | Horizontal split | `Cmd+Shift+H` (or default) | _TBD_ | |
| 5 | New tab | `Cmd+T` | _TBD_ | does new tab inherit domain? |
| 6 | Copy mode | `Cmd+Shift+X` | _TBD_ | can you yank text? |
| 7 | Search | `Cmd+F`, type a substring | _TBD_ | does scrollback respond? |
| 8 | Scrollback walk | `Cmd+Shift+PageUp` after lots of `cat` echoes | _TBD_ | |
| 9 | Mouse selection | drag to highlight | _TBD_ | clipboard works? |
| 10 | Focus events | switch wezterm in/out of focus | _TBD_ | any event surface diff? |
| 11 | Pane close | `Cmd+W` | _TBD_ | does cat get SIGTERM? does wezterm clean up? |
| 12 | Domain re-attach | `wezterm.action.DetachDomain "agent"` then re-attach | _TBD_ | |

If a feature is `FAIL` or `PARTIAL`, capture:
- The error message (terminal output and `RUST_LOG=trace cargo run` log).
- The line in `mux/` or `wezterm-gui/` that special-cases LocalDomain or LocalPane.
- A proposed fix (shim in `wezterm_compat`? upstream patch? accept the limitation?).

## Recording the outcome

Fill in this section after running the spike. Replace `_TBD_` markers.

### Outcome

- Date run: _TBD_
- wezterm tag tested: _TBD_
- macOS / Linux: _TBD_
- Time taken: _TBD_

(see the table above for per-feature outcomes)

### Verdict

- **PASS** (all 12 features work or have trivial fixes): patch budget holds at <500 LoC. AgentDomain proceeds in Phase 3.
- **PARTIAL** (1–3 features need a shim): document the shim in `crates/milliways-term/milliways/src/wezterm_compat/` design notes. Patch budget grows to ~1000 LoC. Decision 11 (compat shim) gets concrete shape.
- **FAIL** (>3 features broken or any blocker affects MVP keybindings):
  - Reconsider the pane abstraction. AgentDomain might need to stay closer to LocalDomain by actually spawning a thin "agent shim" subprocess that talks to milliwaysd over UDS. The pane is then a real-PTY pane backed by the shim process; the shim is what bridges to milliwaysd. This adds a process-per-pane but keeps the wezterm side simple.
  - Update `design.md` Decision 2 with the new shape.
  - Update `tasks.md` TASK-3.2 to spawn the shim instead of doing virtual PTY work in-process.

## What this spike does NOT cover

- Performance — the spike is functional, not a benchmark.
- Multiple panes simultaneously under heavy throughput — single pane only.
- Daemon integration — bytes come from `cat`, not milliwaysd. Daemon-shaped issues (UDS reconnect, replay) are tested in Phase 3.
- Kitty graphics rendering — that's TASK-0.3.

## References

- Wezterm `Domain` trait: `mux/src/domain.rs` in upstream.
- Wezterm `LocalPane`: `mux/src/localpane.rs` (copy as a template).
- `portable-pty` crate: https://docs.rs/portable-pty/ — already a wezterm dependency.
- Architect review (REVIEW.md), risk 3, for the failure-mode discussion.
