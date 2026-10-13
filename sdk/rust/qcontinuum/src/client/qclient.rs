//! QClient：高层客户端，带 protobuf 收发与断线重连。
//!
//! 设计：后台任务独占 `Transport`，通过 channel 与前台通信。
//! - `send_message()` → `send_tx` channel → 后台任务 `transport.send()`
//! - 后台任务 `transport.recv()` → 组帧 → `message_tx` channel → `recv_message()`
//! - 断线后后台任务指数退避重建传输。

use std::sync::Arc;
use std::time::Duration;

use prost::Message as ProstMessage;
use tokio::sync::{mpsc, Mutex};

use super::options::{QClientOptions, TransportKind};
use crate::protocol::error::{QError, QErrorKind};
use crate::protocol::{framing, PacketReader, QMessage, Result};
use crate::transport::{KcpTransport, TcpTransport, Transport, WebSocketTransport};

/// 连接状态。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ConnectionState {
    Disconnected,
    Connecting,
    Connected,
    Reconnecting,
}

/// qcontinuum 高层客户端。
pub struct QClient {
    inner: Arc<ClientInner>,
}

struct ClientInner {
    options: QClientOptions,
    state: Mutex<ConnectionState>,
    /// 发包通道（前台 → 后台）。
    send_tx: mpsc::UnboundedSender<Vec<u8>>,
    /// 收包通道（后台 → 前台）。
    message_rx: Mutex<mpsc::UnboundedReceiver<QMessage>>,
    /// 关闭信号。
    shutdown_tx: mpsc::Sender<()>,
    /// 后台任务句柄。
    bg_handle: Mutex<Option<tokio::task::JoinHandle<()>>>,
}

fn make_transport(kind: TransportKind) -> Box<dyn Transport> {
    match kind {
        TransportKind::Tcp => Box::new(TcpTransport::new()),
        TransportKind::Kcp => Box::new(KcpTransport::new()),
        TransportKind::WebSocket => Box::new(WebSocketTransport::new()),
    }
}

impl QClient {
    /// 连接服务器并启动后台收包任务。
    pub async fn connect(options: QClientOptions) -> Result<Self> {
        let mut transport = make_transport(options.transport);
        transport
            .connect(&options.host, options.port, options.connect_timeout)
            .await?;

        let (send_tx, send_rx) = mpsc::unbounded_channel::<Vec<u8>>();
        let (message_tx, message_rx) = mpsc::unbounded_channel::<QMessage>();
        let (shutdown_tx, shutdown_rx) = mpsc::channel::<()>(1);

        let inner = Arc::new(ClientInner {
            options: options.clone(),
            state: Mutex::new(ConnectionState::Connected),
            send_tx,
            message_rx: Mutex::new(message_rx),
            shutdown_tx,
            bg_handle: Mutex::new(None),
        });

        let inner_clone = inner.clone();
        let opts = options.clone();
        let bg_handle = tokio::spawn(async move {
            Self::bg_loop(opts, transport, send_rx, message_tx, shutdown_rx).await;
            *inner_clone.state.lock().await = ConnectionState::Disconnected;
        });
        *inner.bg_handle.lock().await = Some(bg_handle);

        Ok(Self { inner })
    }

    /// 发送 protobuf 消息。
    pub async fn send_message<T: ProstMessage>(&self, id: i32, msg: &T) -> Result<()> {
        let body = msg.encode_to_vec();
        let packet = framing::encode(id, &body)?;
        self.inner
            .send_tx
            .send(packet)
            .map_err(|_| QError::new(QErrorKind::SendFailed, "send channel closed"))?;
        Ok(())
    }

    /// 接收下一条完整消息（阻塞）。
    pub async fn recv_message(&self) -> Result<QMessage> {
        let mut rx = self.inner.message_rx.lock().await;
        rx.recv()
            .await
            .ok_or_else(|| QError::new(QErrorKind::RemoteClosed, "message channel closed"))
    }

    /// 当前连接状态。
    pub async fn state(&self) -> ConnectionState {
        *self.inner.state.lock().await
    }

    /// 主动关闭（不再重连）。
    pub async fn close(&self) -> Result<()> {
        let _ = self.inner.shutdown_tx.send(()).await;
        if let Some(h) = self.inner.bg_handle.lock().await.take() {
            h.abort();
        }
        *self.inner.state.lock().await = ConnectionState::Disconnected;
        Ok(())
    }

