using Google.Protobuf;
using QContinuum.Protocol;
using QContinuum.Transports;

namespace QContinuum.Client;

/// <summary>
/// qcontinuum 高层客户端：TCP / KCP / WebSocket 三传输统一接口，
/// protobuf 消息收发，错误分类，断线指数退避重连。
/// 线程安全：SendAsync 可并发调用；事件在传输 IO 线程上触发，处理函数应短小、无阻塞。
/// </summary>
public sealed class QClient : IAsyncDisposable
{
    private readonly QClientOptions _options;
    private readonly PacketReader _reader = new();
    private readonly object _stateLock = new();

    private ITransport? _transport;
    private QConnectionState _state = QConnectionState.Disconnected;
    private CancellationTokenSource? _reconnectCts;
    private Task? _reconnectTask;
    private bool _disposed;
    private int _reconnectAttempt;

    /// <summary>建连成功（含重连成功）时触发。</summary>
    public event Action? Connected;

    /// <summary>连接断开时触发。</summary>
    public event Action<QDisconnectedEventArgs>? Disconnected;

    /// <summary>每次重连尝试前触发（参数：attempt 从 1 开始，delay 本次等待）。</summary>
    public event Action<int, TimeSpan>? Reconnecting;

    /// <summary>收到完整业务消息时触发（传输 IO 线程，勿阻塞）。</summary>
    public event Action<QMessage>? MessageReceived;

    /// <summary>非致命错误（如重连次数耗尽）时触发。</summary>
    public event Action<Exception>? Error;

    public QConnectionState State
    {
        get { lock (_stateLock) return _state; }
    }

    public bool IsConnected => State == QConnectionState.Connected;

    public QClient(QClientOptions options)
    {
        options.Validate();
        // 防御性拷贝，避免外部后续修改
        _options = new QClientOptions
        {
            Transport = options.Transport,
            Host = options.Host,
            Port = options.Port,
            ConnectTimeout = options.ConnectTimeout,
            AutoReconnect = options.AutoReconnect,
            ReconnectInitialDelay = options.ReconnectInitialDelay,
            ReconnectMaxDelay = options.ReconnectMaxDelay,
            MaxReconnectAttempts = options.MaxReconnectAttempts,
        };
    }

    // ------------------------------------------------------------- public API

    /// <summary>
    /// 建连（单次尝试）。失败直接抛出 <see cref="QContinuumException"/>，不自动重试。
    /// 成功后若连接意外断开，将按 <see cref="QClientOptions.AutoReconnect"/> 自动重连。
    /// </summary>
    public async Task ConnectAsync(CancellationToken ct = default)
    {
        ThrowIfDisposed();
        CancelReconnectLoop();
        SetState(QConnectionState.Connecting);
        try
        {
            await ConnectTransportAsync(ct).ConfigureAwait(false);
        }
        catch
        {
            SetState(QConnectionState.Disconnected);
            throw;
        }
        SetState(QConnectionState.Connected);
        Connected?.Invoke();
    }

    /// <summary>
    /// 建连直到成功或 ct 取消（指数退避重试）。适合"服务端可能稍后启动"场景。
    /// 每次尝试失败触发 <see cref="Reconnecting"/>（attempt 从 1 开始）。
    /// </summary>
    public async Task EnsureConnectedAsync(CancellationToken ct = default)
    {
        ThrowIfDisposed();
        int attempt = 0;
        while (true)
        {
            ct.ThrowIfCancellationRequested();
            attempt++;
            try
            {
                await ConnectAsync(ct).ConfigureAwait(false);
                return;
            }
            catch (Exception ex) when (ex is not OperationCanceledException)
            {
                var delay = ComputeDelay(attempt);
                Reconnecting?.Invoke(attempt, delay);
                await Task.Delay(delay, ct).ConfigureAwait(false);
            }
        }
    }

    /// <summary>发送 protobuf 消息。</summary>
    public Task SendAsync(int id, IMessage message, CancellationToken ct = default)
        => SendAsync(id, message.ToByteArray(), ct);

