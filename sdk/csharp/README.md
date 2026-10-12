# QContinuum C# SDK

qcontinuum 游戏服务器网络层的 C# 客户端 SDK：TCP / KCP / WebSocket 三传输统一接口，
protobuf 消息收发，错误分类，断线指数退避重连。

## 要求

- .NET 8+
- NuGet：`Google.Protobuf` 3.36.2（与仓库 `tools/protobuf` 工具链版本一致）

## 快速开始

```csharp
using QContinuum.Client;
using QContinuum.Test; // 由 proto/test.proto 生成（见仓库 generated/csharp）

var client = new QClient(new QClientOptions
{
    Transport = TransportKind.Tcp,   // 或 Kcp / WebSocket
    Host = "127.0.0.1",
    Port = 9001,
    AutoReconnect = true,
});

client.Connected += () => Console.WriteLine("connected");
client.Disconnected += e => Console.WriteLine($"disconnected: {e.Reason?.Message}, reconnect={e.WillReconnect}");
client.MessageReceived += msg =>
{
    if (msg.Id == (int)MsgId.Echo)
    {
        var echo = msg.Unpack<Echo>();
        Console.WriteLine($"echo seq={echo.Seq}");
    }
};

await client.ConnectAsync();

// 发送 protobuf 消息（id 与服务端 network.MessageHead.ID 对齐）
var echo = new Echo { Seq = 1, SendTimeUnixNano = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() * 1_000_000 };
await client.SendAsync((int)MsgId.Echo, echo);
```

## 线协议

帧格式与 Go 端 `network/message_header.go` 严格一致（小端）：

```
[Length: int32][ID: int32][body: Length bytes]   // Length 仅为 body 长度
```

body 为 protobuf 序列化字节，单包上限 100 MiB（与 Go 端 `maxMessageLength` 一致）。

## 错误处理

所有可分类错误为 `QContinuumException`（`Kind` 见 `QErrorKind`）：

| Kind | 含义 |
| --- | --- |
| `ConnectionRefused` | 端口未监听 |
| `HostUnreachable` | DNS 失败 / 主机不可达 |
| `ConnectTimeout` / `IoTimeout` | 建连 / 读写超时 |
| `ProtocolViolation` | 帧头非法（对端发了坏数据） |
| `MessageTooLarge` | 单包超 100 MiB |
| `RemoteClosed` | 远端关闭 |
| `SendFailed` | 发送失败（含未连接时调用 Send） |
| `ReconnectExhausted` | 重连次数耗尽 |
| `Cancelled` | 调用方取消 |

协议违规（坏帧头）时 SDK 会关闭当前连接并按配置重连，与 Go 端"过载/解析失败即断连"策略一致。

## 断线重连

- 首次 `ConnectAsync` 失败直接抛出，不自动重试；需要"直到连上"语义用 `EnsureConnectedAsync(ct)`。
- 建连成功后连接意外断开 → 按 `AutoReconnect` 进入指数退避重连
  （`ReconnectInitialDelay` × 2ⁿ + 抖动，上限 `ReconnectMaxDelay`；
  `MaxReconnectAttempts = 0` 表示无限）。
- 重连成功会重新触发 `Connected`；残余半包会被丢弃（`PacketReader.Reset()`）。

## KCP 说明

- 内嵌 KCP 实现（`QContinuum.Kcp`），与 Go 端 `kcp-go/v5` 线格式互通，
  无需额外依赖。
- 参数对齐 Go 端 `DefaultKCPConfig`：`NoDelay(1,10,2,1)`、窗口 4096/4096、MTU 1400。
- KCP 基于 UDP，无握手；conv 为客户端随机生成，服务端按首包 conv 建会话。

## WebSocket 说明

- 连接路径固定为 `/ws`（对齐 `ginhttp.SetupWebsocket`）。
- 一个协议包 = 一条二进制 WS 消息；接收侧可处理 WS 分片重组。

## 线程模型

- `SendAsync` 可并发调用，内部串行化。
- `MessageReceived` 在传输 IO 线程上同步触发：处理函数必须短小、无阻塞；
  业务回调抛异常不会杀死连接，仅通过 `Error` 事件上报。