    /// 后台任务：独占传输，收包/发包/组帧/重连。
    async fn bg_loop(
        options: QClientOptions,
        mut transport: Box<dyn Transport>,
        mut send_rx: mpsc::UnboundedReceiver<Vec<u8>>,
        message_tx: mpsc::UnboundedSender<QMessage>,
        mut shutdown_rx: mpsc::Receiver<()>,
    ) {
        let mut reader = PacketReader::new();
        let mut reconnect_attempts: u32 = 0;

        loop {
            tokio::select! {
                // 收包
                recv_res = transport.recv() => {
                    match recv_res {
                        Ok(Some(data)) => {
                            reconnect_attempts = 0;
                            reader.feed(&data);
                            let mut violated = false;
                            loop {
                                match reader.try_take() {
                                    Ok(Some((id, body))) => {
                                        let _ = message_tx.send(QMessage::new(id, body));
                                    }
                                    Ok(None) => break,
                                    Err(e) => {
                                        eprintln!("[qcontinuum] protocol violation: {e}");
                                        violated = true;
                                        break;
                                    }
                                }
                            }
                            if violated {
                                if !Self::reconnect(
                                    &options, &mut transport, &mut reader,
                                    &mut reconnect_attempts, &mut shutdown_rx,
                                ).await {
                                    break;
                                }
                            }
                        }
                        Ok(None) => {
                            eprintln!("[qcontinuum] remote closed, reconnecting");
                            if !Self::reconnect(
                                &options, &mut transport, &mut reader,
                                &mut reconnect_attempts, &mut shutdown_rx,
                            ).await {
                                break;
                            }
                        }
                        Err(e) => {
                            eprintln!("[qcontinuum] recv error: {e}, reconnecting");
                            if !Self::reconnect(
                                &options, &mut transport, &mut reader,
                                &mut reconnect_attempts, &mut shutdown_rx,
                            ).await {
                                break;
                            }
                        }
                    }
                }
                // 发包
                pkt = send_rx.recv() => {
                    match pkt {
                        Some(p) => {
                            if let Err(e) = transport.send(&p).await {
                                eprintln!("[qcontinuum] send error: {e}, reconnecting");
                                if !Self::reconnect(
                                    &options, &mut transport, &mut reader,
                                    &mut reconnect_attempts, &mut shutdown_rx,
                                ).await {
                                    break;
                                }
                            }
                        }
                        None => break, // 前台已 drop
                    }
                }
                // 关闭
                _ = shutdown_rx.recv() => break,
            }
        }
    }

    /// 重连：指数退避，重建传输。返回 false 表示应退出（次数耗尽或主动关闭）。
    async fn reconnect(
        options: &QClientOptions,
        transport: &mut Box<dyn Transport>,
        reader: &mut PacketReader,
        attempts: &mut u32,
        shutdown_rx: &mut mpsc::Receiver<()>,
    ) -> bool {
        reader.reset(); // 丢弃半包
        let max = options.max_reconnect_attempts;
        if max > 0 && *attempts >= max {
            return false;
        }
        let delay = Self::backoff_delay(options, *attempts);
        *attempts += 1;

        tokio::select! {
            _ = tokio::time::sleep(delay) => {},
            _ = shutdown_rx.recv() => return false,
        }

        let mut new_transport = make_transport(options.transport);
        match new_transport
            .connect(&options.host, options.port, options.connect_timeout)
            .await
        {
            Ok(()) => {
                *transport = new_transport;
                *attempts = 0;
                true
            }
            Err(e) => {
                eprintln!("[qcontinuum] reconnect failed: {e}");
                true // 继续退避循环
            }
        }
    }

    /// 指数退避 + 抖动。
    fn backoff_delay(opts: &QClientOptions, attempt: u32) -> Duration {
        let base = opts.reconnect_base_delay.as_millis() as f64;
        let max = opts.reconnect_max_delay.as_millis() as f64;
        let exp = base * 2f64.powi(attempt.min(10) as i32);
        let capped = exp.min(max);
        let jitter = 1.0 + (rand::random::<f64>() - 0.5) * 2.0 * opts.reconnect_jitter;
        Duration::from_millis((capped * jitter).max(1.0) as u64)
    }
}
