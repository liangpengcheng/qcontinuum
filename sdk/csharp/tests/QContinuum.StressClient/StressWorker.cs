using System.Collections.Concurrent;
using Google.Protobuf;
using QContinuum.Client;
using QContinuum.Test;

namespace QContinuum.StressClient;

/// <summary>
/// 单连接压测 worker：建连后按固定 QPS 发送 protobuf Echo 并校验回显。
/// </summary>
internal sealed class StressWorker : IAsyncDisposable
{
    private static int _idSeed;

    private readonly int _id = Interlocked.Increment(ref _idSeed);
    private readonly QClient _client;
    private readonly Metrics _metrics;
    private readonly int _qps;
    private readonly byte[] _payload;
    private readonly ConcurrentDictionary<long, PendingEcho> _pending = new();
    private readonly CancellationTokenSource _cts = new();
    private Task? _sendTask;
    private Task? _timeoutTask;
    private long _seq;
    private int _running; // 1 = worker 存活（用于 churn 替换判断）

    private sealed class PendingEcho
    {
        public long Seq;
        public long SentNano;
        public TaskCompletionSource<bool> Tcs = new(TaskCreationOptions.RunContinuationsAsynchronously);
    }

    public int Id => _id;
    public bool IsRunning => _running == 1;
    public TransportKind Transport { get; }

    public StressWorker(TransportKind transport, string host, int port, int qps,
        int payloadSize, Metrics metrics, bool autoReconnect)
    {
        Transport = transport;
        _metrics = metrics;
        _qps = qps;
        _payload = new byte[payloadSize];
        Random.Shared.NextBytes(_payload);
        _client = new QClient(new QClientOptions
        {
            Transport = transport,
            Host = host,
            Port = port,
            AutoReconnect = autoReconnect,
            ConnectTimeout = TimeSpan.FromSeconds(10),
            ReconnectInitialDelay = TimeSpan.FromSeconds(1),
            ReconnectMaxDelay = TimeSpan.FromSeconds(10),
        });
        bool firstConnect = true;
        _client.MessageReceived += OnMessage;
        _client.Disconnected += e => _metrics.Disconnected();
        _client.Connected += () =>
        {
            if (firstConnect) firstConnect = false;
            else _metrics.Reconnected();
        };
    }

    public async Task StartAsync(CancellationToken ct)
    {
        using var linked = CancellationTokenSource.CreateLinkedTokenSource(ct, _cts.Token);
        await _client.EnsureConnectedAsync(linked.Token);
        Interlocked.Exchange(ref _running, 1);
        _sendTask = Task.Run(() => SendLoopAsync(linked.Token));
        _timeoutTask = Task.Run(() => TimeoutLoopAsync(linked.Token));
    }

    /// <summary>注入畸形包：截断头 / 超大长度 / 负长度 / 纯垃圾。服务端应断开该连接。</summary>
    public async Task InjectErrorAsync(int kind)
    {
        var pkt = kind switch
        {
            0 => new byte[] { 0x03, 0x00, 0x00 },                       // 截断头（仅3字节）
            1 => new byte[] { 0xFF, 0xFF, 0xFF, 0x7F, 0x01, 0x00, 0x00, 0x00 }, // 巨大长度
            2 => new byte[] { 0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x00, 0x00, 0x00 }, // 负长度
            _ => new byte[] { 0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55 }, // 垃圾
        };
        try
        {
            await _client.SendRawAsync(pkt);
            _metrics.InjectedError();
        }
        catch
        {
            // 连接已断则跳过
        }
    }

    private void OnMessage(Protocol.QMessage msg)
    {
        if (msg.Id != (int)MsgId.Echo)
            return;
        Echo echo;
        try { echo = msg.Unpack<Echo>(); }
        catch { _metrics.Mismatch(); return; }
        if (!_pending.TryRemove(echo.Seq, out var pending))
            return; // 迟到/重复的回显
        var now = NanoNow();
        if (!PayloadEquals(_payload, echo.Payload))
        {
            _metrics.Mismatch();
            pending.Tcs.TrySetResult(false);
            return;
        }
        _metrics.Echoed((now - echo.SendTimeUnixNano) / 1_000_000.0);
        pending.Tcs.TrySetResult(true);
    }

    private async Task SendLoopAsync(CancellationToken ct)
    {
        var interval = TimeSpan.FromSeconds(1.0 / _qps);
        using var timer = new PeriodicTimer(interval);
        try
        {
            while (await timer.WaitForNextTickAsync(ct))
            {
                var seq = Interlocked.Increment(ref _seq);
                var echo = new Echo
                {
                    Seq = seq,
                    SendTimeUnixNano = NanoNow(),
                    Payload = ByteString.CopyFrom(_payload),
                };
                var pending = new PendingEcho { Seq = seq, SentNano = echo.SendTimeUnixNano };
                _pending[seq] = pending;
                try
                {
                    _metrics.Sent();
                    await _client.SendAsync((int)MsgId.Echo, echo, ct);
                }
                catch
                {
                    // 发送失败（重连中）：移除 pending，超时循环会忽略它
                    _pending.TryRemove(seq, out _);
                }
            }
        }
        catch (OperationCanceledException) { /* 结束 */ }
    }

    private async Task TimeoutLoopAsync(CancellationToken ct)
    {
        try
        {
            while (!ct.IsCancellationRequested)
            {
                await Task.Delay(TimeSpan.FromSeconds(2), ct);
                var now = NanoNow();
                foreach (var kv in _pending)
                {
                    if (now - kv.Value.SentNano > 5_000_000_000L) // 5s 未回显
                    {
                        if (_pending.TryRemove(kv.Key, out _))
                            _metrics.Timeout();
                    }
                }
            }
        }
        catch (OperationCanceledException) { /* 结束 */ }
    }

    public async ValueTask DisposeAsync()
    {
        Interlocked.Exchange(ref _running, 0);
        _cts.Cancel();
        if (_sendTask != null) try { await _sendTask; } catch { }
        if (_timeoutTask != null) try { await _timeoutTask; } catch { }
        _cts.Dispose();
        await _client.DisposeAsync();
    }

    private static long NanoNow() =>
        DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() * 1_000_000L;

    private static bool PayloadEquals(byte[] expected, ByteString actual)
    {
        if (expected.Length != actual.Length)
            return false;
        var span = actual.Span;
        for (int i = 0; i < expected.Length; i++)
            if (expected[i] != span[i])
                return false;
        return true;
    }
}
