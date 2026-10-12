namespace QContinuum.Transports;

/// <summary>
/// 底层传输抽象：负责建连、收发原始数据报。
/// 数据报语义：TCP 流分片 / WebSocket 二进制消息 / KCP 消息。
/// 组帧（8 字节头 + protobuf body）由上层 <see cref="Client.QClient"/> 完成。
/// </summary>
internal interface ITransport : IAsyncDisposable
{
    /// <summary>建立连接并启动收包循环。失败时抛出 <see cref="Protocol.QContinuumException"/>。</summary>
    Task ConnectAsync(string host, int port, TimeSpan timeout, CancellationToken ct);

    /// <summary>发送一个完整协议包（已组帧）。可并发调用，内部串行化。</summary>
    Task SendAsync(ReadOnlyMemory<byte> packet, CancellationToken ct);

    /// <summary>
    /// 收到数据报。同步回调：返回后 buffer 即被复用，处理函数必须在返回前拷贝所需数据。
    /// 参数为 (buffer, count)。
    /// </summary>
    event Action<byte[], int> DatagramReceived;

    /// <summary>传输关闭。参数为 null 表示本地主动关闭，否则为关闭原因。</summary>
    event Action<Exception?> Closed;

    /// <summary>主动关闭（幂等）。</summary>
    Task CloseAsync();
}
