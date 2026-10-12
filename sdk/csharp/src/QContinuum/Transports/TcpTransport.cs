using System.Buffers;
using System.Net.Sockets;
using QContinuum.Protocol;

namespace QContinuum.Transports;

/// <summary>TCP 传输：流式收发，NoDelay 开启。</summary>
internal sealed class TcpTransport : ITransport
{
    private TcpClient? _client;
    private NetworkStream? _stream;
    private readonly SemaphoreSlim _sendLock = new(1, 1);
    private CancellationTokenSource? _runCts;
    private Task? _recvTask;
    private int _closed;

    public event Action<byte[], int>? DatagramReceived;
    public event Action<Exception?>? Closed;

    public async Task ConnectAsync(string host, int port, TimeSpan timeout, CancellationToken ct)
    {
        var client = new TcpClient { NoDelay = true };
        try
        {
            using var timeoutCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
            timeoutCts.CancelAfter(timeout);
            await client.ConnectAsync(host, port, timeoutCts.Token).ConfigureAwait(false);
        }
        catch (OperationCanceledException) when (!ct.IsCancellationRequested)
        {
            client.Dispose();
            throw new QContinuumException(QErrorKind.ConnectTimeout,
                $"TCP connect to {host}:{port} timed out after {timeout.TotalSeconds:F1}s.");
        }
        catch (SocketException ex)
        {
            client.Dispose();
            throw ClassifyConnect(ex, host, port);
        }

        _client = client;
        _stream = client.GetStream();
        _runCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
        _recvTask = Task.Run(() => ReceiveLoopAsync(_runCts.Token));
    }

    public async Task SendAsync(ReadOnlyMemory<byte> packet, CancellationToken ct)
    {
        var stream = _stream;
        if (stream == null)
            throw new QNotConnectedException();
        await _sendLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            await stream.WriteAsync(packet, ct).ConfigureAwait(false);
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            throw new QContinuumException(QErrorKind.SendFailed,
                $"TCP send failed: {ex.Message}", ex);
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
        try { _stream?.Close(); } catch { /* ignore */ }
        try { _client?.Close(); } catch { /* ignore */ }
        if (_recvTask != null)
        {
            try { await _recvTask.ConfigureAwait(false); } catch { /* ignore */ }
        }
        Closed?.Invoke(null);
        _runCts?.Dispose();
        _sendLock.Dispose();
    }

    private async Task ReceiveLoopAsync(CancellationToken ct)
    {
        var stream = _stream!;
        var buffer = ArrayPool<byte>.Shared.Rent(64 * 1024);
        try
        {
            while (!ct.IsCancellationRequested)
            {
                int n;
                try
                {
                    n = await stream.ReadAsync(buffer, ct).ConfigureAwait(false);
                }
                catch (OperationCanceledException)
                {
                    break; // 本地关闭
                }
                catch (Exception ex)
                {
                    RaiseClosed(ClassifyRead(ex));
                    return;
                }
                if (n == 0)
                {
                    RaiseClosed(new QContinuumException(QErrorKind.RemoteClosed,
                        "TCP connection closed by remote."));
                    return;
                }
                try
                {
                    DatagramReceived?.Invoke(buffer, n);
                }
                catch (Exception ex)
                {
                    // 上层组帧/回调异常不应杀死收包循环，记录并继续
                    RaiseClosed(new QContinuumException(QErrorKind.ProtocolViolation,
                        $"Datagram handler threw: {ex.Message}", ex));
                    return;
                }
            }
        }
        finally
        {
            ArrayPool<byte>.Shared.Return(buffer);
        }
    }

    private void RaiseClosed(Exception? ex)
    {
        if (Interlocked.Exchange(ref _closed, 1) != 0)
            return;
        _runCts?.Cancel();
        Closed?.Invoke(ex);
    }

    private static QContinuumException ClassifyConnect(SocketException ex, string host, int port)
    {
        var kind = ex.SocketErrorCode switch
        {
            SocketError.ConnectionRefused => QErrorKind.ConnectionRefused,
            SocketError.HostNotFound or SocketError.HostUnreachable => QErrorKind.HostUnreachable,
            SocketError.TimedOut => QErrorKind.ConnectTimeout,
            _ => QErrorKind.Unknown,
        };
        return new QContinuumException(kind,
            $"TCP connect to {host}:{port} failed: {ex.Message} ({ex.SocketErrorCode})", ex);
    }

    private static Exception ClassifyRead(Exception ex) => ex switch
    {
        SocketException sex => new QContinuumException(QErrorKind.RemoteClosed,
            $"TCP read failed: {sex.Message} ({sex.SocketErrorCode})", sex),
        IOException ioex => new QContinuumException(QErrorKind.RemoteClosed,
            $"TCP read failed: {ioex.Message}", ioex),
        _ => new QContinuumException(QErrorKind.Unknown,
            $"TCP read failed: {ex.Message}", ex),
    };

    public async ValueTask DisposeAsync() => await CloseAsync().ConfigureAwait(false);
}
