// Clean-room C# implementation of the KCP protocol
// (algorithm by skywind3000, https://github.com/skywind3000/KCP).
// Wire format is the standard 24-byte KCP header (all little-endian),
// interoperable with xtaci/kcp-go as used by qcontinuum's KCP server.

using System.Buffers.Binary;

namespace QContinuum.Kcp;

internal sealed class KcpSegment
{
    public uint Conv;
    public byte Cmd;
    public byte Frg;
    public ushort Wnd;
    public uint Ts;
    public uint Sn;
    public uint Una;
    public byte[] Data = Array.Empty<byte>();

    // Transmission state (ikcp segment fields).
    public uint ResendTs;
    public uint Rto;
    public int Fastack;
    public int Xmit;
}

internal sealed class KcpAckItem
{
    public uint Sn;
    public uint Ts;
}

/// <summary>
/// KCP: reliable, ordered, message-oriented transport over UDP.
/// Not thread-safe — all methods must be invoked serially from one pump loop.
/// </summary>
internal sealed class Kcp
{
    private const byte CmdPush = 81;
    private const byte CmdAck = 82;
    private const byte CmdWask = 83;
    private const byte CmdWins = 84;

    private const int AskSend = 1;

    private const int Overhead = 24;

    private const uint RtoNdl = 30;
    private const uint RtoMin = 100;
    private const uint RtoDef = 200;
    private const uint RtoMax = 60000;

    private const uint ThreshMin = 2;
    private const uint ProbeInit = 7000;
    private const uint ProbeLimit = 120000;

    private const int MtuDef = 1400;
    private const int IntervalDef = 100;
    private const int WndSndDef = 32;
    private const int WndRcvDef = 128;

    private readonly uint _conv;
    private uint _sndUna;
    private uint _sndNxt;
    private uint _rcvNxt;
    private uint _ssthresh = ThreshMin;

    private uint _rxRttval;
    private uint _rxSrtt;
    private uint _rxRto = RtoDef;
    private uint _rxMinrto = RtoMin;

    private uint _sndWnd = WndSndDef;
    private uint _rcvWnd = WndRcvDef;
    private uint _rmtWnd = WndRcvDef;
    private uint _cwnd = 1;

    private uint _probe;
    private uint _mtu = MtuDef;
    private uint _mss;
    private uint _interval = IntervalDef;
    private uint _tsFlush;

    private int _nodelay;
    private int _updated;
    private uint _tsProbe;
    private uint _probeWait;
    private readonly uint _deadLink = 20;

    private int _fastresend;
    private int _nocwnd;
    private uint _incr;
    // Stream mode is not used: always message mode (frg counts fragments).

    private int _xmit;
    private uint _nrcvBuf;
    private uint _nsndBuf;
    private uint _nrcvQue;
    private uint _nsndQue;
    private int _state;

    private uint _current;
    private int _outUsed;

    private readonly LinkedList<KcpSegment> _sndQueue = new();
    private readonly LinkedList<KcpSegment> _rcvQueue = new();
    private readonly LinkedList<KcpSegment> _sndBuf = new();
    private readonly LinkedList<KcpSegment> _rcvBuf = new();
    private readonly List<KcpAckItem> _acklist = new();
    private readonly byte[] _outbuf;

    /// <summary>Called with each UDP datagram to transmit. Must not retain the buffer.</summary>
    public Action<byte[], int>? Output { get; set; }

    public int WaitSnd => (int)(_nsndBuf + _nsndQue);
    public uint Conv => _conv;

    /// <summary>0 = alive, -1 = dead link detected.</summary>
    public int State => _state;

    public Kcp(uint conv)
    {
        _conv = conv;
        _mss = _mtu - Overhead;
        _outbuf = new byte[(_mtu + Overhead) * 3];
    }

    // ------------------------------------------------------------ configuration

    /// <summary>Mirrors ikcp_nodelay(nodelay, interval, resend, nc).</summary>
    public void NoDelay(int nodelay, int interval, int resend, int nc)
    {
        if (nodelay >= 0)
        {
            _nodelay = nodelay;
            _rxMinrto = nodelay != 0 ? RtoNdl : RtoMin;
        }
        if (interval >= 0)
            _interval = (uint)Math.Clamp(interval, 10, 5000);
        if (resend >= 0) _fastresend = resend;
        if (nc >= 0) _nocwnd = nc;
    }

