//! KCP wire compat test: Rust (kcp crate) <-> kcp-go via file shuttling.
//!
//! 1. Rust generates KCP packets, writes to /tmp/kcp_rust_to_go.bin (length-prefixed).
//! 2. Go (kcp-go) reads, verifies, replies to /tmp/kcp_go_to_rust.bin.
//! 3. Rust reads reply, verifies.

use std::io::Write;

#[derive(Clone, Default)]
struct Out {
    buf: std::sync::Arc<std::sync::Mutex<Vec<u8>>>,
}
impl Write for Out {
    fn write(&mut self, b: &[u8]) -> std::io::Result<usize> {
        self.buf.lock().unwrap().extend_from_slice(b);
        Ok(b.len())
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}

const CONV: u32 = 0x12345678;

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let mode = args.get(1).map(|s| s.as_str()).unwrap_or("send");

    if mode == "send" {
        // Rust -> Go
        let out = Out::default();
        let mut k = kcp::Kcp::new(CONV, out.clone());
        k.set_nodelay(true, 10, 2, true);
        k.set_wndsize(4096, 4096);
        k.set_mtu(1400).unwrap();
        k.send(b"HELLO-FROM-RUST").unwrap();
        k.update(0).unwrap();
        let _ = k.flush();

        let data = std::mem::take(&mut *out.buf.lock().unwrap());
        // Length-prefix each output chunk (here just one)
        let mut file = std::fs::File::create("/tmp/kcp_rust_to_go.bin").unwrap();
        file.write_all(&(data.len() as u32).to_le_bytes()).unwrap();
        file.write_all(&data).unwrap();
        println!("Rust wrote {} bytes (conv={:#x})", data.len(), CONV);
    } else if mode == "verify" {
        // Go -> Rust: verify reply
        let data = std::fs::read("/tmp/kcp_go_to_rust.bin").unwrap();
        let out = Out::default();
        let mut k = kcp::Kcp::new(CONV, out.clone());
        k.set_nodelay(true, 10, 2, true);
        k.set_wndsize(4096, 4096);
        k.set_mtu(1400).unwrap();

        // Feed the reply (may be multiple datagrams, feed as-is)
        k.input(&data).unwrap();
        k.update(100).unwrap();

        let mut buf = vec![0u8; 65535];
        match k.recv(&mut buf) {
            Ok(n) => {
                let msg = String::from_utf8_lossy(&buf[..n]);
                println!("Rust received: {:?}", msg);
                if msg == "REPLY-FROM-KCPGO" {
                    println!("KCP-WIRE-COMPAT OK");
                } else {
                    eprintln!("MISMATCH: expected REPLY-FROM-KCPGO");
                    std::process::exit(1);
                }
            }
            Err(e) => {
                eprintln!("kcp recv failed: {:?}", e);
                std::process::exit(1);
            }
        }
    }
}
