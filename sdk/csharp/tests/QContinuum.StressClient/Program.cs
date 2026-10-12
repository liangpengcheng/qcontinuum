// QContinuum 12小时压力测试客户端。
// 用法示例：
//   StressClient --host 127.0.0.1 --tcp 700 --ws 650 --kcp 650 --qps 10 \
//       --duration-hours 12 --server-pid 12345 --log /tmp/stress
using QContinuum.Client;
using QContinuum.StressClient;

var cfg = Config.Parse(args);
Console.WriteLine($"[stress] host={cfg.Host} tcp={cfg.TcpCount}/{cfg.TcpPort} " +
    $"ws={cfg.WsCount}/{cfg.WsPort} kcp={cfg.KcpCount}/{cfg.KcpPort} " +
    $"qps={cfg.Qps} payload={cfg.PayloadSize}B duration={cfg.Duration.TotalHours:F1}h " +
    $"chaos={cfg.Chaos} log={cfg.LogDir}");
Directory.CreateDirectory(cfg.LogDir);

var metricsTcp = new Metrics("tcp");
var metricsWs = new Metrics("ws");
var metricsKcp = new Metrics("kcp");

using var cts = new CancellationTokenSource(cfg.Duration);
var workers = new List<StressWorker>();
var workersLock = new object();

StressWorker Spawn(TransportKind kind, Metrics m, int port)
{
    var w = new StressWorker(kind, cfg.Host, port, cfg.Qps, cfg.PayloadSize, m, autoReconnect: true);
    lock (workersLock) workers.Add(w);
    // 后台启动：建连可能排队，不阻塞主流程
    _ = w.StartAsync(cts.Token).ContinueWith(t =>
    {
        if (t.IsFaulted)
            Console.WriteLine($"[stress] worker {w.Id} start failed: {t.Exception?.InnerException?.Message}");
    }, TaskScheduler.Default);
    return w;
}

IReadOnlyList<StressWorker> Snapshot()
{
    lock (workersLock) return workers.Where(w => w.IsRunning).ToList();
}

// 分批建连，避免瞬间打爆服务端 accept
async Task SpawnAllAsync()
{
    var plan = new List<(TransportKind, Metrics, int, int)>
    {
        (TransportKind.Tcp, metricsTcp, cfg.TcpPort, cfg.TcpCount),
        (TransportKind.WebSocket, metricsWs, cfg.WsPort, cfg.WsCount),
        (TransportKind.Kcp, metricsKcp, cfg.KcpPort, cfg.KcpCount),
    };
    foreach (var (kind, m, port, count) in plan)
    {
        for (int i = 0; i < count; i++)
        {
            if (cts.IsCancellationRequested) return;
            Spawn(kind, m, port);
            if (i % 50 == 49)
                await Task.Delay(200, cts.Token); // 每50个歇200ms
        }
        Console.WriteLine($"[stress] spawned {count} {kind} workers");
    }
}

// 服务端内存采样：/proc/<pid>/status VmRSS
long ServerRssKb()
{
    if (cfg.ServerPid <= 0) return -1;
    foreach (var line in File.ReadLines($"/proc/{cfg.ServerPid}/status"))
    {
        if (line.StartsWith("VmRSS:"))
            return long.Parse(new string(line.Where(char.IsDigit).ToArray()));
    }
    return -1;
}

await using var reporter = new MetricsReporter(
    new[] { metricsTcp, metricsWs, metricsKcp },
    Path.Combine(cfg.LogDir, "metrics.jsonl"),
    cfg.MetricsInterval,
    ServerRssKb);

ChaosController? chaos = null;
if (cfg.Chaos)
{
    // 混沌只作用于 TCP worker 子集（避免三协议同时抖动难以归因）；
    // 如需全协议混沌可扩展。
    chaos = new ChaosController(
        Snapshot,
        () => Spawn(TransportKind.Tcp, metricsTcp, cfg.TcpPort),
        interval: TimeSpan.FromMinutes(2),
        churnFraction: 0.02,   // 每轮 2% 连接永久 churn
        injectFraction: 0.01); // 每轮 1% 连接注入畸形包
}

await SpawnAllAsync();
Console.WriteLine("[stress] all workers spawned, running...");

try
{
    await Task.Delay(cfg.Duration, cts.Token);
}
catch (OperationCanceledException) { /* 正常结束 */ }

Console.WriteLine("[stress] duration reached, shutting down...");
if (chaos != null)
    await chaos.DisposeAsync();

List<StressWorker> all;
lock (workersLock) all = workers.ToList();
foreach (var w in all)
    try { await w.DisposeAsync(); } catch { /* ignore */ }

foreach (var m in new[] { metricsTcp, metricsWs, metricsKcp })
{
    var s = m.TakeSnapshot();
    Console.WriteLine($"[final] {s.Transport}: sent={s.Sent} echoed={s.Echoed} " +
        $"mismatch={s.Mismatch} timeout={s.Timeout} disc={s.Disconnects} " +
        $"reconn={s.Reconnects} injected={s.InjectedErrors} p50={s.RttP50Ms:F1}ms p99={s.RttP99Ms:F1}ms");
}
Console.WriteLine("[stress] done.");
return 0;

// ------------------------------------------------------------------ config

internal sealed class Config
{
    public string Host = "127.0.0.1";
    public int TcpPort = 9001, WsPort = 9002, KcpPort = 9003;
    public int TcpCount = 700, WsCount = 650, KcpCount = 650;
    public int Qps = 10;
    public int PayloadSize = 256;
    public TimeSpan Duration = TimeSpan.FromHours(12);
    public int ServerPid = -1;
    public TimeSpan MetricsInterval = TimeSpan.FromMinutes(5);
    public string LogDir = "/tmp/qstress-log";
    public bool Chaos = true;

    public static Config Parse(string[] args)
    {
        var c = new Config();
        for (int i = 0; i < args.Length; i++)
        {
            string V() => args[++i];
            switch (args[i])
            {
                case "--host": c.Host = V(); break;
                case "--tcp-port": c.TcpPort = int.Parse(V()); break;
                case "--ws-port": c.WsPort = int.Parse(V()); break;
                case "--kcp-port": c.KcpPort = int.Parse(V()); break;
                case "--tcp": c.TcpCount = int.Parse(V()); break;
                case "--ws": c.WsCount = int.Parse(V()); break;
                case "--kcp": c.KcpCount = int.Parse(V()); break;
                case "--qps": c.Qps = int.Parse(V()); break;
                case "--payload": c.PayloadSize = int.Parse(V()); break;
                case "--duration-hours": c.Duration = TimeSpan.FromHours(double.Parse(V())); break;
                case "--duration-minutes": c.Duration = TimeSpan.FromMinutes(double.Parse(V())); break;
                case "--server-pid": c.ServerPid = int.Parse(V()); break;
                case "--metrics-interval-minutes": c.MetricsInterval = TimeSpan.FromMinutes(double.Parse(V())); break;
                case "--log": c.LogDir = V(); break;
                case "--no-chaos": c.Chaos = false; break;
                default: throw new ArgumentException($"unknown arg: {args[i]}");
            }
        }
        return c;
    }
}
