using System.Collections.Concurrent;
using System.Text.Json;

namespace QContinuum.StressClient;

/// <summary>压测指标：计数器 + RTT 直方图 + 周期性 JSON 上报。</summary>
internal sealed class Metrics
{
    private long _sent;
    private long _echoed;
    private long _mismatch;
    private long _timeout;
    private long _disconnects;
    private long _reconnects;
    private long _injectedErrors;
    private long _reconnectAfterInject;

    // RTT 采样（毫秒），用锁保护的 List，周期性快照后清空
    private readonly object _rttLock = new();
    private readonly List<double> _rtts = new();

    public string Name { get; }

    public Metrics(string name) => Name = name;

    public void Sent() => Interlocked.Increment(ref _sent);
    public void Echoed(double rttMs)
    {
        Interlocked.Increment(ref _echoed);
        lock (_rttLock) _rtts.Add(rttMs);
    }
    public void Mismatch() => Interlocked.Increment(ref _mismatch);
    public void Timeout() => Interlocked.Increment(ref _timeout);
    public void Disconnected() => Interlocked.Increment(ref _disconnects);
    public void Reconnected() => Interlocked.Increment(ref _reconnects);
    public void InjectedError() => Interlocked.Increment(ref _injectedErrors);
    public void ReconnectedAfterInject() => Interlocked.Increment(ref _reconnectAfterInject);

    public Snapshot TakeSnapshot()
    {
        double[] rtts;
        lock (_rttLock)
        {
            rtts = _rtts.ToArray();
            _rtts.Clear();
        }
        Array.Sort(rtts);
        return new Snapshot(
            Name,
            DateTimeOffset.UtcNow,
            Interlocked.Read(ref _sent),
            Interlocked.Read(ref _echoed),
            Interlocked.Read(ref _mismatch),
            Interlocked.Read(ref _timeout),
            Interlocked.Read(ref _disconnects),
            Interlocked.Read(ref _reconnects),
            Interlocked.Read(ref _injectedErrors),
            Interlocked.Read(ref _reconnectAfterInject),
            rtts.Length == 0 ? 0 : rtts[rtts.Length / 2],
            rtts.Length == 0 ? 0 : rtts[(int)(rtts.Length * 0.99)],
            rtts.Length == 0 ? 0 : rtts[rtts.Length - 1]);
    }

    public record Snapshot(
        string Transport,
        DateTimeOffset At,
        long Sent,
        long Echoed,
        long Mismatch,
        long Timeout,
        long Disconnects,
        long Reconnects,
        long InjectedErrors,
        long ReconnectAfterInject,
        double RttP50Ms,
        double RttP99Ms,
        double RttMaxMs);
}

/// <summary>周期性把各传输的指标快照写 JSONL，并打印一行摘要。</summary>
internal sealed class MetricsReporter : IAsyncDisposable
{
    private readonly Metrics[] _all;
    private readonly string _logPath;
    private readonly TimeSpan _interval;
    private readonly Func<long> _serverRssKb;
    private readonly CancellationTokenSource _cts = new();
    private readonly Task _task;

    public MetricsReporter(Metrics[] all, string logPath, TimeSpan interval, Func<long> serverRssKb)
    {
        _all = all;
        _logPath = logPath;
        _interval = interval;
        _serverRssKb = serverRssKb;
        _task = Task.Run(LoopAsync);
    }

    private async Task LoopAsync()
    {
        using var writer = new StreamWriter(_logPath, append: true) { AutoFlush = true };
        var timer = new PeriodicTimer(_interval);
        try
        {
            while (await timer.WaitForNextTickAsync(_cts.Token))
            {
                long rssKb;
                try { rssKb = _serverRssKb(); }
                catch { rssKb = -1; }
                foreach (var m in _all)
                {
                    var s = m.TakeSnapshot();
                    var line = JsonSerializer.Serialize(new
                    {
                        at = s.At.ToString("O"),
                        transport = s.Transport,
                        sent = s.Sent,
                        echoed = s.Echoed,
                        mismatch = s.Mismatch,
                        timeout = s.Timeout,
                        disconnects = s.Disconnects,
                        reconnects = s.Reconnects,
                        injectedErrors = s.InjectedErrors,
                        reconnectAfterInject = s.ReconnectAfterInject,
                        rttP50Ms = Math.Round(s.RttP50Ms, 2),
                        rttP99Ms = Math.Round(s.RttP99Ms, 2),
                        rttMaxMs = Math.Round(s.RttMaxMs, 2),
                        serverRssMb = Math.Round(rssKb / 1024.0, 1),
                    });
                    await writer.WriteLineAsync(line);
                    Console.WriteLine($"[metrics] {s.Transport}: sent={s.Sent} echoed={s.Echoed} " +
                        $"mismatch={s.Mismatch} timeout={s.Timeout} disc={s.Disconnects} " +
                        $"reconn={s.Reconnects} p50={s.RttP50Ms:F1}ms p99={s.RttP99Ms:F1}ms " +
                        $"serverRss={rssKb / 1024.0:F1}MB");
                }
            }
        }
        catch (OperationCanceledException) { /* 正常结束 */ }
    }

    public async ValueTask DisposeAsync()
    {
        _cts.Cancel();
        try { await _task; } catch { /* ignore */ }
        _cts.Dispose();
    }
}
