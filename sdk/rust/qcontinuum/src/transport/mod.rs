//! 传输层：TCP / WebSocket / KCP。
//!
//! 所有传输都暴露统一的异步接口：`connect` / `send` / `recv` / `close`。
//! `recv` 返回从对端收到的下一批原始字节；返回 `Ok(None)` 表示连接已关闭。

pub mod kcp;
pub mod tcp;
pub mod websocket;

use std::time::Duration;

use super::protocol::Result;

pub use kcp::KcpTransport;
pub use tcp::TcpTransport;
pub use websocket::WebSocketTransport;

/// 传输抽象。`Send` 以便在 tokio 任务间移动。
#[async_trait::async_trait]
pub trait Transport: Send {
    /// 建立连接。`timeout` 为建连超时。
    async fn connect(&mut self, host: &str, port: u16, timeout: Duration) -> Result<()>;

    /// 发送一个完整协议包（已组帧）。
    async fn send(&mut self, packet: &[u8]) -> Result<()>;

    /// 接收下一批字节。`Ok(None)` 表示对端已关闭。
    async fn recv(&mut self) -> Result<Option<Vec<u8>>>;

    /// 关闭连接。
    async fn close(&mut self) -> Result<()>;
}
