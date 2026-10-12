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
