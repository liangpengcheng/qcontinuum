namespace QContinuum.Protocol;

/// <summary>SDK 错误分类。调用方可按 Kind 做分支处理（重试 / 上报 / 放弃）。</summary>
public enum QErrorKind
{
    Unknown = 0,

    /// <summary>连接被拒绝（目标端口未监听等）。</summary>
    ConnectionRefused,

    /// <summary>主机不可达 / DNS 解析失败。</summary>
    HostUnreachable,

    /// <summary>连接超时。</summary>
    ConnectTimeout,

    /// <summary>读写超时（连接存活但长时间无数据）。</summary>
    IoTimeout,

    /// <summary>协议违规：帧头非法、长度越界、载荷损坏等。对端可能在发送畸形数据。</summary>
    ProtocolViolation,

    /// <summary>单包超过 <see cref="Framing.MaxMessageSize"/>（默认 100 MiB，与 Go 端一致）。</summary>
    MessageTooLarge,

    /// <summary>远端主动关闭了连接。</summary>
    RemoteClosed,

    /// <summary>发送失败（连接已断开或底层写入错误）。</summary>
    SendFailed,

    /// <summary>重连次数耗尽（仅当 MaxReconnectAttempts &gt; 0 时出现）。</summary>
    ReconnectExhausted,

    /// <summary>操作被取消（调用方传入的 CancellationToken）。</summary>
    Cancelled,
}
