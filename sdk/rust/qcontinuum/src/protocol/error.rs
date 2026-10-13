//! 错误分类，对标 C# SDK 的 `QErrorKind`。

use thiserror::Error;

/// 错误种类。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum QErrorKind {
    /// 连接被拒绝（对端无监听）。
    ConnectionRefused,
    /// 主机不可达。
    HostUnreachable,
    /// 建连超时。
    ConnectTimeout,
    /// IO 超时（读写）。
    IoTimeout,
    /// 协议违规（坏帧头、非法 protobuf 等）。
    ProtocolViolation,
    /// 消息超过上限（100 MiB）。
    MessageTooLarge,
    /// 对端关闭连接。
    RemoteClosed,
    /// 发送失败。
    SendFailed,
    /// 重连次数耗尽。
    ReconnectExhausted,
    /// 操作被取消。
    Cancelled,
    /// 未连接时调用需要连接的方法。
    NotConnected,
}

/// qcontinuum SDK 错误。
#[derive(Debug, Error)]
#[error("[{kind:?}] {message}")]
pub struct QError {
    pub kind: QErrorKind,
    pub message: String,
    #[source]
    pub source: Option<Box<dyn std::error::Error + Send + Sync>>,
}

impl QError {
    pub fn new(kind: QErrorKind, message: impl Into<String>) -> Self {
        Self {
            kind,
            message: message.into(),
            source: None,
        }
    }

    pub fn with_source(
        kind: QErrorKind,
        message: impl Into<String>,
        source: impl std::error::Error + Send + Sync + 'static,
    ) -> Self {
        Self {
            kind,
            message: message.into(),
            source: Some(Box::new(source)),
        }
    }

    pub fn cancelled() -> Self {
        Self::new(QErrorKind::Cancelled, "operation cancelled")
    }

    pub fn not_connected() -> Self {
        Self::new(QErrorKind::NotConnected, "not connected")
    }
}

/// 本 crate 的 Result 别名。
pub type Result<T> = std::result::Result<T, QError>;

/// 把 io::Error 映射到 QErrorKind。
pub fn map_io_error(e: std::io::Error) -> QError {
    use std::io::ErrorKind as K;
    let kind = match e.kind() {
        K::ConnectionRefused => QErrorKind::ConnectionRefused,
        K::HostUnreachable | K::NetworkUnreachable => QErrorKind::HostUnreachable,
        K::TimedOut => QErrorKind::IoTimeout,
        K::UnexpectedEof | K::ConnectionReset | K::ConnectionAborted | K::BrokenPipe => {
            QErrorKind::RemoteClosed
        }
        _ => QErrorKind::SendFailed,
    };
    QError::with_source(kind, e.to_string(), e)
}
