using QContinuum.Client;
using QContinuum.Protocol;
using QContinuum.Transports;

namespace QContinuum.Tests;

/// <summary>
/// 用 FakeTransport（无真实 socket）验证 QClient 的连接/重连状态机、
/// 事件契约与协议违规处理。
/// </summary>
public class QClientReconnectTests
{
    /// <summary>可编程的假传输：控制建连成败、模拟掉线、注入数据报。</summary>
    private sealed class FakeTransport : ITransport
    {
        private int _connectFailuresRemaining;
        private int _connectCount;
        public List<byte[]> SentPackets { get; } = new();
        public int ConnectCount => _connectCount;
        public event Action<byte[], int>? DatagramReceived;
        public event Action<Exception?>? Closed;

        public void FailNextConnects(int n) => _connectFailuresRemaining = n;

        public Task ConnectAsync(string host, int port, TimeSpan timeout, CancellationToken ct)
        {
            Interlocked.Increment(ref _connectCount);
            if (_connectFailuresRemaining > 0)
            {
                _connectFailuresRemaining--;
                throw new QContinuumException(QErrorKind.ConnectionRefused, "fake refused");
            }
            return Task.CompletedTask;
        }

        public Task SendAsync(ReadOnlyMemory<byte> packet, CancellationToken ct)
        {
            SentPackets.Add(packet.ToArray());
            return Task.CompletedTask;
        }

        public void SimulateDrop(Exception? ex = null) => Closed?.Invoke(
            ex ?? new QContinuumException(QErrorKind.RemoteClosed, "fake drop"));

        public void SimulateDatagram(byte[] packet) => DatagramReceived?.Invoke(packet, packet.Length);

        public Task CloseAsync() => Task.CompletedTask;

        public ValueTask DisposeAsync() => ValueTask.CompletedTask;
    }

    /// <summary>创建注入了 FakeTransport 的客户端。</summary>
    private static (QClient client, FakeTransport fake) CreateClient(QClientOptions options)
    {
        var fake = new FakeTransport();
        var client = new QClient(options);
        client.TransportFactory = () => fake;
        return (client, fake);
    }

    private static QClientOptions QuickOptions(bool autoReconnect = true) => new()
    {
        Host = "fake",
        Port = 1,
        AutoReconnect = autoReconnect,
        ConnectTimeout = TimeSpan.FromSeconds(2),
        ReconnectInitialDelay = TimeSpan.FromMilliseconds(50),
        ReconnectMaxDelay = TimeSpan.FromMilliseconds(200),
        MaxReconnectAttempts = 10,
    };

    [Fact]
    public async Task Connect_Send_Receive_Roundtrip()
    {
        var (client, fake) = CreateClient(QuickOptions(autoReconnect: false));
        int connected = 0;
        QMessage? received = null;
        client.Connected += () => Interlocked.Increment(ref connected);
        client.MessageReceived += m => received = m;

        await client.ConnectAsync();
        Assert.Equal(QConnectionState.Connected, client.State);
        Assert.Equal(1, connected);

        await client.SendAsync(2001, new byte[] { 1, 2, 3 });
        Assert.Single(fake.SentPackets);
        var sent = fake.SentPackets[0];
        Assert.True(Framing.TryReadHeader(sent, out int len, out int id));
        Assert.Equal(3, len);
        Assert.Equal(2001, id);

        // 服务端回显
        fake.SimulateDatagram(sent);
        Assert.NotNull(received);
        Assert.Equal(2001, received!.Id);
        Assert.Equal(new byte[] { 1, 2, 3 }, received.Body);

        await client.DisposeAsync();
    }

    [Fact]
    public async Task UnexpectedDrop_TriggersAutoReconnect()
    {
        var (client, fake) = CreateClient(QuickOptions());
        int connected = 0, disconnected = 0;
        bool? willReconnect = null;
        client.Connected += () => Interlocked.Increment(ref connected);
        client.Disconnected += e =>
        {
            Interlocked.Increment(ref disconnected);
            willReconnect = e.WillReconnect;
        };

        await client.ConnectAsync();
        Assert.Equal(1, fake.ConnectCount);

        fake.SimulateDrop();
        // 等待重连完成（退避 50-200ms）
        await WaitForAsync(() => connected >= 2, TimeSpan.FromSeconds(5));

        Assert.Equal(1, disconnected);
        Assert.True(willReconnect);
        Assert.Equal(2, fake.ConnectCount);
        Assert.Equal(QConnectionState.Connected, client.State);

        await client.DisposeAsync();
    }

