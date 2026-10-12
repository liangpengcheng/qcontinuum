using Google.Protobuf;
using QContinuum.Protocol;
using QContinuum.Test;

namespace QContinuum.Tests;

public class FramingTests
{
    [Fact]
    public void HeaderLayout_MatchesGo_LittleEndian()
    {
        // Go: binary.LittleEndian.PutUint32(length), PutUint32(id)
        var packet = Framing.Encode(2001, new byte[] { 1, 2, 3 });
        Assert.Equal(8 + 3, packet.Length);
        Assert.Equal(3, packet[0] | (packet[1] << 8) | (packet[2] << 16) | (packet[3] << 24));
        Assert.Equal(2001, packet[4] | (packet[5] << 8) | (packet[6] << 16) | (packet[7] << 24));
        Assert.Equal(new byte[] { 1, 2, 3 }, packet[8..]);
    }

    [Fact]
    public void PacketReader_SinglePacket()
    {
        var reader = new PacketReader();
        var body = new byte[] { 9, 8, 7 };
        reader.Feed(Framing.Encode(42, body));
        Assert.True(reader.TryTake(out var msg));
        Assert.NotNull(msg);
        Assert.Equal(42, msg!.Id);
        Assert.Equal(body, msg.Body);
        Assert.False(reader.TryTake(out _));
    }

    [Fact]
    public void PacketReader_SplitAcrossFeeds()
    {
        var reader = new PacketReader();
        var packet = Framing.Encode(7, new byte[] { 1, 2, 3, 4, 5 });
        // 头拆成 3+5，body 拆成 2+3
        reader.Feed(packet.AsSpan(0, 3));
        Assert.False(reader.TryTake(out _));
        reader.Feed(packet.AsSpan(3, 5));
        Assert.False(reader.TryTake(out _));
        reader.Feed(packet.AsSpan(8, 2));
        Assert.False(reader.TryTake(out _));
        reader.Feed(packet.AsSpan(10, 3));
        Assert.True(reader.TryTake(out var msg));
        Assert.Equal(7, msg!.Id);
        Assert.Equal(new byte[] { 1, 2, 3, 4, 5 }, msg.Body);
    }

    [Fact]
    public void PacketReader_CoalescedPackets()
    {
        var reader = new PacketReader();
        var p1 = Framing.Encode(1, new byte[] { 0xAA });
        var p2 = Framing.Encode(2, new byte[] { 0xBB, 0xCC });
        var combined = new byte[p1.Length + p2.Length];
        Buffer.BlockCopy(p1, 0, combined, 0, p1.Length);
        Buffer.BlockCopy(p2, 0, combined, p1.Length, p2.Length);
        reader.Feed(combined);
        Assert.True(reader.TryTake(out var m1));
        Assert.True(reader.TryTake(out var m2));
        Assert.False(reader.TryTake(out _));
        Assert.Equal(1, m1!.Id);
        Assert.Equal(2, m2!.Id);
        Assert.Equal(new byte[] { 0xBB, 0xCC }, m2.Body);
    }

    [Fact]
    public void PacketReader_EmptyBody()
    {
        var reader = new PacketReader();
        reader.Feed(Framing.Encode(99, Array.Empty<byte>()));
        Assert.True(reader.TryTake(out var msg));
        Assert.Equal(99, msg!.Id);
        Assert.Empty(msg.Body);
    }

    [Fact]
    public void PacketReader_NegativeLength_ThrowsProtocolViolation()
    {
        var reader = new PacketReader();
        var bad = new byte[8];
        bad[0] = 0xFF; bad[1] = 0xFF; bad[2] = 0xFF; bad[3] = 0xFF; // -1
        reader.Feed(bad);
        var ex = Assert.Throws<QContinuumException>(() => reader.TryTake(out _));
        Assert.Equal(QErrorKind.ProtocolViolation, ex.Kind);
    }

    [Fact]
    public void PacketReader_Oversize_ThrowsMessageTooLarge()
    {
        var reader = new PacketReader();
        var bad = new byte[8];
        int tooBig = Framing.MaxMessageSize + 1;
        bad[0] = (byte)tooBig; bad[1] = (byte)(tooBig >> 8);
        bad[2] = (byte)(tooBig >> 16); bad[3] = (byte)(tooBig >> 24);
        reader.Feed(bad);
        var ex = Assert.Throws<QContinuumException>(() => reader.TryTake(out _));
        Assert.Equal(QErrorKind.MessageTooLarge, ex.Kind);
    }

    [Fact]
    public void Encode_Oversize_Throws()
    {
        var ex = Assert.Throws<QContinuumException>(() =>
            Framing.Encode(1, new byte[Framing.MaxMessageSize + 1]));
        Assert.Equal(QErrorKind.MessageTooLarge, ex.Kind);
    }

    [Fact]
    public void Protobuf_Echo_Roundtrip()
    {
        // 用真实生成的 protobuf 消息验证 body 编解码
        var echo = new Echo
        {
            Seq = 123456789,
            SendTimeUnixNano = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() * 1_000_000,
            Payload = Google.Protobuf.ByteString.CopyFrom(new byte[] { 1, 2, 3, 4 }),
        };
        var body = echo.ToByteArray();
        var packet = Framing.Encode((int)MsgId.Echo, body);

        var reader = new PacketReader();
        reader.Feed(packet);
        Assert.True(reader.TryTake(out var msg));
        Assert.Equal(2001, msg!.Id);
        var back = msg.Unpack<Echo>();
        Assert.Equal(123456789, back.Seq);
        Assert.Equal(echo.SendTimeUnixNano, back.SendTimeUnixNano);
        Assert.Equal(echo.Payload, back.Payload);
    }
}
