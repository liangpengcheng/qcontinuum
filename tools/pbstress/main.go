// pbstress: protobuf 压测客户端（Go 版）。
//
// 对 qcontinuum 压测服务端发起 protobuf Echo（MsgId=2001）压测，覆盖
// TCP / WebSocket / KCP 三协议。支持混沌（churn + 畸形包注入）与
// 服务端内存采样。用法示例：
//
//   go run ./tools/pbstress --tcp 700 --ws 650 --kcp 650 --qps 10 \
//       --duration 12h --server-pid 12345 --log-dir /tmp/pbstress
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	testpb "github.com/liangpengcheng/qcontinuum/generated/go"
	_kcp "github.com/xtaci/kcp-go/v5"
	"google.golang.org/protobuf/proto"
)

const (
	msgID         = 2001 // testpb.MsgId_ECHO
	headerSize    = 8
	maxMsgSize    = 100 * 1024 * 1024
	echoTimeout   = 5 * time.Second
	wsReadLimit   = 4 << 20
)

type config struct {
	host                                   string
	tcpPort, wsPort, kcpPort               int
	tcpCount, wsCount, kcpCount            int
	qps                                    int
	payloadSize                            int
	duration                               time.Duration
	serverPID                              int
	logDir                                 string
	metricsInterval                        time.Duration
	chaos                                  bool
	chaosInterval                          time.Duration
	churnFraction, injectFraction          float64
}

// ---------------------------------------------------------------- stats

type stats struct {
	sent, echoed, mismatch, timeout     atomic.Uint64
	disconnects, reconnects             atomic.Uint64
	injected, reconnectAfterInject      atomic.Uint64
	mu                                 sync.Mutex
	rtts                               []float64
}

func (s *stats) addRTT(ms float64) {
	s.mu.Lock()
	s.rtts = append(s.rtts, ms)
	s.mu.Unlock()
}

func (s *stats) snapshot() (sent, echoed, mismatch, timeout, disc, reconn, inj, reconnInj uint64, p50, p99, max float64) {
	sent = s.sent.Load()
	echoed = s.echoed.Load()
	mismatch = s.mismatch.Load()
	timeout = s.timeout.Load()
	disc = s.disconnects.Load()
	reconn = s.reconnects.Load()
	inj = s.injected.Load()
	reconnInj = s.reconnectAfterInject.Load()
	s.mu.Lock()
	rtts := s.rtts
	s.rtts = nil
	s.mu.Unlock()
	if len(rtts) == 0 {
		return
	}
	sort.Float64s(rtts)
	p50 = rtts[len(rtts)/2]
	p99 = rtts[int(float64(len(rtts))*0.99)]
	max = rtts[len(rtts)-1]
	return
}

// ---------------------------------------------------------------- framing

type frameReader struct {
	buf []byte
}

func (r *frameReader) feed(data []byte) {
	r.buf = append(r.buf, data...)
}

// tryTake 取出一个完整帧，返回 (id, body, ok)。帧头非法时返回错误。
func (r *frameReader) tryTake() (int32, []byte, bool, error) {
	if len(r.buf) < headerSize {
		return 0, nil, false, nil
	}
	length := int32(binary.LittleEndian.Uint32(r.buf[0:4]))
	id := int32(binary.LittleEndian.Uint32(r.buf[4:8]))
	if length < 0 {
		return 0, nil, false, fmt.Errorf("negative frame length %d", length)
	}
	if length > maxMsgSize {
		return 0, nil, false, fmt.Errorf("frame too large %d", length)
	}
	if len(r.buf) < headerSize+int(length) {
		return 0, nil, false, nil
	}
	body := make([]byte, length)
	copy(body, r.buf[headerSize:headerSize+int(length)])
	r.buf = append(r.buf[:0], r.buf[headerSize+int(length):]...)
	return id, body, true, nil
}

func (r *frameReader) reset() { r.buf = r.buf[:0] }

func encodeFrame(id int32, body []byte) []byte {
	pkt := make([]byte, headerSize+len(body))
	binary.LittleEndian.PutUint32(pkt[0:4], uint32(len(body)))
	binary.LittleEndian.PutUint32(pkt[4:8], uint32(id))
	copy(pkt[headerSize:], body)
	return pkt
}

// ---------------------------------------------------------------- conn

