//! 8 字节小端帧头编解码，与 Go 服务端严格对齐。
//!
//! ```text
//! [Length: int32 LE][ID: int32 LE][protobuf body]
//! ```
//! - 头固定 8 字节，`Length` 仅表示 body 长度（不含头）。
//! - 单包 body 上限 100 MiB（与 Go 服务端一致）。

/// 帧头长度（字节）。
pub const HEADER_LEN: usize = 8;
/// 单包 body 上限：100 MiB。
pub const MAX_MESSAGE_SIZE: i32 = 100 * 1024 * 1024;

use super::error::{QError, QErrorKind, Result};

/// 把 (id, body) 编码为完整协议包。
pub fn encode(id: i32, body: &[u8]) -> Result<Vec<u8>> {
    if body.len() as i64 > MAX_MESSAGE_SIZE as i64 {
        return Err(QError::new(
            QErrorKind::MessageTooLarge,
            format!("body {} bytes exceeds {} bytes", body.len(), MAX_MESSAGE_SIZE),
        ));
    }
    let mut pkt = Vec::with_capacity(HEADER_LEN + body.len());
    pkt.extend_from_slice(&(body.len() as i32).to_le_bytes());
    pkt.extend_from_slice(&id.to_le_bytes());
    pkt.extend_from_slice(body);
    Ok(pkt)
}

/// 尝试从缓冲区头部解析帧头。返回 `None` 表示数据不足 8 字节。
pub fn try_read_header(buf: &[u8]) -> Result<Option<(i32, i32)>> {
    if buf.len() < HEADER_LEN {
        return Ok(None);
    }
    let length = i32::from_le_bytes(buf[0..4].try_into().unwrap());
    let id = i32::from_le_bytes(buf[4..8].try_into().unwrap());
    if length < 0 {
        return Err(QError::new(
            QErrorKind::ProtocolViolation,
            format!("negative frame length: {length}"),
        ));
    }
    if length > MAX_MESSAGE_SIZE {
        return Err(QError::new(
            QErrorKind::MessageTooLarge,
            format!("frame length {length} exceeds {MAX_MESSAGE_SIZE}"),
        ));
    }
    Ok(Some((length, id)))
}

/// 流式组包器：处理粘包 / 半包 / 跨包。
#[derive(Default)]
pub struct PacketReader {
    buf: Vec<u8>,
}

impl PacketReader {
    pub fn new() -> Self {
        Self::default()
    }

    /// 喂入新收到的数据。
    pub fn feed(&mut self, data: &[u8]) {
        self.buf.extend_from_slice(data);
    }

    /// 尝试取出一个完整包。返回 `Ok(None)` 表示数据不足。
    /// 帧头非法时返回 `Err`（调用方应关闭连接）。
    pub fn try_take(&mut self) -> Result<Option<(i32, Vec<u8>)>> {
        let (length, id) = match try_read_header(&self.buf)? {
            Some(h) => h,
            None => return Ok(None),
        };
        let total = HEADER_LEN + length as usize;
        if self.buf.len() < total {
            return Ok(None);
        }
        let body = self.buf[HEADER_LEN..total].to_vec();
        self.buf.drain(..total);
        Ok(Some((id, body)))
    }

    /// 清空内部缓冲（重连时调用，丢弃半包）。
    pub fn reset(&mut self) {
        self.buf.clear();
    }

    pub fn buffered_len(&self) -> usize {
        self.buf.len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn header_layout_matches_go_little_endian() {
        let pkt = encode(2001, &[0x08, 0x2A]).unwrap();
        assert_eq!(pkt.len(), 8 + 2);
        // length = 2 (LE)
        assert_eq!(&pkt[0..4], &[0x02, 0x00, 0x00, 0x00]);
        // id = 2001 = 0x7D1 (LE)
        assert_eq!(&pkt[4..8], &[0xD1, 0x07, 0x00, 0x00]);
        assert_eq!(&pkt[8..], &[0x08, 0x2A]);
    }

    #[test]
    fn packet_reader_single_packet() {
        let mut r = PacketReader::new();
        let pkt = encode(2001, b"hello").unwrap();
        r.feed(&pkt);
        let (id, body) = r.try_take().unwrap().unwrap();
        assert_eq!(id, 2001);
        assert_eq!(body, b"hello");
        assert!(r.try_take().unwrap().is_none());
    }

    #[test]
    fn packet_reader_split_across_feeds() {
        let mut r = PacketReader::new();
        let pkt = encode(2001, b"world").unwrap();
        r.feed(&pkt[..3]);
        assert!(r.try_take().unwrap().is_none());
        r.feed(&pkt[3..7]);
        assert!(r.try_take().unwrap().is_none());
        r.feed(&pkt[7..]);
        let (id, body) = r.try_take().unwrap().unwrap();
        assert_eq!(id, 2001);
        assert_eq!(body, b"world");
    }

    #[test]
    fn packet_reader_coalesced_packets() {
        let mut r = PacketReader::new();
        let p1 = encode(2001, b"a").unwrap();
        let p2 = encode(2002, b"bb").unwrap();
        let mut both = p1.clone();
        both.extend_from_slice(&p2);
        r.feed(&both);
        let (id1, b1) = r.try_take().unwrap().unwrap();
        let (id2, b2) = r.try_take().unwrap().unwrap();
        assert_eq!((id1, b1), (2001, b"a".to_vec()));
        assert_eq!((id2, b2), (2002, b"bb".to_vec()));
    }

    #[test]
    fn packet_reader_empty_body() {
        let mut r = PacketReader::new();
        r.feed(&encode(2001, &[]).unwrap());
        let (id, body) = r.try_take().unwrap().unwrap();
        assert_eq!(id, 2001);
        assert!(body.is_empty());
    }

    #[test]
    fn packet_reader_negative_length_is_protocol_violation() {
        let mut r = PacketReader::new();
        r.feed(&[0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x00, 0x00, 0x00]);
        let err = r.try_take().unwrap_err();
        assert_eq!(err.kind, QErrorKind::ProtocolViolation);
    }

    #[test]
    fn packet_reader_oversize_is_rejected() {
        let mut r = PacketReader::new();
        let mut hdr = [0u8; 8];
        hdr[0..4].copy_from_slice(&(MAX_MESSAGE_SIZE + 1).to_le_bytes());
        r.feed(&hdr);
        let err = r.try_take().unwrap_err();
        assert_eq!(err.kind, QErrorKind::MessageTooLarge);
    }

    #[test]
    fn encode_oversize_is_rejected() {
        let big = vec![0u8; MAX_MESSAGE_SIZE as usize + 1];
        let err = encode(1, &big).unwrap_err();
        assert_eq!(err.kind, QErrorKind::MessageTooLarge);
    }
}