    [Fact]
    public async Task Reconnect_RetriesWithBackoff_OnRepeatedFailure()
    {
        var (client, fake) = CreateClient(QuickOptions());
        var attempts = new List<int>();
        client.Reconnecting += (a, _) => attempts.Add(a);

        await client.ConnectAsync();
        fake.FailNextConnects(3); // 接下来 3 次重连都失败
        fake.SimulateDrop();

        await WaitForAsync(() => fake.ConnectCount >= 5, TimeSpan.FromSeconds(10));

        Assert.True(attempts.Count >= 3, $"expected >=3 reconnect attempts, got {attempts.Count}");
        // attempt 序号递增
        for (int i = 1; i < attempts.Count; i++)
            Assert.True(attempts[i] > attempts[i - 1]);
        Assert.Equal(QConnectionState.Connected, client.State);

        await client.DisposeAsync();
    }

    [Fact]
    public async Task ProtocolViolation_ClosesAndReconnects()
    {
        var (client, fake) = CreateClient(QuickOptions());
        int disconnected = 0;
        client.Disconnected += _ => Interlocked.Increment(ref disconnected);

        await client.ConnectAsync();
        // 注入坏帧头（负长度）
        var bad = new byte[8];
        bad[0] = 0xFF; bad[1] = 0xFF; bad[2] = 0xFF; bad[3] = 0xFF;
        fake.SimulateDatagram(bad);

        // 协议违规 → 连接关闭 → 自动重连
        await WaitForAsync(() => disconnected >= 1 && fake.ConnectCount >= 2,
            TimeSpan.FromSeconds(5));
        Assert.Equal(QConnectionState.Connected, client.State);

        await client.DisposeAsync();
    }

    [Fact]
    public async Task MaxReconnectAttempts_GivesUp()
    {
        var options = QuickOptions();
        options.MaxReconnectAttempts = 2;
        var (client, fake) = CreateClient(options);
        Exception? terminalError = null;
        client.Error += e => terminalError = e;

        await client.ConnectAsync();
        fake.FailNextConnects(100); // 永远失败
        fake.SimulateDrop();

        await WaitForAsync(() => terminalError != null, TimeSpan.FromSeconds(10));

        var qex = Assert.IsType<QContinuumException>(terminalError);
        Assert.Equal(QErrorKind.ReconnectExhausted, qex.Kind);
        Assert.Equal(QConnectionState.Disconnected, client.State);

        await client.DisposeAsync();
    }

    [Fact]
    public async Task LocalDisconnect_DoesNotReconnect()
    {
        var (client, fake) = CreateClient(QuickOptions());
        await client.ConnectAsync();
        await client.DisconnectAsync();
        int connects = fake.ConnectCount;
        await Task.Delay(500); // 等待确认没有重连发生
        Assert.Equal(connects, fake.ConnectCount);
        Assert.Equal(QConnectionState.Disconnected, client.State);
        await client.DisposeAsync();
    }

    [Fact]
    public async Task Send_WhenDisconnected_ThrowsNotConnected()
    {
        var (client, fake) = CreateClient(QuickOptions(autoReconnect: false));
        await Assert.ThrowsAsync<QNotConnectedException>(() =>
            client.SendAsync(1, new byte[] { 0x00 }));
        await client.DisposeAsync();
    }

    private static async Task WaitForAsync(Func<bool> condition, TimeSpan timeout)
    {
        var deadline = DateTime.UtcNow + timeout;
        while (!condition())
        {
            if (DateTime.UtcNow >= deadline)
                throw new TimeoutException("condition not met in time");
            await Task.Delay(20);
        }
    }
}