    public void WndSize(int sndwnd, int rcvwnd)
    {
        if (sndwnd > 0) _sndWnd = (uint)sndwnd;
        if (rcvwnd > 0) _rcvWnd = (uint)rcvwnd;
    }

    public int SetMtu(int mtu)
    {
        if (mtu < 50 || mtu < Overhead) return -1;
        _mtu = (uint)mtu;
        _mss = _mtu - Overhead;
        return 0;
    }

    // ---------------------------------------------------------------- helpers

    private static int TimeDiff(uint later, uint earlier) => unchecked((int)(later - earlier));

    private static void EncodeSeg(Span<byte> dst, KcpSegment seg, int dataLen)
    {
        BinaryPrimitives.WriteUInt32LittleEndian(dst, seg.Conv);
        dst[4] = seg.Cmd;
        dst[5] = seg.Frg;
        BinaryPrimitives.WriteUInt16LittleEndian(dst.Slice(6), seg.Wnd);
        BinaryPrimitives.WriteUInt32LittleEndian(dst.Slice(8), seg.Ts);
        BinaryPrimitives.WriteUInt32LittleEndian(dst.Slice(12), seg.Sn);
        BinaryPrimitives.WriteUInt32LittleEndian(dst.Slice(16), seg.Una);
        BinaryPrimitives.WriteUInt32LittleEndian(dst.Slice(20), (uint)dataLen);
    }

    private void Emit(byte[] data, int size)
    {
        var output = Output;
        if (output == null) return;
        var copy = new byte[size];
        Buffer.BlockCopy(data, 0, copy, 0, size);
        output(copy, size);
    }

    private int WndUnused() => _nrcvQue < _rcvWnd ? (int)(_rcvWnd - _nrcvQue) : 0;

    private void UpdateAck(int rtt)
    {
        if (_rxSrtt == 0)
        {
            _rxSrtt = (uint)rtt;
            _rxRttval = (uint)rtt / 2;
        }
        else
        {
            int delta = rtt - (int)_rxSrtt;
            if (delta < 0) delta = -delta;
            _rxRttval = (3 * _rxRttval + (uint)delta) / 4;
            _rxSrtt = (uint)((7 * _rxSrtt + rtt) / 8);
            if (_rxSrtt < 1) _rxSrtt = 1;
        }
        uint rto = _rxSrtt + Math.Max(_interval, 4 * _rxRttval);
        _rxRto = Math.Clamp(rto, _rxMinrto, RtoMax);
    }

    private void ShrinkBuf()
    {
        _sndUna = _sndBuf.First != null ? _sndBuf.First.Value.Sn : _sndNxt;
    }

    private void ParseAck(uint sn)
    {
        if (TimeDiff(sn, _sndUna) < 0 || TimeDiff(sn, _sndNxt) >= 0)
            return;
        var node = _sndBuf.First;
        while (node != null)
        {
            var next = node.Next;
            var seg = node.Value;
            if (seg.Sn == sn)
            {
                _sndBuf.Remove(node);
                _nsndBuf--;
                break;
            }
            if (TimeDiff(sn, seg.Sn) < 0)
                break;
            node = next;
        }

        // Congestion window growth (classic ikcp), skipped when nocwnd.
        if (_nocwnd == 0 && TimeDiff(sn, _sndUna) >= 0 && _cwnd < _rmtWnd)
        {
            uint mss = _mss;
            if (_cwnd < _ssthresh)
            {
                _cwnd++;
                _incr += mss;
            }
            else
            {
                if (_incr < mss) _incr = mss;
                _incr += (mss * mss) / _incr + (mss / 16);
                if ((_cwnd + 1) * mss <= _incr)
                    _cwnd++;
            }
            if (_cwnd > _rmtWnd)
            {
                _cwnd = _rmtWnd;
                _incr = _rmtWnd * mss;
            }
        }
    }

    private void ParseFastAck(uint sn)
    {
        if (TimeDiff(sn, _sndUna) < 0 || TimeDiff(sn, _sndNxt) >= 0)
            return;
        foreach (var seg in _sndBuf)
        {
            if (TimeDiff(sn, seg.Sn) < 0)
                break;
            if (sn != seg.Sn)
                seg.Fastack++;
        }
    }

