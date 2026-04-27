//! Newline-delimited JSON-RPC 2.0 client. One in-flight call at a time per
//! `Client` — concurrent callers should dial more clients.

use anyhow::{anyhow, Context, Result};
use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicI64, Ordering};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::UnixStream;
use tokio::sync::mpsc;

/// Resolve the default UDS path:
/// `${XDG_RUNTIME_DIR:-$HOME/.local/state/milliways}/sock`.
pub fn default_socket_path() -> Option<PathBuf> {
    if let Some(x) = std::env::var_os("XDG_RUNTIME_DIR") {
        return Some(PathBuf::from(x).join("milliways").join("sock"));
    }
    let home = std::env::var_os("HOME")?;
    Some(
        PathBuf::from(home)
            .join(".local")
            .join("state")
            .join("milliways")
            .join("sock"),
    )
}

/// JSON-RPC 2.0 error returned by the daemon. The numeric code maps to the
/// catalogue in term-daemon-rpc/spec.md.
#[derive(Debug, Clone, Serialize, Deserialize, thiserror::Error)]
#[error("rpc error {code}: {message}")]
pub struct RpcError {
    pub code: i32,
    pub message: String,
}

#[derive(Debug, Serialize)]
struct WireRequest<'a, P: Serialize> {
    jsonrpc: &'a str,
    method: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    params: Option<P>,
    id: i64,
}

#[derive(Debug, Deserialize)]
struct WireResponse<R> {
    #[serde(rename = "jsonrpc")]
    _jsonrpc: String,
    result: Option<R>,
    error: Option<RpcError>,
    #[serde(rename = "id")]
    _id: Option<i64>,
}

/// JSON-RPC 2.0 client over a Unix domain socket.
pub struct Client {
    socket: PathBuf,
    reader: BufReader<tokio::net::unix::OwnedReadHalf>,
    writer: tokio::net::unix::OwnedWriteHalf,
    next_id: AtomicI64,
    line_buf: String,
}

/// SubscribeResp is the unary response shape of any *.subscribe-style
/// method: the daemon allocates a stream_id and a starting offset.
#[derive(Debug, Deserialize)]
struct SubscribeResp {
    stream_id: i64,
    output_offset: i64,
}

/// Subscription is the receiver end of an open server-pushed stream.
/// Each item is one NDJSON line (without the trailing newline). The
/// channel closes when the daemon ends the stream or the sidecar drops.
pub struct Subscription {
    pub rx: mpsc::Receiver<Vec<u8>>,
}

impl Client {
    /// Path of the socket this Client is connected to. Used to dial the
    /// sidecar against the same UDS.
    pub fn socket(&self) -> &Path {
        &self.socket
    }

    /// Dial the milliwaysd UDS at `socket`.
    pub async fn dial(socket: impl AsRef<Path>) -> Result<Self> {
        let path = socket.as_ref().to_path_buf();
        let stream = UnixStream::connect(&path)
            .await
            .with_context(|| format!("dial {}", path.display()))?;
        let (rd, wr) = stream.into_split();
        Ok(Self {
            socket: path,
            reader: BufReader::new(rd),
            writer: wr,
            next_id: AtomicI64::new(0),
            line_buf: String::new(),
        })
    }

    /// Subscribe to a *.subscribe-style method. Internally:
    ///   1. Calls the method to allocate a stream_id.
    ///   2. Dials a second UDS connection.
    ///   3. Writes a `STREAM <id> <offset>\n` preamble.
    ///   4. Spawns a tokio task that reads NDJSON lines and forwards them
    ///      via mpsc.
    /// The returned Subscription's receiver yields one Vec<u8> per event.
    pub async fn subscribe<P>(&mut self, method: &str, params: P) -> Result<Subscription>
    where
        P: Serialize,
    {
        let resp: SubscribeResp = self.call(method, params).await?;
        let mut sidecar = UnixStream::connect(&self.socket)
            .await
            .with_context(|| format!("dial sidecar {}", self.socket.display()))?;
        let preamble = format!("STREAM {} {}\n", resp.stream_id, resp.output_offset);
        sidecar
            .write_all(preamble.as_bytes())
            .await
            .context("write STREAM preamble")?;
        sidecar.flush().await.ok();

        let (tx, rx) = mpsc::channel::<Vec<u8>>(16);
        tokio::spawn(async move {
            let mut reader = BufReader::new(sidecar);
            let mut buf = String::new();
            loop {
                buf.clear();
                match reader.read_line(&mut buf).await {
                    Ok(0) => break, // EOF
                    Ok(_) => {
                        let line = buf.trim_end_matches('\n').as_bytes().to_vec();
                        if tx.send(line).await.is_err() {
                            break;
                        }
                    }
                    Err(_) => break,
                }
            }
        });
        Ok(Subscription { rx })
    }

    /// Invoke `method` with `params` and decode the result. Use `()` for
    /// methods that take no parameters.
    pub async fn call<P, R>(&mut self, method: &str, params: P) -> Result<R>
    where
        P: Serialize,
        R: DeserializeOwned,
    {
        let id = self.next_id.fetch_add(1, Ordering::Relaxed) + 1;
        let req = WireRequest {
            jsonrpc: "2.0",
            method,
            params: Some(params),
            id,
        };
        let mut line = serde_json::to_vec(&req).context("encode request")?;
        line.push(b'\n');
        self.writer
            .write_all(&line)
            .await
            .context("write request")?;
        self.writer.flush().await.context("flush request")?;

        self.line_buf.clear();
        let n = self
            .reader
            .read_line(&mut self.line_buf)
            .await
            .context("read response")?;
        if n == 0 {
            return Err(anyhow!("connection closed by peer"));
        }
        let resp: WireResponse<R> =
            serde_json::from_str(self.line_buf.trim_end()).context("decode response")?;
        if let Some(e) = resp.error {
            return Err(anyhow::Error::from(e));
        }
        resp.result
            .ok_or_else(|| anyhow!("response missing both result and error"))
    }
}
