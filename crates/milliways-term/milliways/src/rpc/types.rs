//! RPC message shapes — generated from `proto/milliways.json` by `build.rs`
//! via the `typify` crate. The generated file lives in `$OUT_DIR/generated.rs`.
//!
//! If the build-time codegen fails (e.g., proxy issues), `build.rs` writes
//! an empty placeholder file and a `cargo:warning=` is surfaced. In that
//! degraded mode this file compiles but no types are visible — bring back
//! the hand-mirrored types here as a fallback if needed.

#![allow(clippy::all, dead_code)] // typify-generated code: lints and dead_code do not apply to codegen output

include!(concat!(env!("OUT_DIR"), "/generated.rs"));