    private void ParseUna(uint una)
    {
        var node = _sndBuf.First;
        while (node != null)
        {
            var next = node.Next;
            if (TimeDiff(una, node.Value.Sn) > 0)
            {
                _sndBuf.Remove(node);
                _nsndBuf--;
            }
            else break;
            node = next;
        }
    }

    private void ParseData(KcpSegment newseg)
    {
        uint sn = newseg.Sn;
        if (TimeDiff(sn, _rcvNxt + _rcvWnd) >= 0 || TimeDiff(sn, _rcvNxt) < 0)
            return;

        // Insert sorted into rcv_buf, drop duplicates.
        var node = _rcvBuf.Last;
        bool repeat = false;
        while (node != null)
        {
            var seg = node.Value;
            if (seg.Sn == sn) { repeat = true; break; }
            if (TimeDiff(sn, seg.Sn) > 0) break;
            node = node.Previous;
        }
        if (repeat) return;
        if (node == null)
            _rcvBuf.AddFirst(newseg);
        else
            _rcvBuf.AddAfter(node, newseg);
        _nrcvBuf++;

        // Move in-order data from rcv_buf to rcv_queue.
        while (_rcvBuf.First != null)
        {
            var seg = _rcvBuf.First.Value;
            if (seg.Sn == _rcvNxt && _nrcvQue < _rcvWnd)
            {
                _rcvBuf.RemoveFirst();
                _nrcvBuf--;
                _rcvQueue.AddLast(seg);
                _nrcvQue++;
                _rcvNxt++;
            }
            else break;
        }
    }

    // ------------------------------------------------------------------ input

    /// <summary>Feed one received UDP datagram. Returns 0 on success, negative on error.</summary>
    public int Input(byte[] data, int offset, int size, uint current)
    {
        if (size < Overhead) return -1;
        int pos = offset;
        int end = offset + size;

        while (pos + Overhead <= end)
        {
            var head = data.AsSpan(pos, Overhead);
            uint conv = BinaryPrimitives.ReadUInt32LittleEndian(head);
            if (conv != _conv) return -1;

            byte cmd = head[4];
            byte frg = head[5];
            ushort wnd = BinaryPrimitives.ReadUInt16LittleEndian(head.Slice(6));
            uint ts = BinaryPrimitives.ReadUInt32LittleEndian(head.Slice(8));
            uint sn = BinaryPrimitives.ReadUInt32LittleEndian(head.Slice(12));
            uint una = BinaryPrimitives.ReadUInt32LittleEndian(head.Slice(16));
            uint segLen = BinaryPrimitives.ReadUInt32LittleEndian(head.Slice(20));

            if (cmd != CmdPush && cmd != CmdAck && cmd != CmdWask && cmd != CmdWins)
                return -3;
            if (pos + Overhead + segLen > end)
                return -2;

            var seg = new KcpSegment
            {
                Conv = conv, Cmd = cmd, Frg = frg, Wnd = wnd,
                Ts = ts, Sn = sn, Una = una,
                Data = new byte[segLen],
            };
            if (segLen > 0)
                Buffer.BlockCopy(data, pos + Overhead, seg.Data, 0, (int)segLen);

            _rmtWnd = wnd;
            ParseUna(una);
            ShrinkBuf();

            switch (cmd)
            {
                case CmdAck:
                    if (TimeDiff(current, ts) >= 0)
                        UpdateAck(TimeDiff(current, ts));
                    ParseAck(sn);
                    ParseFastAck(sn);
                    ShrinkBuf();
                    break;
                case CmdPush:
                    if (TimeDiff(sn, _rcvNxt + _rcvWnd) < 0)
                    {
                        _acklist.Add(new KcpAckItem { Sn = sn, Ts = ts });
                        if (TimeDiff(sn, _rcvNxt) >= 0)
                            ParseData(seg);
                    }
                    break;
                case CmdWask:
                    _probe |= AskSend;
                    break;
                case CmdWins:
                    break;
            }

            pos += Overhead + (int)segLen;
        }
        return 0;
    }