    /// <summary>发送原始 body（调用方已序列化好，通常是 protobuf）。</summary>
    public async Task SendAsync(int id, byte[] body, CancellationToken ct = default)
    {
        ThrowIfDisposed();
        var transport = _transport;
        if (transport == null || State != QConnectionState.Connected)
            throw new QNotConnectedException();
        var packet = Framing.Encode(id, body);
        try
        {
            await transport.SendAsync(packet, ct).ConfigureAwait(false);
        }
        catch (QContinuumException)
        {
            throw;
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            throw new QContinuumException(QErrorKind.SendFailed,
                $"Send id={id} failed: {ex.Message}", ex);
        }
    }

    /// <summary>
    /// 发送原始协议包（不组帧、不校验），仅供压测错误注入使用。
    /// </summary>
    internal async Task SendRawAsync(byte[] packet, CancellationToken ct = default)
    {
        ThrowIfDisposed();
        var transport = _transport;
        if (transport == null || State != QConnectionState.Connected)
            throw new QNotConnectedException();
        await transport.SendAsync(packet, ct).ConfigureAwait(false);
    }

    /// <summary>主动断开（含停止重连循环）。幂等。</summary>
    public async Task DisconnectAsync()
    {
        CancelReconnectLoop();
        var transport = Interlocked.Exchange(ref _transport, null);
        SetState(QConnectionState.Disconnected);
        if (transport != null)
        {
            try { await transport.CloseAsync().ConfigureAwait(false); }
            catch { /* ignore */ }
            await transport.DisposeAsync().ConfigureAwait(false);
        }
    }

    public async ValueTask DisposeAsync()
    {
        if (_disposed)
            return;
        _disposed = true;
        await DisconnectAsync().ConfigureAwait(false);
        var task = _reconnectTask;
        if (task != null)
        {
            try { await task.ConfigureAwait(false); } catch { /* ignore */ }
        }
    }

    // ------------------------------------------------------------ internals

    private async Task ConnectTransportAsync(CancellationToken ct)
    {
        var transport = CreateTransport();
        WireTransport(transport);
        bool published = false;
        try
        {
            await transport.ConnectAsync(_options.Host, _options.Port,
                _options.ConnectTimeout, ct).ConfigureAwait(false);
            // 建连成功但已被取消（重连循环被 ConnectAsync/Dispose 抢占）时不发布，
            // 避免覆盖新的连接。
            ct.ThrowIfCancellationRequested();
            var old = Interlocked.Exchange(ref _transport, transport);
            published = true;
            if (old != null)
                await old.DisposeAsync().ConfigureAwait(false);
            lock (_stateLock)
            {
                _reader.Reset(); // 丢弃旧连接的残余半包
                _reconnectAttempt = 0;
            }
        }
        finally
        {
            if (!published)
            {
                UnwireTransport(transport);
                await transport.DisposeAsync().ConfigureAwait(false);
            }
        }
    }

    private ITransport CreateTransport() => _options.Transport switch
    {
        TransportKind.Tcp => new TcpTransport(),
        TransportKind.Kcp => new KcpTransport(),
        TransportKind.WebSocket => new WebSocketTransport(),
        _ => throw new ArgumentOutOfRangeException(nameof(_options.Transport)),
    };

    private void WireTransport(ITransport transport)
    {
        transport.DatagramReceived += OnDatagram;
        transport.Closed += OnTransportClosed;
    }

    private void UnwireTransport(ITransport transport)
    {
        transport.DatagramReceived -= OnDatagram;
        transport.Closed -= OnTransportClosed;
    }

    private void OnDatagram(byte[] buffer, int count)
    {
        // 同步执行：transport 在回调返回后复用 buffer，这里先拷贝进组帧器
        _reader.Feed(buffer.AsSpan(0, count));
        while (true)
        {
            QMessage? msg;
            try
            {
                if (!_reader.TryTake(out msg))
                    break;
            }
            catch (QContinuumException ex)
            {
                // 帧头非法：与 Go 端行为一致，视为连接已损坏，关闭并（按配置）重连
                _ = CloseTransportWithErrorAsync(ex);
                break;
            }
            try
            {
                MessageReceived?.Invoke(msg!);
            }
            catch (Exception ex)
            {
                // 业务回调异常不杀死连接，仅上报
                Error?.Invoke(ex);
            }
        }
    }

