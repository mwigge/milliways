//! Subscribe to status.subscribe and print events for `n` seconds.
//!
//! Usage: `cargo run --example subscribe_status -p milliways -- [socket] [seconds]`

use anyhow::Result;
use std::time::Duration;

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().collect();
    let socket = args
        .get(1)
        .map(std::path::PathBuf::from)
        .or_else(milliways::rpc::default_socket_path)
        .ok_or_else(|| anyhow::anyhow!("no socket path"))?;
    let seconds: u64 = args.get(2).and_then(|s| s.parse().ok()).unwrap_or(4);

    eprintln!("subscribing on {} for {}s", socket.display(), seconds);
    let mut client = milliways::rpc::Client::dial(&socket).await?;
    let mut sub = client.subscribe("status.subscribe", ()).await?;
    let deadline = tokio::time::Instant::now() + Duration::from_secs(seconds);
    let mut count = 0;
    loop {
        tokio::select! {
            _ = tokio::time::sleep_until(deadline) => break,
            ev = sub.rx.recv() => match ev {
                Some(line) => {
                    count += 1;
                    println!("{}", String::from_utf8_lossy(&line));
                }
                None => break,
            }
        }
    }
    eprintln!("received {} events", count);
    Ok(())
}