    public int Input(byte[] data, uint current) => Input(data, 0, data.Length, current);

    // ------------------------------------------------------------------- send

    /// <summary>Queue a message for sending. Returns 0 on success, negative on error.</summary>
    public int Send(byte[] buffer, int offset, int size)
    {
        if (size < 0) return -2;
        int count = size <= (int)_mss ? 1 : (size + (int)_mss - 1) / (int)_mss;
        if (count == 0) count = 1;
        if (count > 255) return -2;

        for (int i = 0; i < count; i++)
        {
            int segSize = Math.Min((int)_mss, size - i * (int)_mss);
            var segData = new byte[Math.Max(segSize, 0)];
            if (segSize > 0)
                Buffer.BlockCopy(buffer, offset + i * (int)_mss, segData, 0, segSize);
            _sndQueue.AddLast(new KcpSegment
            {
                Data = segData,
                Frg = (byte)(count - i - 1), // message mode
            });
            _nsndQue++;
        }
        return 0;
    }

    public int Send(byte[] buffer) => Send(buffer, 0, buffer.Length);

    // ------------------------------------------------------------------ receive

    /// <summary>
    /// Receive one complete message. Returns bytes copied (&gt;0),
    /// -1 when no complete message is available, -3 when the buffer is too small.
    /// </summary>
    public int Receive(byte[] buffer)
    {
        if (_rcvQueue.Count == 0) return -1;

        // In-order delivery guarantees the head run ending with frg == 0,
        // with fragment numbers descending by 1, is exactly one whole message.
        var run = new List<LinkedListNode<KcpSegment>>();
        int expect = -1;
        for (var node = _rcvQueue.First; node != null; node = node.Next)
        {
            var seg = node.Value;
            if (expect == -1)
            {
                run.Add(node);
                if (seg.Frg == 0) break;
                expect = seg.Frg - 1;
            }
            else if (seg.Frg == expect)
            {
                run.Add(node);
                if (seg.Frg == 0) break;
                expect--;
            }
            else break;
        }

        if (run.Count == 0 || run[^1].Value.Frg != 0)
            return -1; // head message incomplete

        int total = 0;
        foreach (var n in run) total += n.Value.Data.Length;
        if (total > buffer.Length) return -3;

        int pos = 0;
        foreach (var n in run)
        {
            var d = n.Value.Data;
            Buffer.BlockCopy(d, 0, buffer, pos, d.Length);
            pos += d.Length;
            _rcvQueue.Remove(n);
            _nrcvQue--;
        }
        return total;
    }

    // ------------------------------------------------------------------- drive

    /// <summary>Drive the protocol. Must be called at least every interval ms.</summary>
    public void Update(uint current)
    {
        _current = current;
        if (_updated == 0)
        {
            _updated = 1;
            _tsFlush = current;
        }

        int slap = TimeDiff(current, _tsFlush);
        if (slap >= 10000 || slap < -10000)
        {
            _tsFlush = current;
            slap = 0;
        }
        if (slap >= 0)
        {
            _tsFlush += _interval;
            if (TimeDiff(current, _tsFlush) >= 0)
                _tsFlush = current + _interval;
            Flush();
        }
    }

    /// <summary>Earliest time <see cref="Update"/> should be called again.</summary>
    public uint Check(uint current)
    {
        if (_updated == 0) return current;
        uint target = _tsFlush;
        foreach (var seg in _sndBuf)
        {
            if (seg.Xmit != 0 && TimeDiff(seg.ResendTs, target) < 0)
                target = seg.ResendTs;
        }
        if (TimeDiff(target, current) <= 0) return current;
        return target;
    }

