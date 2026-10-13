//! TCP 传输：`tokio::net::TcpStream`，开启 `nodelay`。

use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::time::timeout;

use super::Transport;
use crate::protocol::error::{map_io_error, QError, QErrorKind};
use crate::protocol::Result;

/// TCP 传输。
pub struct TcpTransport {
    stream: Option<TcpStream>,
}

impl TcpTransport {
    pub fn new() -> Self {
        Self { stream: None }
    }
}

impl Default for TcpTransport {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait::async_trait]
impl Transport for TcpTransport {
    async fn connect(&mut self, host: &str, port: u16, timeout_dur: Duration) -> Result<()> {
        let addr = format!("{host}:{port}");
        let stream = timeout(timeout_dur, TcpStream::connect(&addr))
            .await
            .map_err(|_| {
                QError::new(
                    QErrorKind::ConnectTimeout,
                    format!("tcp connect to {addr} timed out"),
                )
            })?
            .map_err(map_io_error)?;
        stream.set_nodelay(true).map_err(map_io_error)?;
        self.stream = Some(stream);
        Ok(())
    }

    async fn send(&mut self, packet: &[u8]) -> Result<()> {
        let stream = self.stream.as_mut().ok_or_else(QError::not_connected)?;
        stream.write_all(packet).await.map_err(map_io_error)?;
        Ok(())
    }

    async fn recv(&mut self) -> Result<Option<Vec<u8>>> {
        let stream = self.stream.as_mut().ok_or_else(QError::not_connected)?;
        let mut buf = vec![0u8; 64 * 1024];
        match stream.read(&mut buf).await {
            Ok(0) => Ok(None), // 对端关闭
            Ok(n) => {
                buf.truncate(n);
                Ok(Some(buf))
            }
            Err(e) => Err(map_io_error(e)),
        }
    }

    async fn close(&mut self) -> Result<()> {
        if let Some(mut s) = self.stream.take() {
            let _ = s.shutdown().await;
        }
        Ok(())
    }
}
