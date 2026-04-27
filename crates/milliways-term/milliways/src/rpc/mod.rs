//! JSON-RPC 2.0 client to milliwaysd. Newline-delimited (NDJSON) framing
//! per term-daemon-rpc/spec.md, Decision 3.
//!
//! The wire-format types live in `types`; the client is in `client`.
//!
//! Long-term, `types` will be regenerated from `proto/milliways.json` via
//! `typify` invoked from a `build.rs` step. Until that lands in Phase 1,
//! types are hand-mirrored here.

pub mod client;
pub mod types;

pub use client::{default_socket_path, Client, RpcError};
pub use types::{PingResult, ProtoVersion};
