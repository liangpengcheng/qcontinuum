//! 协议层：错误、帧编解码、消息。

pub mod error;
pub mod framing;
pub mod message;

pub use error::{QError, QErrorKind, Result};
pub use framing::{PacketReader, HEADER_LEN, MAX_MESSAGE_SIZE};
pub use message::{msg_id, pack, QMessage};
