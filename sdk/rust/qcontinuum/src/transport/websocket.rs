//! WebSocket 传输：`tokio-tungstenite`，固定路径 `/ws`，二进制消息。

use std::time::Duration;
use futures_util::{SinkExt, StreamExt};
use tokio::time::timeout;
use tokio_tungstenite::{connect_async, tungstenite::Message, MaybeTlsStream, WebSocketStream};

use super::Transport;
use crate::protocol::error::{map_io_error, QError, QErrorKind};
use crate::protocol::Result;

type WsStream = WebSocketStream<MaybeTlsStream<tokio::net::TcpStream>>;

/// WebSocket 传输。
pub struct WebSocketTransport {
    stream: Option<WsStream>,
}

impl WebSocketTransport {
    pub fn new() -> Self {
        Self { stream: None }
    }
}

impl Default for WebSocketTransport {
    fn default() -> Self {
        Self::new()
    }
}

fn map_ws_error(e: tokio_tungstenite::tungstenite::Error) -> QError {
    use tokio_tungstenite::tungstenite::Error as E;
    let kind = match e {
        E::ConnectionClosed | E::AlreadyClosed => QErrorKind::RemoteClosed,
        E::Io(io) => return map_io_error(io),
        _ => QErrorKind::SendFailed,
    };
    QError::new(kind, e.to_string())
}

impl Transport for WebSocketTransport {
    async fn connect(&mut self, host: &str, port: u16, timeout_dur: Duration) -> Result<()> {
        let url = format!("ws://{host}:{port}/ws");
        let (stream, _) = timeout(timeout_dur, connect_async(&url))
            .await
            .map_err(|_| {
                QError::new(
                    QErrorKind::ConnectTimeout,
                    format!("websocket connect to {url} timed out"),
                )
            })?
            .map_err(map_ws_error)?;
        self.stream = Some(stream);
        Ok(())
    }

    async fn send(&mut self, packet: &[u8]) -> Result<()> {
        let stream = self.stream.as_mut().ok_or_else(QError::not_connected)?;
        stream
            .send(Message::Binary(packet.to_vec()))
            .await
            .map_err(map_ws_error)?;
        Ok(())
    }

    async fn recv(&mut self) -> Result<Option<Vec<u8>>> {
        let stream = self.stream.as_mut().ok_or_else(QError::not_connected)?;
        // 聚合分片，直到收到完整消息
        let mut assembled: Vec<u8> = Vec::new();
        loop {
            match stream.next().await {
                None => return Ok(None),
                Some(Err(e)) => return Err(map_ws_error(e)),
                Some(Ok(Message::Binary(data))) => {
                    assembled.extend_from_slice(&data);
                    // tungstenite 已处理分片重组，Binary 即完整消息
                    return Ok(Some(assembled));
                }
                Some(Ok(Message::Close(_))) => return Ok(None),
                Some(Ok(_)) => continue, // Ping/Pong/Text 忽略
            }
        }
    }

    async fn close(&mut self) -> Result<()> {
        if let Some(mut s) = self.stream.take() {
            let _ = s.close(None).await;
        }
        Ok(())
    }
}
