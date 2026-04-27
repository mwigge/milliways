//! Ping the milliwaysd daemon over UDS. Smoke test for the Rust ↔ Go wire.
//!
//! Usage: `cargo run --example ping -p milliways -- [socket-path]`
//! With no argument, uses the default socket path.

use anyhow::Result;

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<()> {
    let socket = std::env::args()
        .nth(1)
        .map(std::path::PathBuf::from)
        .or_else(milliways::rpc::default_socket_path)
        .ok_or_else(|| anyhow::anyhow!("no socket path; set XDG_RUNTIME_DIR or HOME, or pass an argument"))?;

    eprintln!("dialing {}", socket.display());
    let mut client = milliways::rpc::Client::dial(&socket).await?;
    let result: milliways::rpc::PingResult = client.call("ping", ()).await?;
    println!(
        "pong={} version={} uptime_s={:.3} proto={}.{}",
        result.pong, result.version, result.uptime_s, result.proto.major, result.proto.minor
    );
    Ok(())
}
