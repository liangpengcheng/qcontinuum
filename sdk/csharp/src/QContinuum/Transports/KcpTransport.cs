using System.Buffers;
using System.Net;
using System.Net.Sockets;
using QContinuum.Kcp;
using QContinuum.Protocol;

namespace QContinuum.Transports;

/// <summary>
/// KCP 传输：UDP + 可靠 ARQ（内嵌 Kcp 实现，与 Go 端 kcp-go 互通）。
/// 参数对齐 Go 端 DefaultKCPConfig：NoDelay(1,10,2,1)，窗口 4096/4096，MTU 1400。
/// KCP 所有调用都在一把锁下串行执行（Kcp 实例非线程安全）。
/// </summary>
internal sealed class KcpTransport : ITransport
{
    private UdpClient? _udp;
    private IPEndPoint? _remote;
    private Kcp.Kcp? _kcp;
    private readonly object _kcpLock = new();
    private readonly SemaphoreSlim _sendLock = new(1, 1);
    private CancellationTokenSource? _runCts;
    private Task? _recvTask;
    private Task? _updateTask;
    private int _closed;
    private uint _conv;

    public event Action<byte[], int>? DatagramReceived;
    public event Action<Exception?>? Closed;

    public async Task ConnectAsync(string host, int port, TimeSpan timeout, CancellationToken ct)
    {
        IPAddress[] addresses;
        try
        {
            addresses = await Dns.GetHostAddressesAsync(host, ct).ConfigureAwait(false);
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            throw new QContinuumException(QErrorKind.HostUnreachable,
                $"KCP DNS resolve {host} failed: {ex.Message}", ex);
        }
        var ip = addresses.First(a => a.AddressFamily == AddressFamily.InterNetwork);
        var remote = new IPEndPoint(ip, port);

        var udp = new UdpClient();
        try
        {
            udp.Connect(remote);
            // 连通性探测：UDP 无握手，发送一个空 KCP 包验证对端可达性？
            // kcp-go 服务端对任意 conv 的首包即建会话，这里直接建 KCP 实例即可。
        }
        catch (Exception ex)
        {
            udp.Dispose();
            throw new QContinuumException(QErrorKind.HostUnreachable,
                $"KCP udp setup to {remote} failed: {ex.Message}", ex);
        }

        _conv = (uint)Random.Shared.NextInt64(1, uint.MaxValue);
        var kcp = new Kcp.Kcp(_conv);
        kcp.NoDelay(1, 10, 2, 1);
        kcp.WndSize(4096, 4096);
        kcp.SetMtu(1400);
        var udpRef = udp;
        var remoteRef = remote;
        kcp.Output = (buf, len) =>
        {
            try { udpRef.Send(buf, len, remoteRef); }
            catch { /* 发送失败由上层重连处理 */ }
        };

        _udp = udp;
        _remote = remote;
        _kcp = kcp;
        _runCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
        _recvTask = Task.Run(() => ReceiveLoopAsync(_runCts.Token));
        _updateTask = Task.Run(() => UpdateLoopAsync(_runCts.Token));

        // UDP connect 本身不验证对端；给 KCP 一次 flush 机会让首包发出。
        // 真正的连通性由上层心跳/业务超时判定。
        using var timeoutCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
        timeoutCts.CancelAfter(timeout);
        try
        {
            await Task.Delay(1, timeoutCts.Token).ConfigureAwait(false);
        }
        catch (OperationCanceledException) when (!ct.IsCancellationRequested)
        {
            await CloseAsync().ConfigureAwait(false);
            throw new QContinuumException(QErrorKind.ConnectTimeout,
                $"KCP connect to {remote} timed out.");
        }
    }

