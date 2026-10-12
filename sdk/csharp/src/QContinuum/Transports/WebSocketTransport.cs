using System.Buffers;
using System.Net.WebSockets;
using QContinuum.Protocol;

namespace QContinuum.Transports;

/// <summary>
/// WebSocket 传输：一个协议包 = 一条二进制 WS 消息（与 Go 端 ginhttp 对接，路径 /ws）。
/// 接收侧可处理分片（多条 WS 消息拼成一个数据报）与粘连（一条 WS 消息含多包由上层组帧器拆分）。
/// </summary>
internal sealed class WebSocketTransport : ITransport
{
    private const string WsPath = "/ws";

    private ClientWebSocket? _ws;
    private readonly SemaphoreSlim _sendLock = new(1, 1);
    private CancellationTokenSource? _runCts;
    private Task? _recvTask;
    private int _closed;

    public event Action<byte[], int>? DatagramReceived;
    public event Action<Exception?>? Closed;

    public async Task ConnectAsync(string host, int port, TimeSpan timeout, CancellationToken ct)
    {
        var ws = new ClientWebSocket();
        var uri = new Uri($"ws://{host}:{port}{WsPath}");
        try
        {
            using var timeoutCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
            timeoutCts.CancelAfter(timeout);
            await ws.ConnectAsync(uri, timeoutCts.Token).ConfigureAwait(false);
        }
        catch (OperationCanceledException) when (!ct.IsCancellationRequested)
        {
            ws.Dispose();
            throw new QContinuumException(QErrorKind.ConnectTimeout,
                $"WebSocket connect to {uri} timed out after {timeout.TotalSeconds:F1}s.");
        }
        catch (WebSocketException ex)
        {
            ws.Dispose();
            throw new QContinuumException(MapWsError(ex),
                $"WebSocket connect to {uri} failed: {ex.Message}", ex);
        }

        _ws = ws;
        _runCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
        _recvTask = Task.Run(() => ReceiveLoopAsync(_runCts.Token));
    }

    public async Task SendAsync(ReadOnlyMemory<byte> packet, CancellationToken ct)
    {
        var ws = _ws;
        if (ws == null)
            throw new QNotConnectedException();
        await _sendLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            await ws.SendAsync(packet, WebSocketMessageType.Binary, endOfMessage: true, ct)
                .ConfigureAwait(false);
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            throw new QContinuumException(QErrorKind.SendFailed,
                $"WebSocket send failed: {ex.Message}", ex);
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
        var ws = _ws;
        if (ws != null)
        {
            try
            {
                if (ws.State == WebSocketState.Open)
                    await ws.CloseAsync(WebSocketCloseStatus.NormalClosure, "bye",
                        CancellationToken.None).ConfigureAwait(false);
            }
            catch { /* ignore */ }
            ws.Dispose();
        }
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
        var ws = _ws!;
        var chunk = ArrayPool<byte>.Shared.Rent(8 * 1024);
        // WS 分片重组缓冲：一个数据报可能跨多条 WS 消息
        byte[]? frag = null;
        int fragCount = 0;
        try
        {
            while (!ct.IsCancellationRequested && ws.State == WebSocketState.Open)
            {
                WebSocketReceiveResult result;
                try
                {
                    result = await ws.ReceiveAsync(new ArraySegment<byte>(chunk), ct).ConfigureAwait(false);
                }
                catch (OperationCanceledException)
                {
                    break;
                }
                catch (Exception ex)
                {
                    RaiseClosed(new QContinuumException(MapWsError(ex),
                        $"WebSocket receive failed: {ex.Message}", ex));
                    return;
                }

                if (result.MessageType == WebSocketMessageType.Close)
                {
                    RaiseClosed(new QContinuumException(QErrorKind.RemoteClosed,
                        $"WebSocket closed by remote: {result.CloseStatus} {result.CloseStatusDescription}"));
                    return;
                }
                if (result.MessageType != WebSocketMessageType.Binary)
                    continue; // 忽略文本/Ping 等控制帧

                if (!result.EndOfMessage)
                {
                    // 累积分片
                    if (frag == null)
                    {
                        frag = ArrayPool<byte>.Shared.Rent(64 * 1024);
                        fragCount = 0;
                    }
                    else if (fragCount + result.Count > frag.Length)
                    {
                        var bigger = ArrayPool<byte>.Shared.Rent((fragCount + result.Count) * 2);
                        Buffer.BlockCopy(frag, 0, bigger, 0, fragCount);
                        ArrayPool<byte>.Shared.Return(frag);
                        frag = bigger;
                    }
                    Buffer.BlockCopy(chunk, 0, frag, fragCount, result.Count);
                    fragCount += result.Count;
                    continue;
                }

                if (frag != null)
                {
                    Buffer.BlockCopy(chunk, 0, frag, fragCount, result.Count);
                    fragCount += result.Count;
                    Deliver(frag, fragCount);
                    ArrayPool<byte>.Shared.Return(frag);
                    frag = null;
                    fragCount = 0;
                }
                else
                {
                    Deliver(chunk, result.Count);
                }
            }
        }
        finally
        {
            ArrayPool<byte>.Shared.Return(chunk);
            if (frag != null)
                ArrayPool<byte>.Shared.Return(frag);
        }
    }

    private void Deliver(byte[] buffer, int count)
    {
        try
        {
            DatagramReceived?.Invoke(buffer, count);
        }
        catch (Exception ex)
        {
            RaiseClosed(new QContinuumException(QErrorKind.ProtocolViolation,
                $"Datagram handler threw: {ex.Message}", ex));
        }
    }

    private void RaiseClosed(Exception? ex)
    {
        if (Interlocked.Exchange(ref _closed, 1) != 0)
            return;
        _runCts?.Cancel();
        Closed?.Invoke(ex);
    }

    private static QErrorKind MapWsError(Exception ex) => ex switch
    {
        WebSocketException wsex => wsex.WebSocketErrorCode switch
        {
            WebSocketError.ConnectionClosedPrematurely => QErrorKind.RemoteClosed,
            _ => QErrorKind.HostUnreachable,
        },
        TimeoutException => QErrorKind.ConnectTimeout,
        _ => QErrorKind.Unknown,
    };

    public async ValueTask DisposeAsync() => await CloseAsync().ConfigureAwait(false);
}
