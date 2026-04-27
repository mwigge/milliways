//! `SharedChild` — a `portable_pty::Child` wrapper that lets the
//! reconnect watcher swap the underlying child process when the bridge
//! is re-spawned.
//!
//! `LocalPane` consumes a `Box<dyn Child + Send>` by value. Once handed
//! over, the pane owns the child and the watcher would have no way to
//! observe exits or replace the child after a reconnect. We work around
//! this by wrapping the original child in a thin `SharedChild` whose
//! inner `Box<dyn Child>` is behind an `Arc<parking_lot::Mutex<…>>`. The
//! same `Arc` is held by the watcher; on a successful reconnect the
//! watcher swaps the inner Box for the freshly spawned child, so
//! LocalPane's view (e.g. `kill`, `try_wait`) transparently follows the
//! current process.
//!
//! Concurrency:
//!
//!   - `try_wait` is called from both LocalPane (every render frame, in
//!     practice) and the watcher (every 250 ms tick).
//!   - `wait` blocks; we forward to the inner child but warn that this
//!     will pin the lock until the inner child exits. LocalPane's
//!     `wait_for_child` typically runs on a dedicated thread, so this
//!     is acceptable.
//!   - `replace` is only called by the watcher, after a fresh
//!     `spawn_command`, when the FSM transitions from Reconnecting →
//!     Connected.

use parking_lot::Mutex;
use portable_pty::{Child, ChildKiller, ExitStatus};
use std::io::Result as IoResult;
use std::sync::Arc;

/// Shared, swappable handle to a bridge subprocess.
#[derive(Clone, Debug)]
pub struct SharedChild {
    inner: Arc<Mutex<Box<dyn Child + Send>>>,
}

impl SharedChild {
    pub fn new(child: Box<dyn Child + Send>) -> Self {
        Self {
            inner: Arc::new(Mutex::new(child)),
        }
    }

    /// Replace the inner child with a freshly spawned one. Caller is
    /// responsible for ensuring the previous child has actually exited
    /// (the FSM only calls this after `try_wait_exited` returned true
    /// AND a successful reconnect).
    pub fn replace(&self, new_child: Box<dyn Child + Send + Sync>) {
        // SlavePty::spawn_command returns Box<dyn Child + Send + Sync>,
        // which coerces to Box<dyn Child + Send> (we don't need Sync
        // inside the Mutex).
        let coerced: Box<dyn Child + Send> = new_child;
        *self.inner.lock() = coerced;
    }

    /// Returns true if the inner child has already exited. Non-blocking.
    pub fn try_wait_exited(&self) -> bool {
        let mut guard = self.inner.lock();
        match guard.try_wait() {
            Ok(Some(_)) => true,
            Ok(None) => false,
            Err(err) => {
                log::warn!("milliways: try_wait on bridge child failed: {err}");
                // Treat errors conservatively as "not exited" so we
                // don't spam reconnects.
                false
            }
        }
    }
}

/// Box-newtype handed to `LocalPane::new`. Holds a clone of the same
/// `Arc` so LocalPane and the watcher see the same underlying child.
#[derive(Debug)]
pub struct BridgeChild {
    shared: SharedChild,
}

impl BridgeChild {
    pub fn new(shared: SharedChild) -> Self {
        Self { shared }
    }
}

impl Child for BridgeChild {
    fn try_wait(&mut self) -> IoResult<Option<ExitStatus>> {
        self.shared.inner.lock().try_wait()
    }

    fn wait(&mut self) -> IoResult<ExitStatus> {
        // NOTE: pinning the lock for the duration of wait() means a
        // concurrent try_wait() would block. In practice LocalPane's
        // wait_for_child path is the only blocking caller and it runs
        // on a dedicated thread; the watcher only ever uses try_wait.
        self.shared.inner.lock().wait()
    }

    fn process_id(&self) -> Option<u32> {
        self.shared.inner.lock().process_id()
    }

    #[cfg(windows)]
    fn as_raw_handle(&self) -> Option<std::os::windows::io::RawHandle> {
        self.shared.inner.lock().as_raw_handle()
    }
}

impl ChildKiller for BridgeChild {
    fn kill(&mut self) -> IoResult<()> {
        self.shared.inner.lock().kill()
    }

    fn clone_killer(&self) -> Box<dyn ChildKiller + Send + Sync> {
        // Delegate clone_killer to the inner child. The returned killer
        // targets *the current* inner child only; if the watcher swaps
        // the child later, prior killers retain their original target.
        // That's fine: clone_killer is used to send SIGKILL on tab
        // close, which races with reconnect either way.
        self.shared.inner.lock().clone_killer()
    }
}
