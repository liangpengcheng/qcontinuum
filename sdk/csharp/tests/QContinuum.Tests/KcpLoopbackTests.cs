using QContinuum.Kcp;
using KcpCore = QContinuum.Kcp.Kcp;

namespace QContinuum.Tests;

/// <summary>
/// KCP 自环测试：两个 Kcp 实例经内存队列互联，模拟丢包，
/// 验证可靠、有序、分片消息传输（不依赖网络）。
/// </summary>
public class KcpLoopbackTests
{
    [Fact]
    public void Loopback_ReliableOrderedDelivery_WithPacketLoss()
    {
        var a2b = new Queue<byte[]>();
        var b2a = new Queue<byte[]>();
        var rnd = new Random(1234);

        var a = new KcpCore(0x11223344);
        var b = new KcpCore(0x11223344);
        a.Output = (buf, len) => MaybeEnqueue(a2b, buf, len, rnd, dropRate: 0.2);
        b.Output = (buf, len) => MaybeEnqueue(b2a, buf, len, rnd, dropRate: 0.2);
        // 对齐 Go 端 DefaultKCPConfig
        a.NoDelay(1, 10, 2, 1); a.WndSize(4096, 4096); a.SetMtu(1400);
        b.NoDelay(1, 10, 2, 1); b.WndSize(4096, 4096); b.SetMtu(1400);

        const int count = 200;
        var sent = new List<byte[]>();
        var payloadRnd = new Random(42);
        for (int i = 0; i < count; i++)
        {
            // 每 10 个包构造一个超 mss 的大包，覆盖分片路径
            int size = i % 10 == 0 ? 3000 + i * 7 : 64 + i;
            var data = new byte[size];
            payloadRnd.NextBytes(data);
            BitConverter.GetBytes(i).CopyTo(data, 0); // 序号用于校验有序
            sent.Add(data);
            Assert.Equal(0, a.Send(data));
        }

        var received = new List<byte[]>();
        var recvBuf = new byte[64 * 1024];
        uint now = 1_000_000;
        for (int t = 0; t < 6000 && received.Count < count; t++)
        {
            now += 10;
            Pump(a, b2a, now);
            Pump(b, a2b, now);
            a.Update(now);
            b.Update(now);
            int n;
            while ((n = b.Receive(recvBuf)) > 0)
            {
                var copy = new byte[n];
                Buffer.BlockCopy(recvBuf, 0, copy, 0, n);
                received.Add(copy);
            }
        }

        Assert.Equal(count, received.Count);
        for (int i = 0; i < count; i++)
        {
            Assert.Equal(i, BitConverter.ToInt32(received[i], 0)); // 有序
            Assert.Equal(sent[i], received[i]);                    // 内容一致
        }
    }

    [Fact]
    public void Loopback_Bidirectional()
    {
        var a2b = new Queue<byte[]>();
        var b2a = new Queue<byte[]>();
        var rnd = new Random(7);
        var a = new KcpCore(99);
        var b = new KcpCore(99);
        a.Output = (buf, len) => MaybeEnqueue(a2b, buf, len, rnd, 0);
        b.Output = (buf, len) => MaybeEnqueue(b2a, buf, len, rnd, 0);
        a.NoDelay(1, 10, 2, 1);
        b.NoDelay(1, 10, 2, 1);

        var ping = new byte[] { 1, 2, 3, 4 };
        var pong = new byte[] { 5, 6, 7, 8 };
        Assert.Equal(0, a.Send(ping));

        var buf = new byte[4096];
        uint now = 5000;
        bool gotPing = false, gotPong = false;
        for (int t = 0; t < 500 && !(gotPing && gotPong); t++)
        {
            now += 10;
            Pump(a, b2a, now);
            Pump(b, a2b, now);
            a.Update(now);
            b.Update(now);
            int n;
            while ((n = b.Receive(buf)) > 0)
            {
                gotPing = true;
                Assert.Equal(ping, buf[..n]);
                Assert.Equal(0, b.Send(pong)); // 收到 ping 回 pong
            }
            while ((n = a.Receive(buf)) > 0)
            {
                gotPong = true;
                Assert.Equal(pong, buf[..n]);
            }
        }
        Assert.True(gotPing && gotPong);
    }

    [Fact]
    public void ConvMismatch_IsRejected()
    {
        var kcp = new KcpCore(111);
        kcp.Output = (_, _) => { };
        // 构造一个 conv 不匹配的包
        var bad = new byte[24];
        BitConverter.GetBytes(222u).CopyTo(bad, 0); // conv=222
        bad[4] = 81; // PUSH
        Assert.Equal(-1, kcp.Input(bad, 1000));
    }

    private static void MaybeEnqueue(Queue<byte[]> q, byte[] buf, int len, Random rnd, double dropRate)
    {
        if (rnd.NextDouble() < dropRate)
            return; // 模拟丢包
        var copy = new byte[len];
        Buffer.BlockCopy(buf, 0, copy, 0, len);
        q.Enqueue(copy);
    }

    private static void Pump(KcpCore kcp, Queue<byte[]> inbound, uint now)
    {
        while (inbound.Count > 0)
            kcp.Input(inbound.Dequeue(), now);
    }
}
