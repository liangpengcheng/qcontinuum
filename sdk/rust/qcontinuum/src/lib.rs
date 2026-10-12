//! qcontinuum Rust 客户端 SDK。
//!
//! 覆盖 TCP / KCP / WebSocket 三传输，protobuf 协议，带错误分类与断线重连。
//!
//! ```ignore
//! use qcontinuum::{QClient, QClientOptions, TransportKind};
//!
//! #[tokio::main]
//! async fn main() -> qcontinuum::Result<()> {
//!     let client = QClient::connect(QClientOptions {
//!         transport: TransportKind::Tcp,
//!         host: "127.0.0.1".into(),
//!         port: 9001,
//!         ..Default::default()
//!     })
//!     .await?;
//!     // ...
//!     Ok(())
//! }
//! ```

pub mod client;
pub mod protocol;
pub mod transport;

/// prost 生成的测试协议（proto/test.proto）。
pub mod test_proto {
    include!(concat!(env!("OUT_DIR"), "/qcontinuum.test.rs"));
}

pub use protocol::{QError, QErrorKind, Result};