    private async Task CloseTransportWithErrorAsync(Exception ex)
    {
        var transport = Interlocked.Exchange(ref _transport, null);
        if (transport != null)
        {
            UnwireTransport(transport);
            try { await transport.CloseAsync().ConfigureAwait(false); }
            catch { /* ignore */ }
            await transport.DisposeAsync().ConfigureAwait(false);
        }
        OnTransportClosed(ex);
    }

    private void OnTransportClosed(Exception? reason)
    {
        // 本地主动关闭（Disconnect/Dispose）不触发重连
        if (_disposed)
            return;
        lock (_stateLock)
        {
            if (_state is QConnectionState.Disconnected or QConnectionState.Connecting)
                return;
        }

        bool willReconnect = _options.AutoReconnect;
        SetState(willReconnect ? QConnectionState.Reconnecting : QConnectionState.Disconnected);
        Disconnected?.Invoke(new QDisconnectedEventArgs(reason, willReconnect));

        if (willReconnect)
            StartReconnectLoop();
    }

    private void StartReconnectLoop()
    {
        lock (_stateLock)
        {
            if (_disposed)
                return;
            if (_reconnectTask != null && !_reconnectTask.IsCompleted)
                return; // 已有重连循环在跑
            _reconnectCts?.Dispose();
            _reconnectCts = new CancellationTokenSource();
            _reconnectTask = Task.Run(() => ReconnectLoopAsync(_reconnectCts.Token));
        }
    }

    private void CancelReconnectLoop()
    {
        Task? task;
        lock (_stateLock)
        {
            _reconnectCts?.Cancel();
            _reconnectCts?.Dispose();
            _reconnectCts = null;
            task = _reconnectTask;
            _reconnectTask = null;
        }
        // 不等待：调用方在 DisposeAsync 中会等待
    }

    private async Task ReconnectLoopAsync(CancellationToken ct)
    {
        try
        {
            while (!ct.IsCancellationRequested && !_disposed)
            {
                int attempt = Interlocked.Increment(ref _reconnectAttempt);
                if (_options.MaxReconnectAttempts > 0 && attempt > _options.MaxReconnectAttempts)
                {
                    var exhausted = new QContinuumException(QErrorKind.ReconnectExhausted,
                        $"Reconnect attempts exhausted ({_options.MaxReconnectAttempts}).");
                    SetState(QConnectionState.Disconnected);
                    Disconnected?.Invoke(new QDisconnectedEventArgs(exhausted, false));
                    Error?.Invoke(exhausted);
                    return;
                }

                var delay = ComputeDelay(attempt);
                Reconnecting?.Invoke(attempt, delay);
                try
                {
                    await Task.Delay(delay, ct).ConfigureAwait(false);
                }
                catch (OperationCanceledException)
                {
                    return;
                }

                SetState(QConnectionState.Connecting);
                try
                {
                    await ConnectTransportAsync(ct).ConfigureAwait(false);
                }
                catch (OperationCanceledException)
                {
                    return;
                }
                catch (Exception)
                {
                    // 下一轮继续退避重试
                    continue;
                }

                SetState(QConnectionState.Connected);
                Connected?.Invoke();
                return;
            }
        }
        finally
        {
            EndReconnectLoop();
        }
    }

    private void EndReconnectLoop()
    {
        lock (_stateLock)
        {
            // 只有已完成的任务才清理引用，避免新循环被误判为重复
            if (_reconnectTask?.IsCompleted == true)
                _reconnectTask = null;
        }
    }

    private TimeSpan ComputeDelay(int attempt)
    {
        double exp = (double)_options.ReconnectInitialDelay.TotalMilliseconds * (1 << Math.Min(attempt - 1, 10));
        double capped = Math.Min(exp, _options.ReconnectMaxDelay.TotalMilliseconds);
        double jitter = capped * (0.75 + 0.5 * Random.Shared.NextDouble());
        return TimeSpan.FromMilliseconds(jitter);
    }

    private void SetState(QConnectionState state)
    {
        lock (_stateLock) _state = state;
    }

    private void ThrowIfDisposed()
    {
        if (_disposed)
            throw new ObjectDisposedException(nameof(QClient));
    }
}
