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
pub trait Transport: Send {
    /// 建立连接。`timeout` 为建连超时。
    fn connect(
        &mut self,
        host: &str,
        port: u16,
        timeout: Duration,
    ) -> impl std::future::Future<Output = Result<()>> + Send;

    /// 发送一个完整协议包（已组帧）。
    fn send(
        &mut self,
        packet: &[u8],
    ) -> impl std::future::Future<Output = Result<()>> + Send;

    /// 接收下一批字节。`Ok(None)` 表示对端已关闭。
    fn recv(&mut self) -> impl std::future::Future<Output = Result<Option<Vec<u8>>>> + Send;

    /// 关闭连接。
    fn close(&mut self) -> impl std::future::Future<Output = Result<()>> + Send;
}