// pbConn 抽象三种传输的收发。write 必须完整写入一帧；read 返回原始字节流。
type pbConn interface {
	write(pkt []byte) error
	read() ([]byte, error) // 返回下一批字节；EOF 表示远端关闭
	close() error
}

type tcpConn struct{ c net.Conn }

func (t *tcpConn) write(pkt []byte) error {
	_, err := t.c.Write(pkt)
	return err
}
func (t *tcpConn) read() ([]byte, error) {
	buf := make([]byte, 64*1024)
	n, err := t.c.Read(buf)
	return buf[:n], err
}
func (t *tcpConn) close() error { return t.c.Close() }

type wsConn struct {
	c  *websocket.Conn
	mu sync.Mutex // gorilla/websocket 禁止并发写；sendLoop 与混沌注入可能同时写
}

func (w *wsConn) write(pkt []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.c.WriteMessage(websocket.BinaryMessage, pkt)
}
func (w *wsConn) read() ([]byte, error) {
	_, data, err := w.c.ReadMessage()
	return data, err
}
func (w *wsConn) close() error { return w.c.Close() }

type kcpConn struct{ c net.Conn }

func (k *kcpConn) write(pkt []byte) error {
	_, err := k.c.Write(pkt)
	return err
}
func (k *kcpConn) read() ([]byte, error) {
	buf := make([]byte, 64*1024)
	n, err := k.c.Read(buf)
	return buf[:n], err
}
func (k *kcpConn) close() error { return k.c.Close() }

func dialTransport(kind, host string, port int) (pbConn, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	switch kind {
	case "tcp":
		c, err := net.DialTimeout("tcp", addr, 10*time.Second)
		if err != nil {
			return nil, err
		}
		if tc, ok := c.(*net.TCPConn); ok {
			tc.SetNoDelay(true)
		}
		return &tcpConn{c}, nil
	case "ws":
		u := url.URL{Scheme: "ws", Host: addr, Path: "/ws"}
		d := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
		c, _, err := d.Dial(u.String(), http.Header{})
		if err != nil {
			return nil, err
		}
		c.SetReadLimit(wsReadLimit)
		return &wsConn{c: c}, nil
	case "kcp":
		c, err := _kcp.DialWithOptions(addr, nil, 0, 0)
		if err != nil {
			return nil, err
		}
		return &kcpConn{c}, nil
	}
	return nil, fmt.Errorf("unknown transport %s", kind)
}

// ---------------------------------------------------------------- worker

type pending struct {
	seq      int64
	sentNano int64
	payload  []byte
}

type worker struct {
	id      int
	kind    string
	cfg     *config
	st      *stats
	payload []byte

	mu      sync.Mutex
	conn    pbConn
	reader  frameReader
	pendMu  sync.Mutex
	pending map[int64]*pending
	seq     atomic.Int64

	closed   atomic.Bool
	stopCh   chan struct{}
	stopOnce sync.Once
}

func newWorker(id int, kind string, cfg *config, st *stats) *worker {
	payload := make([]byte, cfg.payloadSize)
	rand.Read(payload)
	return &worker{
		id: id, kind: kind, cfg: cfg, st: st,
		payload: payload,
		pending: make(map[int64]*pending),
		stopCh:  make(chan struct{}),
	}
}

func (w *worker) dial() error {
	c, err := dialTransport(w.kind, w.cfg.host, portFor(w.kind, w.cfg))
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.conn = c
	w.reader.reset()
	w.mu.Unlock()
	return nil
}

func portFor(kind string, cfg *config) int {
	switch kind {
	case "tcp":
		return cfg.tcpPort
	case "ws":
		return cfg.wsPort
	case "kcp":
		return cfg.kcpPort
	}
	return 0
}

func (w *worker) run(globalStop <-chan struct{}) {
	first := true
	for {
		select {
		case <-globalStop:
			return
		case <-w.stopCh:
			return
		default:
		}
		// 建连（重试直到成功或退出）
		for {
			select {
			case <-globalStop:
				return
			case <-w.stopCh:
				return
			default:
			}
			if err := w.dial(); err == nil {
				break
			}
			select {
			case <-globalStop:
				return
			case <-w.stopCh:
				return
			case <-time.After(time.Second):
			}
		}
		if !first {
			w.st.reconnects.Add(1)
		}
		first = false

		disc := make(chan struct{})
		var discOnce sync.Once
		signalDisc := func() { discOnce.Do(func() { close(disc) }) }

		var inner sync.WaitGroup
		inner.Add(2)
		go func() { defer inner.Done(); w.sendLoop(globalStop, disc) }()
		go func() { defer inner.Done(); w.readLoop(globalStop, disc, signalDisc) }()

		select {
		case <-globalStop:
			inner.Wait()
			return
		case <-w.stopCh:
			inner.Wait()
			return
		case <-disc:
			inner.Wait()
			// 回到外层循环重连
		}
	}
}

