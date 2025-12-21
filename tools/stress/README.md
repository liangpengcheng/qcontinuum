# QContinuum 压力测试框架使用指南

QContinuum 压力测试框架用于验证网络模块的高并发处理能力、稳定性和极限负载情况。支持 TCP、WebSocket (Gin) 和 KCP 三种协议。

## 1. 编译

在项目根目录下运行以下命令进行编译：

```bash
go build -o stress_test ./tools/stress/
```

## 2. 运行服务器 (Server Mode)

服务器模式会启动所有支持的协议监听，并利用 `Processor` 处理回显请求。

```bash
./stress_test --mode server [flags]
```

### 常用参数：
- `--immediate`: (bool) 是否开启立即模式。开启后消息在 I/O 线程直接回调，延迟最低；关闭后消息进入队列处理，更安全但有队列开销。
- `--tcp-port`: TCP 监听端口 (默认 9001)
- `--ws-port`: WebSocket 监听端口 (默认 9002)
- `--kcp-port`: KCP 监听端口 (默认 9003)

## 3. 运行客户端 (Client Mode)

客户端模式会启动指定数量的并发连接，并以设定的频率发送心跳/数据包。

```bash
./stress_test --mode client [flags]
```

### 常用参数：
- `--clients`: **每种协议**的并发连接数 (默认 10)
- `--rate`: 每个客户端每秒发送的消息数量 (默认 100)
- `--msg-size`: 消息体大小（字节）(默认 64)。支持大包测试，最高支持 100MB（系统限制）。
- `--test-concurrent`: (bool) 是否开启单连接内的并发写入测试。开启后每个连接会启动多个协程同时对该连接进行 `Write`。
- `--connect-delay`: 每个客户端连接之间的延时（毫秒）(默认 0，表示无延时)。当大量客户端同时连接时，设置此参数可以避免连接失败，让客户端依次启动。

## 4. 典型测试场景

### 4.1 高并发连接测试
测试服务器支持海量连接的能力：
```bash
# 无延时连接（适用于连接数较少的情况）
./stress_test --mode client --clients 1000 --rate 10

# 有延时连接（适用于大量并发连接，避免连接失败）
./stress_test --mode client --clients 1000 --rate 10 --connect-delay 10
```

### 4.2 极限吞吐量测试
测试高频小包的处理能力：
```bash
./stress_test --mode client --clients 100 --rate 5000 --test-concurrent=true
```

### 4.3 大包极限测试
验证系统对大内存分配和解析的稳定性：
```bash
./stress_test --mode client --msg-size 1048576  # 1MB 消息
```

### 4.4 立即模式性能对比
对比 `ImmediateMode` 开启前后的延迟表现：
- **开启立即模式**: `./stress_test --mode server --immediate=true`
- **关闭立即模式**: `./stress_test --mode server --immediate=false`

## 5. 统计指标说明

客户端运行期间会实时打印统计信息：
- **发送 QPS**: 全局每秒发送成功次数。
- **接收 QPS**: 全局每秒接收成功次数。
- **平均延迟**: 消息从发送到收到 Echo 回包的往返时间。
- **错误**: 记录发送失败或连接断开的次数。

## 6. 注意事项

1.  **系统限制**: 在进行万级以上并发测试时，请确保已调高操作系统的文件描述符限制 (`ulimit -n`)。
2.  **容量限制**: 
    *   单条消息最大长度为 **100MB**。超过后服务器会自动断开连接。
    *   异步处理队列长度为 **1024**。如果生产速度远大于消费速度，会导致队列满并丢弃消息。
3.  **KCP**: KCP 协议对 CPU 消耗较高，大规模测试时请关注 CPU 占用。
4.  **连接延时**: 当使用大量并发客户端（如超过 1000 个连接）时，建议设置 `--connect-delay` 参数（如 10-50 毫秒），避免所有客户端同时连接导致服务器处理不过来而出现连接失败。