    /// <summary>Flush pending ACKs, window probes and data segments.</summary>
    public void Flush()
    {
        uint current = _current;
        if (_updated == 0) return;

        // ACKs
        foreach (var ack in _acklist)
        {
            var seg = new KcpSegment
            {
                Conv = _conv, Cmd = CmdAck, Frg = 0,
                Wnd = (ushort)WndUnused(), Ts = ack.Ts, Sn = ack.Sn, Una = _rcvNxt,
            };
            EncodeSeg(_outbuf.AsSpan(_outUsed), seg, 0);
            _outUsed += Overhead;
        }
        _acklist.Clear();

        // Window probing
        if (_rmtWnd == 0)
        {
            if (_probeWait == 0)
            {
                _probeWait = ProbeInit;
                _tsProbe = current + _probeWait;
            }
            else if (TimeDiff(current, _tsProbe) >= 0)
            {
                _probeWait = Math.Min(_probeWait + _probeWait / 2, ProbeLimit);
                if (_probeWait < ProbeInit) _probeWait = ProbeInit;
                _tsProbe = current + _probeWait;
                _probe |= AskSend;
            }
        }
        else
        {
            _tsProbe = 0;
            _probeWait = 0;
        }

        if ((_probe & AskSend) != 0)
        {
            var seg = new KcpSegment
            {
                Conv = _conv, Cmd = CmdWask, Frg = 0,
                Wnd = (ushort)WndUnused(), Ts = current, Una = _rcvNxt,
            };
            EncodeSeg(_outbuf.AsSpan(_outUsed), seg, 0);
            _outUsed += Overhead;
        }
        _probe = 0;

        // Congestion window
        uint cwnd = Math.Min(_sndWnd, _rmtWnd);
        if (_nocwnd == 0)
            cwnd = Math.Min(_cwnd, cwnd);

        // Move data from snd_queue to snd_buf
        while (TimeDiff(_sndNxt, _sndUna + cwnd) < 0)
        {
            var first = _sndQueue.First;
            if (first == null) break;
            var seg = first.Value;
            _sndQueue.RemoveFirst();
            _nsndQue--;

            seg.Conv = _conv;
            seg.Cmd = CmdPush;
            seg.Wnd = (ushort)WndUnused();
            seg.Ts = current;
            seg.Sn = _sndNxt++;
            seg.Una = _rcvNxt;
            seg.ResendTs = current;
            seg.Rto = _rxRto;
            seg.Fastack = 0;
            seg.Xmit = 0;
            _sndBuf.AddLast(seg);
            _nsndBuf++;
        }

        // Flush data segments
        foreach (var seg in _sndBuf)
        {
            bool needsend = false;
            bool lost = false;
            if (seg.Xmit == 0)
            {
                needsend = true;
                seg.Xmit = 1;
                seg.Rto = _rxRto;
                seg.ResendTs = current + seg.Rto;
            }
            else if (TimeDiff(current, seg.ResendTs) >= 0)
            {
                needsend = true;
                seg.Xmit++;
                _xmit++;
                lost = true;
                if (_nodelay == 0)
                    seg.Rto += Math.Max(seg.Rto, _rxRto);
                else
                {
                    int step = _nodelay < 2 ? seg.Xmit : _nodelay;
                    seg.Rto += _rxRto * (uint)step / 2;
                }
                seg.ResendTs = current + seg.Rto;
            }
            else if (seg.Fastack >= _fastresend && _fastresend > 0)
            {
                needsend = true;
                seg.Xmit++;
                seg.Fastack = 0;
                seg.ResendTs = current + seg.Rto;
            }

            if (needsend)
            {
                seg.Ts = current;
                seg.Wnd = (ushort)WndUnused();
                seg.Una = _rcvNxt;

                int need = Overhead + seg.Data.Length;
                if (_outUsed + need > _mtu)
                {
                    Emit(_outbuf, _outUsed);
                    _outUsed = 0;
                }
                EncodeSeg(_outbuf.AsSpan(_outUsed), seg, seg.Data.Length);
                Buffer.BlockCopy(seg.Data, 0, _outbuf, _outUsed + Overhead, seg.Data.Length);
                _outUsed += need;

                if (_outUsed >= (int)_mtu)
                {
                    Emit(_outbuf, _outUsed);
                    _outUsed = 0;
                }

                if (lost && _nocwnd == 0)
                {
                    // Back off on timeout retransmit (classic ikcp).
                    _ssthresh = cwnd / 2;
                    if (_ssthresh < ThreshMin) _ssthresh = ThreshMin;
                    _cwnd = 1;
                    _incr = _mss;
                }
            }

            if (seg.Xmit >= _deadLink)
                _state = -1;
        }

        if (_outUsed > 0)
        {
            Emit(_outbuf, _outUsed);
            _outUsed = 0;
        }
    }
}