    public async Task SendAsync(ReadOnlyMemory<byte> packet, CancellationToken ct)
    {
        var kcp = _kcp;
        if (kcp == null)
            throw new QNotConnectedException();
        await _sendLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            var buf = ArrayPool<byte>.Shared.Rent(packet.Length);
            try
            {
                packet.Span.CopyTo(buf);
                lock (_kcpLock)
                {
                    if (kcp.Send(buf, 0, packet.Length) != 0)
                        throw new QContinuumException(QErrorKind.SendFailed,
                            $"KCP send queue rejected packet of {packet.Length} bytes.");
                    kcp.Update(NowMs()); // 立即 flush，降低延迟
                }
            }
            finally
            {
                ArrayPool<byte>.Shared.Return(buf);
            }
        }
        catch (QContinuumException)
        {
            throw;
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            throw new QContinuumException(QErrorKind.SendFailed,
                $"KCP send failed: {ex.Message}", ex);
        }
        finally
        {
            _sendLock.Release();
        }
    }

    public async Task CloseAsync()
    {
        if (Interlocked.Exchange(ref _closed, 1) != 0)
            return;
        _runCts?.Cancel();
        try { _udp?.Close(); } catch { /* ignore */ }
        if (_recvTask != null)
        {
            try { await _recvTask.ConfigureAwait(false); } catch { /* ignore */ }
        }
        if (_updateTask != null)
        {
            try { await _updateTask.ConfigureAwait(false); } catch { /* ignore */ }
        }
        Closed?.Invoke(null);
        _runCts?.Dispose();
        _sendLock.Dispose();
        _udp?.Dispose();
    }

    private async Task ReceiveLoopAsync(CancellationToken ct)
    {
        var udp = _udp!;
        var kcp = _kcp!;
        var recvBuf = ArrayPool<byte>.Shared.Rent(64 * 1024);
        try
        {
            while (!ct.IsCancellationRequested)
            {
                UdpReceiveResult result;
                try
                {
                    result = await udp.ReceiveAsync(ct).ConfigureAwait(false);
                }
                catch (OperationCanceledException)
                {
                    break;
                }
                catch (Exception ex)
                {
                    RaiseClosed(new QContinuumException(QErrorKind.RemoteClosed,
                        $"KCP udp receive failed: {ex.Message}", ex));
                    return;
                }

                var datagram = result.Buffer;
                lock (_kcpLock)
                {
                    if (kcp.Input(datagram, NowMs()) != 0)
                        continue; // conv 不匹配或损坏包：忽略（可能是旧会话残留）
                    while (true)
                    {
                        int n = kcp.Receive(recvBuf);
                        if (n <= 0)
                            break;
                        try
                        {
                            DatagramReceived?.Invoke(recvBuf, n);
                        }
                        catch (Exception ex)
                        {
                            RaiseClosed(new QContinuumException(QErrorKind.ProtocolViolation,
                                $"Datagram handler threw: {ex.Message}", ex));
                            return;
                        }
                    }
                }
            }
        }
        finally
        {
            ArrayPool<byte>.Shared.Return(recvBuf);
        }
    }

    private async Task UpdateLoopAsync(CancellationToken ct)
    {
        var kcp = _kcp!;
        try
        {
            while (!ct.IsCancellationRequested)
            {
                await Task.Delay(10, ct).ConfigureAwait(false);
                lock (_kcpLock)
                {
                    kcp.Update(NowMs());
                    if (kcp.State == -1)
                    {
                        RaiseClosed(new QContinuumException(QErrorKind.RemoteClosed,
                            "KCP dead link detected (no ACK, peer may be gone)."));
                        return;
                    }
                }
            }
        }
        catch (OperationCanceledException)
        {
            // 正常关闭
        }
    }

    private void RaiseClosed(Exception? ex)
    {
        if (Interlocked.Exchange(ref _closed, 1) != 0)
            return;
        _runCts?.Cancel();
        Closed?.Invoke(ex);
    }

    private static uint NowMs() => (uint)(Environment.TickCount64 & 0xFFFFFFFF);

    public async ValueTask DisposeAsync() => await CloseAsync().ConfigureAwait(false);
}
