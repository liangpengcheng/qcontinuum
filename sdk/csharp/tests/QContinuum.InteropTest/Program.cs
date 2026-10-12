// Interop check: C# SDK <-> Go stress server (protobuf echo, MsgId=2001).
// Usage: InteropTest <tcp|kcp|ws> [host] [port] [count]
using System.Collections.Concurrent;
using Google.Protobuf;
using QContinuum.Client;
using QContinuum.Test;

var transport = args.Length > 0 ? args[0].ToLowerInvariant() : "tcp";
var host = args.Length > 1 ? args[1] : "127.0.0.1";
var port = args.Length > 2 ? int.Parse(args[2]) : transport switch
{
    "tcp" => 9001,
    "ws" => 9002,
    "kcp" => 9003,
    _ => throw new ArgumentException("transport must be tcp|kcp|ws"),
};
int count = args.Length > 3 ? int.Parse(args[3]) : 100;

var kind = transport switch
{
    "tcp" => TransportKind.Tcp,
    "kcp" => TransportKind.Kcp,
    "ws" => TransportKind.WebSocket,
    _ => throw new ArgumentException("transport must be tcp|kcp|ws"),
};

var pending = new ConcurrentDictionary<long, TaskCompletionSource<Echo>>();
var errors = new ConcurrentBag<Exception>();
var rtts = new ConcurrentBag<double>();

var client = new QClient(new QClientOptions
{
    Transport = kind,
    Host = host,
    Port = port,
    AutoReconnect = true,
});
client.MessageReceived += msg =>
{
    if (msg.Id != (int)MsgId.Echo)
        return;
    var echo = msg.Unpack<Echo>();
    var now = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() * 1_000_000L;
    rtts.Add((now - echo.SendTimeUnixNano) / 1_000_000.0);
    if (pending.TryRemove(echo.Seq, out var tcs))
        tcs.TrySetResult(echo);
};
client.Error += e => errors.Add(e);

Console.WriteLine($"[{transport}] connecting to {host}:{port} ...");
await client.ConnectAsync();
Console.WriteLine($"[{transport}] connected, sending {count} protobuf echoes ...");

var payload = new byte[256];
new Random(1).NextBytes(payload);
var sw = System.Diagnostics.Stopwatch.StartNew();
for (long seq = 1; seq <= count; seq++)
{
    var echo = new Echo
    {
        Seq = seq,
        SendTimeUnixNano = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() * 1_000_000L,
        Payload = ByteString.CopyFrom(payload),
    };
    var tcs = new TaskCompletionSource<Echo>(TaskCreationOptions.RunContinuationsAsynchronously);
    pending[seq] = tcs;
    await client.SendAsync((int)MsgId.Echo, echo);
    var back = await tcs.Task.WaitAsync(TimeSpan.FromSeconds(10));
    if (back.Seq != seq || !back.Payload.Equals(echo.Payload))
    {
        Console.WriteLine($"[{transport}] MISMATCH seq={seq}");
        return 2;
    }
}
sw.Stop();

var rttArr = rtts.ToArray();
Array.Sort(rttArr);
Console.WriteLine($"[{transport}] OK: {count}/{count} echoes matched, " +
    $"{sw.Elapsed.TotalSeconds:F1}s, " +
    $"rtt p50={rttArr[rttArr.Length / 2]:F2}ms p99={rttArr[(int)(rttArr.Length * 0.99)]:F2}ms");
if (!errors.IsEmpty)
    Console.WriteLine($"[{transport}] note: {errors.Count} non-fatal errors observed");

await client.DisposeAsync();
return 0;
