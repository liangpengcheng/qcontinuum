namespace QContinuum.Client;

/// <summary>传输类型。</summary>
public enum TransportKind
{
    Tcp,
    Kcp,
    WebSocket,
}

/// <summary>QClient 配置。</summary>
public sealed class QClientOptions
{
    /// <summary>传输类型，默认 TCP。</summary>
    public TransportKind Transport { get; set; } = TransportKind.Tcp;

    public string Host { get; set; } = "127.0.0.1";

    public int Port { get; set; }

    /// <summary>单次建连超时，默认 5 秒。</summary>
    public TimeSpan ConnectTimeout { get; set; } = TimeSpan.FromSeconds(5);

    /// <summary>
    /// 建连成功后若连接意外断开，是否自动重连。默认 true。
    /// 注意：首次 ConnectAsync 失败不会自动重试（会直接抛出），
    /// 需要"直到连上为止"语义请用 <see cref="QClient.EnsureConnectedAsync"/>。
    /// </summary>
    public bool AutoReconnect { get; set; } = true;

    /// <summary>重连初始退避，默认 1 秒。</summary>
    public TimeSpan ReconnectInitialDelay { get; set; } = TimeSpan.FromSeconds(1);

    /// <summary>重连退避上限，默认 30 秒。</summary>
    public TimeSpan ReconnectMaxDelay { get; set; } = TimeSpan.FromSeconds(30);

    /// <summary>最大重连次数，0 表示无限。默认 0。</summary>
    public int MaxReconnectAttempts { get; set; } = 0;

    public void Validate()
    {
        if (string.IsNullOrWhiteSpace(Host))
            throw new ArgumentException("Host is required.", nameof(Host));
        if (Port <= 0 || Port > 65535)
            throw new ArgumentOutOfRangeException(nameof(Port), "Port must be 1-65535.");
        if (ConnectTimeout <= TimeSpan.Zero)
            throw new ArgumentOutOfRangeException(nameof(ConnectTimeout));
        if (ReconnectInitialDelay <= TimeSpan.Zero || ReconnectMaxDelay <= TimeSpan.Zero)
            throw new ArgumentOutOfRangeException("Reconnect delays must be positive.");
        if (MaxReconnectAttempts < 0)
            throw new ArgumentOutOfRangeException(nameof(MaxReconnectAttempts));
    }
}
