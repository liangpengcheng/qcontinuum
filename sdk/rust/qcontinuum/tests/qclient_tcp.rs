//! QClient 集成测试：tokio TCP 回显服务器。

use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;

use qcontinuum::protocol::framing;
use qcontinuum::test_proto::Echo;
use qcontinuum::{QClient, QClientOptions, TransportKind};
use prost::Message;

/// 最小 TCP 回显服务器：读 8 字节头 + body，原样返回。
async fn echo_server(listener: TcpListener) {
    loop {
        let (mut stream, _) = listener.accept().await.unwrap();
        tokio::spawn(async move {
            let mut buf = vec![0u8; 65535];
            let mut acc: Vec<u8> = Vec::new();
            loop {
                let n = match stream.read(&mut buf).await {
                    Ok(0) | Err(_) => break,
                    Ok(n) => n,
                };
                acc.extend_from_slice(&buf[..n]);
                // 尝试解析完整包并回显
                while acc.len() >= 8 {
                    let len = i32::from_le_bytes(acc[0..4].try_into().unwrap());
                    if len < 0 || len > 100 * 1024 * 1024 {
                        break;
                    }
                    let total = 8 + len as usize;
                    if acc.len() < total {
                        break;
                    }
                    let pkt: Vec<u8> = acc.drain(..total).collect();
                    if stream.write_all(&pkt).await.is_err() {
                        break;
                    }
                }
            }
        });
    }
}

#[tokio::test]
async fn qclient_tcp_echo() {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let port = listener.local_addr().unwrap().port();
    tokio::spawn(echo_server(listener));
    tokio::time::sleep(Duration::from_millis(50)).await;

    let client = QClient::connect(QClientOptions {
        transport: TransportKind::Tcp,
        host: "127.0.0.1".into(),
        port,
        ..Default::default()
    })
    .await
    .expect("connect failed");

    // 发送 Echo
    let echo = Echo {
        seq: 42,
        send_time_unix_nano: 123,
        payload: b"hello-rust".to_vec(),
    };
    client
        .send_message(2001, &echo)
        .await
        .expect("send failed");

    // 接收回显
    let msg = tokio::time::timeout(Duration::from_secs(5), client.recv_message())
        .await
        .expect("recv timeout")
        .expect("recv failed");
    assert_eq!(msg.id, 2001);
    let back: Echo = msg.unpack().expect("unpack failed");
    assert_eq!(back.seq, 42);
    assert_eq!(back.payload, b"hello-rust");

    client.close().await.expect("close failed");
}

#[tokio::test]
async fn qclient_reconnects_after_server_drop() {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let port = listener.local_addr().unwrap().port();

    // 服务器：接受连接后立即关闭（模拟断线），第二次才正常回显
    let attempt = std::sync::Arc::new(std::sync::atomic::AtomicU32::new(0));
    let attempt_clone = attempt.clone();
    tokio::spawn(async move {
        loop {
            let (stream, _) = listener.accept().await.unwrap();
            let n = attempt_clone.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
            if n == 0 {
                // 第一次：直接关闭
                drop(stream);
            } else {
                // 之后：回显
                let mut s = stream;
                let mut buf = vec![0u8; 65535];
                let mut acc: Vec<u8> = Vec::new();
                loop {
                    let n = match s.read(&mut buf).await {
                        Ok(0) | Err(_) => break,
                        Ok(n) => n,
                    };
                    acc.extend_from_slice(&buf[..n]);
                    while acc.len() >= 8 {
                        let len = i32::from_le_bytes(acc[0..4].try_into().unwrap());
                        let total = 8 + len as usize;
                        if acc.len() < total {
                            break;
                        }
                        let pkt: Vec<u8> = acc.drain(..total).collect();
                        if s.write_all(&pkt).await.is_err() {
                            break;
                        }
                    }
                }
            }
        }
    });
    tokio::time::sleep(Duration::from_millis(50)).await;

    let client = QClient::connect(QClientOptions {
        transport: TransportKind::Tcp,
        host: "127.0.0.1".into(),
        port,
        reconnect_base_delay: Duration::from_millis(100),
        reconnect_max_delay: Duration::from_millis(500),
        ..Default::default()
    })
    .await
    .expect("connect failed");

    // 等待重连发生（服务器第一次关闭了连接）
    tokio::time::sleep(Duration::from_secs(2)).await;

    // 重连后应该能正常收发
    let echo = Echo {
        seq: 99,
        send_time_unix_nano: 0,
        payload: b"reconnect".to_vec(),
    };
    client.send_message(2001, &echo).await.expect("send failed");
    let msg = tokio::time::timeout(Duration::from_secs(5), client.recv_message())
        .await
        .expect("recv timeout")
        .expect("recv failed");
    assert_eq!(msg.id, 2001);
    let back: Echo = msg.unpack().unwrap();
    assert_eq!(back.seq, 99);

    client.close().await.unwrap();
}
