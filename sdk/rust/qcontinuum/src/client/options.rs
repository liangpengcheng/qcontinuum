//! 客户端配置。

use std::time::Duration;

/// 传输类型。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TransportKind {
    Tcp,
    Kcp,
    WebSocket,
}

/// QClient 配置。
#[derive(Debug, Clone)]
pub struct QClientOptions {
    /// 传输类型。
    pub transport: TransportKind,
    /// 服务器地址。
    pub host: String,
    /// 服务器端口。
    pub port: u16,
    /// 建连超时。
    pub connect_timeout: Duration,
    /// 最大重连次数。0 = 无限重连。
    pub max_reconnect_attempts: u32,
    /// 重连基础延迟（指数退避起点）。
    pub reconnect_base_delay: Duration,
    /// 重连最大延迟（指数退避上限）。
    pub reconnect_max_delay: Duration,
    /// 重连抖动比例（0.0-1.0）。
    pub reconnect_jitter: f64,
}

impl Default for QClientOptions {
    fn default() -> Self {
        Self {
            transport: TransportKind::Tcp,
            host: "127.0.0.1".into(),
            port: 9001,
            connect_timeout: Duration::from_secs(10),
            max_reconnect_attempts: 0, // 无限
            reconnect_base_delay: Duration::from_millis(500),
            reconnect_max_delay: Duration::from_secs(30),
            reconnect_jitter: 0.2,
        }
    }
}
