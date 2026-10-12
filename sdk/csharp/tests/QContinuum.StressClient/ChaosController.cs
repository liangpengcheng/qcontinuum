namespace QContinuum.StressClient;

/// <summary>
/// 混沌控制器：周期性地制造断线、连接 churn 与畸形包注入，
/// 验证服务端协议健壮性与客户端重连能力。
/// </summary>
internal sealed class ChaosController : IAsyncDisposable
{
    private readonly Func<IReadOnlyList<StressWorker>> _getWorkers;
    private readonly Func<StressWorker> _spawnWorker;
    private readonly TimeSpan _interval;
    private readonly double _churnFraction;
    private readonly double _injectFraction;
    private readonly CancellationTokenSource _cts = new();
    private readonly Task _task;

    /// <param name="getWorkers">当前存活 worker 快照</param>
    /// <param name="spawnWorker">新建并启动一个 worker（用于 churn 补充）</param>
    /// <param name="interval">混沌轮次间隔</param>
    /// <param name="churnFraction">每轮永久替换的 worker 比例（老连接退出、新连接加入）</param>
    /// <param name="injectFraction">每轮注入畸形包的 worker 比例</param>
    public ChaosController(
        Func<IReadOnlyList<StressWorker>> getWorkers,
        Func<StressWorker> spawnWorker,
        TimeSpan interval,
        double churnFraction,
        double injectFraction)
    {
        _getWorkers = getWorkers;
        _spawnWorker = spawnWorker;
        _interval = interval;
        _churnFraction = churnFraction;
        _injectFraction = injectFraction;
        _task = Task.Run(LoopAsync);
    }

    private async Task LoopAsync()
    {
        using var timer = new PeriodicTimer(_interval);
        try
        {
            while (await timer.WaitForNextTickAsync(_cts.Token))
            {
                var workers = _getWorkers().Where(w => w.IsRunning).ToList();
                if (workers.Count == 0)
                    continue;
                Shuffle(workers);

                // 1) 畸形包注入：服务端应断开这些连接，客户端应自动重连
                int injectCount = Math.Max(1, (int)(workers.Count * _injectFraction));
                for (int i = 0; i < injectCount && i < workers.Count; i++)
                {
                    try { await workers[i].InjectErrorAsync(Random.Shared.Next(4)); }
                    catch { /* ignore */ }
                }

                // 2) churn：老连接永久退出，新连接加入
                int churnCount = (int)(workers.Count * _churnFraction);
                for (int i = 0; i < churnCount; i++)
                {
                    var victim = workers[workers.Count - 1 - i];
                    try { await victim.DisposeAsync(); } catch { /* ignore */ }
                    try { _spawnWorker(); } catch { /* ignore */ }
                }

                Console.WriteLine($"[chaos] injected={injectCount} churned={churnCount} alive~{workers.Count}");
            }
        }
        catch (OperationCanceledException) { /* 结束 */ }
    }

    private static void Shuffle<T>(List<T> list)
    {
        for (int i = list.Count - 1; i > 0; i--)
        {
            int j = Random.Shared.Next(i + 1);
            (list[i], list[j]) = (list[j], list[i]);
        }
    }

    public async ValueTask DisposeAsync()
    {
        _cts.Cancel();
        try { await _task; } catch { /* ignore */ }
        _cts.Dispose();
    }
}
