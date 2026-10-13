//! Live interop echo client: QClient <-> Go server.
//!
//! Start the Go server first (see tools/stress --mode server):
//!   go run ./tools/stress --mode server --tcp-port 9001 --ws-port 9002 --kcp-port 9003
//!
//! Then run (transport defaults shown):
//!   cargo run --example echo -- tcp [host] [port] [count]   # default 127.0.0.1 9001 100
//!   cargo run --example echo -- ws  [host] [port] [count]   # default 127.0.0.1 9002 100
//!   cargo run --example echo -- kcp [host] [port] [count]   # default 127.0.0.1 9003 100
//!
//! Sends N protobuf Echo messages (id 2001), verifies seq/payload of each
//! echo, prints RTT stats. Exit code 1 on any mismatch/timeout.

use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use qcontinuum::test_proto::Echo;
use qcontinuum::{QClient, QClientOptions, TransportKind};

fn parse_args() -> (TransportKind, String, u16, usize) {
    let args: Vec<String> = std::env::args().collect();
    let kind = match args.get(1).map(|s| s.as_str()).unwrap_or("tcp") {
        "tcp" => TransportKind::Tcp,
        "ws" => TransportKind::WebSocket,
        "kcp" => TransportKind::Kcp,
        other => {
            eprintln!("unknown transport '{other}', use tcp|ws|kcp");
            std::process::exit(2);
        }
    };
    let default_port = match kind {
        TransportKind::Tcp => 9001,
        TransportKind::WebSocket => 9002,
        TransportKind::Kcp => 9003,
    };
    let host = args.get(2).cloned().unwrap_or_else(|| "127.0.0.1".into());
    let port = args
        .get(3)
        .and_then(|s| s.parse().ok())
        .unwrap_or(default_port);
    let count = args.get(4).and_then(|s| s.parse().ok()).unwrap_or(100);
    (kind, host, port, count)
}

fn now_nano() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos() as i64)
        .unwrap_or(0)
}

#[tokio::main]
async fn main() {
    let (kind, host, port, count) = parse_args();
    println!("echo: {kind:?} -> {host}:{port}, {count} messages");

    let client = QClient::connect(QClientOptions {
        transport: kind,
        host: host.clone(),
        port,
        ..Default::default()
    })
    .await
    .unwrap_or_else(|e| {
        eprintln!("connect failed: {e}");
        std::process::exit(1);
    });
    println!("connected, state={:?}", client.state().await);

    let mut rtts: Vec<u128> = Vec::with_capacity(count);
    let mut mismatches: u32 = 0;
    for seq in 0..count {
        let payload = format!("rust-echo-{seq}").into_bytes();
        let echo = Echo {
            seq: seq as i64,
            send_time_unix_nano: now_nano(),
            payload: payload.clone(),
        };
        let t0 = Instant::now();
        if let Err(e) = client.send_message(2001, &echo).await {
            eprintln!("send {seq} failed: {e}");
            mismatches += 1;
            continue;
        }
        let msg = match tokio::time::timeout(Duration::from_secs(5), client.recv_message()).await {
            Ok(Ok(m)) => m,
            Ok(Err(e)) => {
                eprintln!("recv {seq} failed: {e}");
                mismatches += 1;
                continue;
            }
            Err(_) => {
                eprintln!("recv {seq} timed out");
                mismatches += 1;
                continue;
            }
        };
        let rtt = t0.elapsed().as_micros();
        let back: Echo = match msg.unpack() {
            Ok(m) => m,
            Err(e) => {
                eprintln!("unpack {seq} failed: {e}");
                mismatches += 1;
                continue;
            }
        };
        if msg.id != 2001 || back.seq != seq as i64 || back.payload != payload {
            eprintln!(
                "MISMATCH seq={seq}: id={} seq={} payload_len={}",
                msg.id,
                back.seq,
                back.payload.len()
            );
            mismatches += 1;
        } else {
            rtts.push(rtt);
        }
    }

    client.close().await.ok();

    rtts.sort_unstable();
    let n = rtts.len();
    if n > 0 {
        let avg = rtts.iter().sum::<u128>() / n as u128;
        let p50 = rtts[n / 2];
        let p99 = rtts[(n * 99 / 100).min(n - 1)];
        println!("ECHO-RESULT ok={n}/{count} mismatches={mismatches} rtt_us avg={avg} p50={p50} p99={p99}");
    } else {
        println!("ECHO-RESULT ok=0/{count} mismatches={mismatches}");
    }
    if mismatches > 0 {
        std::process::exit(1);
    }
}