func (w *worker) sendLoop(globalStop, disc <-chan struct{}) {
	ticker := time.NewTicker(time.Second / time.Duration(w.cfg.qps))
	defer ticker.Stop()
	for {
		select {
		case <-globalStop:
			return
		case <-w.stopCh:
			return
		case <-disc:
			return
		case <-ticker.C:
		}
		seq := w.seq.Add(1)
		now := time.Now().UnixNano()
		echo := &testpb.Echo{Seq: seq, SendTimeUnixNano: now, Payload: w.payload}
		body, err := proto.Marshal(echo)
		if err != nil {
			continue
		}
		pkt := encodeFrame(msgID, body)
		w.pendMu.Lock()
		w.pending[seq] = &pending{seq: seq, sentNano: now, payload: w.payload}
		w.pendMu.Unlock()
		w.st.sent.Add(1)
		w.mu.Lock()
		c := w.conn
		w.mu.Unlock()
		if c == nil {
			w.dropPending(seq)
			continue
		}
		if err := c.write(pkt); err != nil {
			w.dropPending(seq)
		}
	}
}

func (w *worker) dropPending(seq int64) {
	w.pendMu.Lock()
	delete(w.pending, seq)
	w.pendMu.Unlock()
}

func (w *worker) readLoop(globalStop, disc <-chan struct{}, signalDisc func()) {
	for {
		select {
		case <-globalStop:
			return
		case <-w.stopCh:
			return
		case <-disc:
			return
		default:
		}
		w.mu.Lock()
		c := w.conn
		w.mu.Unlock()
		if c == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		data, err := c.read()
		if err != nil {
			w.onDisconnect()
			signalDisc()
			return
		}
		if len(data) == 0 {
			continue
		}
		badFrame := false
		w.mu.Lock()
		w.reader.feed(data)
		for {
			id, body, ok, ferr := w.reader.tryTake()
			if ferr != nil {
				badFrame = true
				break
			}
			if !ok {
				break
			}
			w.mu.Unlock()
			w.onMessage(id, body)
			w.mu.Lock()
		}
		w.mu.Unlock()
		if badFrame {
			w.onDisconnect() // 协议违规：关闭并重连
			signalDisc()
			return
		}

		// 超时扫描
		now := time.Now().UnixNano()
		w.pendMu.Lock()
		for seq, p := range w.pending {
			if now-p.sentNano > int64(echoTimeout) {
				delete(w.pending, seq)
				w.st.timeout.Add(1)
			}
		}
		w.pendMu.Unlock()
	}
}

func (w *worker) onMessage(id int32, body []byte) {
	if id != msgID {
		return
	}
	var echo testpb.Echo
	if err := proto.Unmarshal(body, &echo); err != nil {
		w.st.mismatch.Add(1)
		return
	}
	w.pendMu.Lock()
	p, ok := w.pending[echo.Seq]
	if ok {
		delete(w.pending, echo.Seq)
	}
	w.pendMu.Unlock()
	if !ok {
		return
	}
	if !bytes.Equal(p.payload, echo.Payload) {
		w.st.mismatch.Add(1)
		return
	}
	rttMs := float64(time.Now().UnixNano()-echo.SendTimeUnixNano) / 1e6
	w.st.addRTT(rttMs)
	w.st.echoed.Add(1)
}

func (w *worker) onDisconnect() {
	w.mu.Lock()
	c := w.conn
	w.conn = nil
	w.mu.Unlock()
	if c != nil {
		c.close()
	}
	w.st.disconnects.Add(1)
	// 清理 pending（会计为 timeout，避免泄漏）
	w.pendMu.Lock()
	n := len(w.pending)
	w.pending = make(map[int64]*pending)
	w.pendMu.Unlock()
	w.st.timeout.Add(uint64(n))
}

