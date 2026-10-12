using System.Buffers.Binary;

namespace QContinuum.Protocol;

/// <summary>
/// qcontinuum 线协议帧编解码。
/// 帧格式（与 Go 端 network/message_header.go 严格一致）：
/// <c>[Length: int32 LE][ID: int32 LE][body: Length bytes]</c>，
/// 其中 Length 仅为 body 长度（不含 8 字节头）。
/// </summary>
public static class Framing
{
    public const int HeaderSize = 8;

    /// <summary>单包 body 上限，与 Go 端 maxMessageLength (100 MiB) 一致。</summary>
    public const int MaxMessageSize = 100 * 1024 * 1024;

    /// <summary>把头写入 8 字节 span（小端）。</summary>
    public static void WriteHeader(Span<byte> dest, int length, int id)
    {
        if (dest.Length < HeaderSize)
            throw new ArgumentException($"Header needs {HeaderSize} bytes.", nameof(dest));
        BinaryPrimitives.WriteInt32LittleEndian(dest, length);
        BinaryPrimitives.WriteInt32LittleEndian(dest.Slice(4), id);
    }

    /// <summary>
    /// 尝试从 span 解析帧头。成功返回 true；长度非法时抛出
    /// <see cref="QContinuumException"/>（<see cref="QErrorKind.ProtocolViolation"/>
    /// 或 <see cref="QErrorKind.MessageTooLarge"/>）。
    /// </summary>
    public static bool TryReadHeader(ReadOnlySpan<byte> src, out int length, out int id)
    {
        length = 0;
        id = 0;
        if (src.Length < HeaderSize)
            return false;

        length = BinaryPrimitives.ReadInt32LittleEndian(src);
        id = BinaryPrimitives.ReadInt32LittleEndian(src.Slice(4));

        if (length < 0)
            throw new QContinuumException(
                QErrorKind.ProtocolViolation,
                $"Invalid frame length {length} (negative). Remote may be sending corrupt data.");
        if (length > MaxMessageSize)
            throw new QContinuumException(
                QErrorKind.MessageTooLarge,
                $"Frame length {length} exceeds max {MaxMessageSize}.");
        return true;
    }

    /// <summary>打包一个完整协议包：头 + body。</summary>
    public static byte[] Encode(int id, ReadOnlySpan<byte> body)
    {
        if (body.Length > MaxMessageSize)
            throw new QContinuumException(
                QErrorKind.MessageTooLarge,
                $"Body length {body.Length} exceeds max {MaxMessageSize}.");
        var packet = new byte[HeaderSize + body.Length];
        WriteHeader(packet, body.Length, id);
        body.CopyTo(packet.AsSpan(HeaderSize));
        return packet;
    }
}

/// <summary>
/// 流式组包器：把传输层收到的数据报（TCP 流分片 / WS 消息 / KCP 消息）
/// 还原为完整 <see cref="QMessage"/>。可处理粘包、半包、跨包。
/// 非线程安全：调用方保证串行 Feed。
/// </summary>
public sealed class PacketReader
{
    private byte[] _buf = new byte[64 * 1024];
    private int _count;

    /// <summary>把收到的数据追加到组包缓冲区。</summary>
    public void Feed(ReadOnlySpan<byte> data)
    {
        if (data.Length == 0)
            return;
        EnsureCapacity(_count + data.Length);
        data.CopyTo(_buf.AsSpan(_count));
        _count += data.Length;
    }

    /// <summary>
    /// 尝试取出一个完整消息。有完整包返回 true；数据不足返回 false；
    /// 帧头非法时抛出 <see cref="QContinuumException"/>。
    /// </summary>
    public bool TryTake(out QMessage? message)
    {
        message = null;
        if (_count < Framing.HeaderSize)
            return false;

        if (!Framing.TryReadHeader(_buf.AsSpan(0, Framing.HeaderSize), out int length, out int id))
            return false; // 不可达：上面已保证 8 字节

        if (_count < Framing.HeaderSize + length)
            return false;

        var body = new byte[length];
        _buf.AsSpan(Framing.HeaderSize, length).CopyTo(body);

        int consumed = Framing.HeaderSize + length;
        int remain = _count - consumed;
        if (remain > 0)
            Buffer.BlockCopy(_buf, consumed, _buf, 0, remain);
        _count = remain;

        message = new QMessage(id, body);
        return true;
    }

    /// <summary>丢弃已缓冲但尚未组包的数据（重连后重置用）。</summary>
    public void Reset() => _count = 0;

    private void EnsureCapacity(int needed)
    {
        if (needed <= _buf.Length)
            return;
        int next = _buf.Length * 2;
        while (next < needed)
            next *= 2;
        if (next > Framing.HeaderSize + Framing.MaxMessageSize)
            throw new QContinuumException(
                QErrorKind.MessageTooLarge,
                $"Reassembly buffer would exceed max message size.");
        Array.Resize(ref _buf, next);
    }
}
