# 消息过载与连接生命周期

TCP、标准库 WebSocket、Gin WebSocket 和 KCP 采用同一消息过载策略：非阻塞入队失败时关闭当前连接，不继续读取或处理它后续的业务消息。已经开始执行的回调允许完成，尚未处理的积压消息会被跳过。`ImmediateMode` 继续直接调用业务消息回调，也会拒绝关闭连接和正在退出的 Processor。

## 注册与注销

使用 `proc.TryPostEvent(&network.Event{ID: network.AddEvent, Peer: peer})` 注册连接，成功后才启动读取；注册失败应立即关闭连接。内置传输已遵循此顺序。

携带 Peer 的 `AddEvent` 和 `RemoveEvent` 使用独立 FIFO，不占用普通 `EventChan` 容量。生命周期积压达到 1024 时拒绝新注册；注销不受此容量限制，待清理事件只随已接受的连接数增长。重复注册和注销按连接实例去重，不使用可能重复的业务 ID；事件统一携带 `ToClientPeer()` 返回的稳定视图。成功注册时，尚无处理器的连接会归属到注册它的 Processor。

调用 `peer.Close()` 即会可靠记录注销，业务层与传输适配器无需再补发 `RemoveEvent`。未成功注册的服务端连接不产生注销回调；`NewTcpConnection` 保留主动 TCP 连接原有的关闭通知。生命周期回调在 `StartProcess()` 循环中执行，注销等待连接资源清理及该连接的在途消息回调结束，`Close()` 不等待业务回调，因此允许在回调内关闭自身连接。

显式投递 `RemoveEvent` 也会关闭连接，并向连接当前的 Processor 记录注销；重复投递仍只执行一次注销回调。注销回调只会在连接资源清理完成及在途消息回调结束后执行。

共享消息投递和连接注销使用当前 Processor；关闭开始时确定注销归属，此后 `Redirect` 不再改变它。调用方仍负责 `Redirect` 对应的业务记录迁移。其他自定义事件继续使用普通 `EventChan`，`TryPostEvent` 保留非阻塞、容量不足返回 false 的行为。外部直接写 channel 会绕过入队检查，建议通过投递方法发送连接事件和消息。

## 正常停止

先停止接入并关闭所有连接，再发送 `ExitEvent`，最后等待 `StartProcess()` 返回。HTTP 服务的 `Close` 不会关闭已升级的 WebSocket，调用方需要显式关闭这些连接。

Processor 收到 `ExitEvent` 后拒绝新注册和新业务消息，继续清空生命周期 FIFO，并等待在途消息回调完成才退出。普通积压任务不在退出时继续执行。退出后的 Processor 不再接受新工作；应为新一轮服务创建新的 Processor。

注销回调需正常完成。保证覆盖进程运行及有序退出期间，不提供进程崩溃后的事件持久化或回调重试。

## Gin WebSocket 接入限制

`ginhttp.SetupWebsocket(router, proc)` 保留原有签名和 `/ws` 路径，但采用以下默认配置：

| 配置 | 默认行为 |
| --- | --- |
| `CheckOrigin` | nil 使用 Gorilla 的主机校验：Origin 主机（含端口）必须与请求 Host 一致；无 Origin 请求可以连接 |
| `ReadLimit` | 1 MiB，同时限制单个 WebSocket 消息的总负载和单个业务协议包的总长度，协议包长度包含 8 字节头部 |
| `ReadTimeout` | 60 秒，包含等待下一条消息及读取完整业务协议包的时间 |
| `HandshakeTimeout` | 10 秒，限制升级响应写入；HTTP 请求头读取超时由调用方的 HTTP 服务配置 |
| `WriteTimeout` | 10 秒，限制每次二进制消息发送 |

跨主机的浏览器客户端需要显式配置 Origin 策略。原来发送超过 1 MiB 消息的客户端需要提高 `ReadLimit`；空闲超过 60 秒的连接会断开。每个完整业务协议包结束后开始下一轮读取期限，分片、未完成的协议包、文本和 Ping/Pong 不延长期限，因此仅发送 WebSocket Ping/Pong 不能维持空闲连接。需要保活的客户端应在期限内发送完整业务协议包，或配置更长的读取超时。

通过新入口配置允许的来源、更大的消息和更长的超时，例如：

```go
err := ginhttp.SetupWebsocketWithConfig(router, proc, ginhttp.WebSocketConfig{
	CheckOrigin: func(r *http.Request) bool {
		// 精确匹配完整 Origin；此策略拒绝无 Origin 的请求。
		return r.Header.Get("Origin") == "https://game.example.com"
	},
	ReadLimit:        4 << 20, // 包含协议头的 4 MiB 接收上限
	ReadTimeout:      2 * time.Minute,
	HandshakeTimeout: 10 * time.Second,
	WriteTimeout:     10 * time.Second,
})
if err != nil {
	return err
}
```

`WebSocketConfig{}` 与旧入口使用相同默认配置。数值零值采用默认值，负值或非零且小于 8 字节的 `ReadLimit` 返回错误，错误发生时不会注册路由。Origin 回调可能并发执行，调用方需保证回调线程安全。

接收过程先读取并检查 8 字节协议头，再以最多 8 KiB 的块读取消息体并交给现有协议解析器；不会先缓存整个 WebSocket 消息。业务协议包可以跨多个 WebSocket 消息，一个 WebSocket 消息也可以包含多个协议包，但都受上述两个大小限制约束。网络层现有的 100 MiB 消息体上限仍然有效，即使提高 `ReadLimit` 也不会取消此限制。完整协议包立即投递；后续数据错误不会回滚已执行的业务回调，关闭连接时尚未处理的队列消息仍会被跳过。

Gorilla 会在 WebSocket 消息累计超限时发送关闭码 1009，包括连续分片和被忽略的文本消息。协议长度错误、读取/写入超时及消息入队失败都会关闭当前连接，注册成功的连接继续通过既有生命周期流程可靠注销。