// injectMalformed 注入畸形包，服务端应断开该连接
func (w *worker) injectMalformed(kind int) {
	w.mu.Lock()
	c := w.conn
	w.mu.Unlock()
	if c == nil {
		return
	}
	var pkt []byte
	switch kind {
	case 0:
		pkt = []byte{0x03, 0x00, 0x00} // 截断头
	case 1:
		pkt = []byte{0xFF, 0xFF, 0xFF, 0x7F, 0x01, 0x00, 0x00, 0x00} // 巨大长度
	case 2:
		pkt = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x00, 0x00, 0x00} // 负长度
	default:
		pkt = []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x11, 0x22, 0x33}
	}
	if c.write(pkt) == nil {
		w.st.injected.Add(1)
	}
}

func (w *worker) stop() {
	w.closed.Store(true)
	w.stopOnce.Do(func() { close(w.stopCh) })
	w.mu.Lock()
	c := w.conn
	w.conn = nil
	w.mu.Unlock()
	if c != nil {
		c.close()
	}
}

// ---------------------------------------------------------------- main

func main() {
	cfg := &config{}
	flag.StringVar(&cfg.host, "host", "127.0.0.1", "")
	flag.IntVar(&cfg.tcpPort, "tcp-port", 9001, "")
	flag.IntVar(&cfg.wsPort, "ws-port", 9002, "")
	flag.IntVar(&cfg.kcpPort, "kcp-port", 9003, "")
	flag.IntVar(&cfg.tcpCount, "tcp", 700, "")
	flag.IntVar(&cfg.wsCount, "ws", 650, "")
	flag.IntVar(&cfg.kcpCount, "kcp", 650, "")
	flag.IntVar(&cfg.qps, "qps", 10, "")
	flag.IntVar(&cfg.payloadSize, "payload", 256, "")
	flag.DurationVar(&cfg.duration, "duration", 12*time.Hour, "")
	flag.IntVar(&cfg.serverPID, "server-pid", 0, "")
	flag.StringVar(&cfg.logDir, "log-dir", "/tmp/pbstress", "")
	flag.DurationVar(&cfg.metricsInterval, "metrics-interval", 5*time.Minute, "")
	flag.BoolVar(&cfg.chaos, "chaos", true, "")
	flag.DurationVar(&cfg.chaosInterval, "chaos-interval", 2*time.Minute, "")
	flag.Float64Var(&cfg.churnFraction, "churn", 0.02, "")
	flag.Float64Var(&cfg.injectFraction, "inject", 0.01, "")
	flag.Parse()

	os.MkdirAll(cfg.logDir, 0755)
	fmt.Printf("[pbstress] tcp=%d ws=%d kcp=%d qps=%d payload=%d duration=%v chaos=%v log=%s\n",
		cfg.tcpCount, cfg.wsCount, cfg.kcpCount, cfg.qps, cfg.payloadSize, cfg.duration, cfg.chaos, cfg.logDir)

	stTcp, stWs, stKcp := &stats{}, &stats{}, &stats{}
	stop := make(chan struct{})
	var wg sync.WaitGroup

	var workersMu sync.Mutex
	workers := []*worker{}
	spawn := func(kind string, st *stats) *worker {
		workersMu.Lock()
		defer workersMu.Unlock()
		w := newWorker(len(workers), kind, cfg, st)
		workers = append(workers, w)
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.run(stop)
		}()
		return w
	}

	// 分批建连
	plan := []struct {
		kind string
		st   *stats
		n    int
	}{
		{"tcp", stTcp, cfg.tcpCount},
		{"ws", stWs, cfg.wsCount},
		{"kcp", stKcp, cfg.kcpCount},
	}
	for _, p := range plan {
		for i := 0; i < p.n; i++ {
			spawn(p.kind, p.st)
			if i%50 == 49 {
				time.Sleep(200 * time.Millisecond)
			}
		}
		fmt.Printf("[pbstress] spawned %d %s workers\n", p.n, p.kind)
	}

	// 指标上报
	metricsDone := make(chan struct{})
	go func() {
		defer close(metricsDone)
		logPath := filepath.Join(cfg.logDir, "metrics.jsonl")
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Println("metrics log open failed:", err)
			return
		}
		defer f.Close()
		ticker := time.NewTicker(cfg.metricsInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				rssKb := serverRSS(cfg.serverPID)
				for _, s := range []struct {
					name string
					st   *stats
				}{{"tcp", stTcp}, {"ws", stWs}, {"kcp", stKcp}} {
					sent, echoed, mismatch, timeout, disc, reconn, inj, reconnInj, p50, p99, max := s.st.snapshot()
					rec := map[string]interface{}{
						"at":                 time.Now().UTC().Format(time.RFC3339),
						"transport":          s.name,
						"sent":               sent,
						"echoed":             echoed,
						"mismatch":           mismatch,
						"timeout":            timeout,
						"disconnects":        disc,
						"reconnects":         reconn,
						"injected":           inj,
						"reconnectAfterInj":  reconnInj,
						"rttP50Ms":           round2(p50),
						"rttP99Ms":           round2(p99),
						"rttMaxMs":           round2(max),
						"serverRssMB":        round2(float64(rssKb) / 1024),
					}
					line, _ := json.Marshal(rec)
					f.Write(append(line, '\n'))
					fmt.Printf("[metrics] %s sent=%d echoed=%d mismatch=%d timeout=%d disc=%d reconn=%d p50=%.1fms p99=%.1fms rss=%.1fMB\n",
						s.name, sent, echoed, mismatch, timeout, disc, reconn, p50, p99, float64(rssKb)/1024)
				}
			}
		}
	}()

	// 混沌
	var chaosWg sync.WaitGroup
	if cfg.chaos {
		chaosWg.Add(1)
		go func() {
			defer chaosWg.Done()
			ticker := time.NewTicker(cfg.chaosInterval)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
				}
				workersMu.Lock()
				alive := []*worker{}
				for _, w := range workers {
					if !w.closed.Load() {
						alive = append(alive, w)
					}
				}
				workersMu.Unlock()
				if len(alive) == 0 {
					continue
				}
				rand.Shuffle(len(alive), func(i, j int) { alive[i], alive[j] = alive[j], alive[i] })
				// 畸形包注入
				nInject := max(1, int(float64(len(alive))*cfg.injectFraction))
				for i := 0; i < nInject && i < len(alive); i++ {
					alive[i].injectMalformed(rand.Intn(4))
				}
				// churn：老连接退出 + 新连接加入（保持总数）
				nChurn := int(float64(len(alive)) * cfg.churnFraction)
				for i := 0; i < nChurn; i++ {
					victim := alive[len(alive)-1-i]
					victim.stop()
					spawn(victim.kind, statsFor(victim.kind, stTcp, stWs, stKcp))
				}
				fmt.Printf("[chaos] injected=%d churned=%d\n", nInject, nChurn)
			}
		}()
	}

	// 等待时长
	select {
	case <-time.After(cfg.duration):
	case <-waitInterrupt():
	}
	close(stop)
	chaosWg.Wait()

	workersMu.Lock()
	all := append([]*worker{}, workers...)
	workersMu.Unlock()
	for _, w := range all {
		w.stop()
	}
	wg.Wait()
	<-metricsDone // 让上报协程退出（它监听 stop）

	for _, s := range []struct {
		name string
		st   *stats
	}{{"tcp", stTcp}, {"ws", stWs}, {"kcp", stKcp}} {
		sent, echoed, mismatch, timeout, disc, reconn, inj, _, p50, p99, max := s.st.snapshot()
		fmt.Printf("[final] %s sent=%d echoed=%d mismatch=%d timeout=%d disc=%d reconn=%d injected=%d p50=%.1fms p99=%.1fms max=%.1fms\n",
			s.name, sent, echoed, mismatch, timeout, disc, reconn, inj, p50, p99, max)
	}
	fmt.Println("[pbstress] done")
}

func statsFor(kind string, tcp, ws, kcp *stats) *stats {
	switch kind {
	case "tcp":
		return tcp
	case "ws":
		return ws
	}
	return kcp
}

func serverRSS(pid int) int64 {
	if pid <= 0 {
		return -1
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			var kb int64
			fmt.Sscanf(line, "VmRSS: %d kB", &kb)
			return kb
		}
	}
	return -1
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func waitInterrupt() <-chan struct{} {
	// 简化：不处理信号，靠 duration 结束。实际部署可用 signal.Notify。
	c := make(chan struct{})
	return c
}
