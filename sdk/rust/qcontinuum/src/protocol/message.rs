//! 协议消息：QMessage + protobuf 编解码。

use prost::Message as ProstMessage;

use super::error::{QError, QErrorKind, Result};

/// 一条解帧后的协议消息。
#[derive(Debug, Clone)]
pub struct QMessage {
    /// 消息号（如 2001 = ECHO）。
    pub id: i32,
    /// protobuf body。
    pub body: Vec<u8>,
}

impl QMessage {
    pub fn new(id: i32, body: Vec<u8>) -> Self {
        Self { id, body }
    }

    /// 把 body 解析为 protobuf 消息。
    pub fn unpack<T: ProstMessage + Default>(&self) -> Result<T> {
        T::decode(self.body.as_slice()).map_err(|e| {
            QError::with_source(
                QErrorKind::ProtocolViolation,
                format!("invalid protobuf body for id {}: {e}", self.id),
                e,
            )
        })
    }
}

/// 把 protobuf 消息打包为 (id, body)。
pub fn pack<T: ProstMessage>(id: i32, msg: &T) -> (i32, Vec<u8>) {
    (id, msg.encode_to_vec())
}

/// 消息号常量（与 proto/test.proto 的 MsgId 对齐）。
pub mod msg_id {
    pub const ECHO: i32 = 2001;
    pub const HEARTBEAT: i32 = 2002;
}
