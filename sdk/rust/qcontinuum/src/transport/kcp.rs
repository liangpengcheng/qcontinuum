//! KCP 传输：`kcp` crate + `tokio::net::UdpSocket`。
//!
//! 参数对齐 Go `DefaultKCPConfig`：`nodelay(1, 10, 2, 1)`，窗口 4096/4096，
//! MTU 1400，message 模式。conv 随机生成（对标 kcp-go 客户端行为）。

use std::io::{self, Write};
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tokio::net::UdpSocket;
use tokio::time::{interval, timeout};

use super::Transport;
use crate::protocol::error::{map_io_error, QError, QErrorKind};
use crate::protocol::Result;

/// KCP 的 output：写到共享 buffer，由传输层取走经 UDP 发出。
#[derive(Clone, Default)]
struct KcpOutput {
    buf: Arc<Mutex<Vec<u8>>>,
}

impl Write for KcpOutput {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.buf.lock().unwrap().extend_from_slice(buf);
        Ok(buf.len())
    }
    fn flush(&mut self) -> io::Result<()> {
        Ok(())
    }
}

impl KcpOutput {
    fn take(&self) -> Vec<u8> {
        std::mem::take(&mut *self.buf.lock().unwrap())
    }
}

/// KCP 传输。
pub struct KcpTransport {
    inner: Option<KcpInner>,
}

struct KcpInner {
    socket: Arc<UdpSocket>,
    kcp: Arc<Mutex<kcp::Kcp<KcpOutput>>>,
    output: KcpOutput,
    update_handle: tokio::task::JoinHandle<()>,
}

impl KcpTransport {
    pub fn new() -> Self {
        Self { inner: None }
    }

    fn now_ms() -> u32 {
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_millis() as u32
    }
}

impl Default for KcpTransport {
    fn default() -> Self {
        Self::new()
    }
}

impl Transport for KcpTransport {
    async fn connect(&mut self, host: &str, port: u16, timeout_dur: Duration) -> Result<()> {
        let addr = format!("{host}:{port}");
        let socket = timeout(timeout_dur, async {
            let s = UdpSocket::bind("0.0.0.0:0").await.map_err(map_io_error)?;
            s.connect(&addr).await.map_err(map_io_error)?;
            Ok::<_, QError>(s)
        })
        .await
        .map_err(|_| {
            QError::new(
                QErrorKind::ConnectTimeout,
                format!("kcp connect to {addr} timed out"),
            )
        })??;

        let socket = Arc::new(socket);
        let conv: u32 = rand::random();
        let output = KcpOutput::default();
        let kcp = Arc::new(Mutex::new({
            let mut k = kcp::Kcp::new(conv, output.clone());
            k.set_nodelay(true, 10, 2, true);
            k.set_wndsize(4096, 4096);
            k.set_mtu(1400).map_err(|e| {
                QError::new(QErrorKind::ProtocolViolation, format!("kcp set_mtu: {e}"))
            })?;
            k
        }));

        // 后台 update 循环：每 10ms 驱动一次 KCP
        let kcp_clone = kcp.clone();
        let socket_clone = socket.clone();
        let output_clone = output.clone();
        let update_handle = tokio::spawn(async move {
            let mut ticker = interval(Duration::from_millis(10));
            loop {
                ticker.tick().await;
                {
                    let mut k = kcp_clone.lock().unwrap();
                    let _ = k.update(Self::now_ms());
                }
                let out = output_clone.take();
                if !out.is_empty() {
                    let _ = socket_clone.send(&out).await;
                }
            }
        });

        self.inner = Some(KcpInner {
            socket,
            kcp,
            output,
            update_handle,
        });
        Ok(())
    }

    async fn send(&mut self, packet: &[u8]) -> Result<()> {
        let inner = self.inner.as_ref().ok_or_else(QError::not_connected)?;
        {
            let mut k = inner.kcp.lock().unwrap();
            k.send(packet)
                .map_err(|e| QError::new(QErrorKind::SendFailed, format!("kcp send: {e}")))?;
            // update + flush 确保数据立即发出
            let _ = k.update(Self::now_ms());
            let _ = k.flush();
        }
        let out = inner.output.take();
        if !out.is_empty() {
            inner.socket.send(&out).await.map_err(map_io_error)?;
        }
        Ok(())
    }

    async fn recv(&mut self) -> Result<Option<Vec<u8>>> {
        let inner = self.inner.as_ref().ok_or_else(QError::not_connected)?;
        let mut udp_buf = vec![0u8; 64 * 1024];
        let mut msg_buf = vec![0u8; 64 * 1024];
        loop {
            // 先看 kcp 里有没有已重组好的消息
            {
                let mut k = inner.kcp.lock().unwrap();
                match k.recv(&mut msg_buf) {
                    Ok(n) => return Ok(Some(msg_buf[..n].to_vec())),
                    Err(_) => {} // 没有完整消息，继续收 UDP
                }
            }
            // 收 UDP 数据报，喂给 kcp
            let n = inner.socket.recv(&mut udp_buf).await.map_err(map_io_error)?;
            if n == 0 {
                return Ok(None);
            }
            {
                let mut k = inner.kcp.lock().unwrap();
                k.input(&udp_buf[..n]).map_err(|e| {
                    QError::new(QErrorKind::ProtocolViolation, format!("kcp input: {e}"))
                })?;
            }
        }
    }

    async fn close(&mut self) -> Result<()> {
        if let Some(inner) = self.inner.take() {
            inner.update_handle.abort();
        }
        Ok(())
    }
}
