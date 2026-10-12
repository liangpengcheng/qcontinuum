namespace QContinuum.Client;

/// <summary>连接状态。</summary>
public enum QConnectionState
{
    Disconnected,
    Connecting,
    Connected,
    Reconnecting,
}

/// <summary>断开事件参数。</summary>
public sealed class QDisconnectedEventArgs : EventArgs
{
    /// <summary>断开原因；本地主动关闭时为 null。</summary>
    public Exception? Reason { get; }

    /// <summary>是否会进入自动重连。</summary>
    public bool WillReconnect { get; }

    public QDisconnectedEventArgs(Exception? reason, bool willReconnect)
    {
        Reason = reason;
        WillReconnect = willReconnect;
    }
}
