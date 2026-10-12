package kcp

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/liangpengcheng/qcontinuum/base"
	"github.com/liangpengcheng/qcontinuum/network"
	_kcp "github.com/xtaci/kcp-go/v5"
)

const maxKCPMessageLength = 100 * 1024 * 1024

// ============ KCP配置 ============

// KCPConfig KCP服务器配置
type KCPConfig struct {
	// 网络配置
	MTU         int // MTU大小，默认1400
	SndWnd      int // 发送窗口大小
	RcvWnd      int // 接收窗口大小
	ReadBuffer  int // 读缓冲区大小
	WriteBuffer int // 写缓冲区大小

	// 延迟优化参数（NoDelay参数）
	// NoDelay: 0-关闭, 1-开启
	NoDelay int
	// Interval: 内部更新间隔（毫秒），默认100ms
	Interval int
	// Resend: 快速重传阈值，0-关闭，2-2次ACK跨越将会直接重传
	Resend int
	// NC: 0-正常拥塞控制，1-关闭拥塞控制
	NC int

	// 流模式
	StreamMode bool
	// 写延迟
	WriteDelay bool
	// ACK无延迟
	ACKNoDelay bool

	// FEC配置（前向纠错）
	DataShards   int // 数据分片数，0表示禁用FEC
	ParityShards int // 校验分片数

	// DSCP配置（服务质量）
	DSCP int
}

// DefaultKCPConfig 返回默认配置（适合实时游戏）
func DefaultKCPConfig() *KCPConfig {
	return &KCPConfig{
		MTU:          1400,
		SndWnd:       4096,
		RcvWnd:       4096,
		ReadBuffer:   4 * 1024 * 1024, // 4MB
		WriteBuffer:  4 * 1024 * 1024, // 4MB
		NoDelay:      1,
		Interval:     10,
		Resend:       2,
		NC:           1,
		StreamMode:   false,
		WriteDelay:   false,
		ACKNoDelay:   true,
		DataShards:   0,
		ParityShards: 0,
		DSCP:         0,
	}
}

// FastKCPConfig 返回极速模式配置（最低延迟，适合FPS游戏）
func FastKCPConfig() *KCPConfig {
	return &KCPConfig{
		MTU:          1400,
		SndWnd:       4096,
		RcvWnd:       4096,
		ReadBuffer:   4 * 1024 * 1024,
		WriteBuffer:  4 * 1024 * 1024,
		NoDelay:      1,
		Interval:     5, // 更快的更新间隔
		Resend:       1, // 更激进的重传
		NC:           1,
		StreamMode:   false,
		WriteDelay:   false,
		ACKNoDelay:   true,
		DataShards:   0,
		ParityShards: 0,
		DSCP:         46, // EF（Expedited Forwarding）
	}
}

// ReliableKCPConfig 返回可靠模式配置（适合重要数据传输）
func ReliableKCPConfig() *KCPConfig {
	return &KCPConfig{
		MTU:          1400,
		SndWnd:       2048,
		RcvWnd:       2048,
		ReadBuffer:   4 * 1024 * 1024,
		WriteBuffer:  4 * 1024 * 1024,
		NoDelay:      0, // 使用正常模式
		Interval:     40,
		Resend:       0,
		NC:           0, // 开启拥塞控制
		StreamMode:   true,
		WriteDelay:   false,
		ACKNoDelay:   false,
		DataShards:   10, // 启用FEC
		ParityShards: 3,
		DSCP:         0,
	}
}

// ============ KCP专用缓冲区池 ============

const kcpBufferSize = 32 * 1024 // 32KB，适合KCP数据包

var kcpBufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, kcpBufferSize)
	},
}

func getKCPBuffer() []byte {
	return kcpBufferPool.Get().([]byte)
}

func putKCPBuffer(buf []byte) {
	if cap(buf) >= kcpBufferSize {
		kcpBufferPool.Put(buf[:kcpBufferSize])
	}
}

// ============ KCP Peer ============

// KCPPeerState peer状态
type KCPPeerState int32

const (
	KCPPeerStateConnected KCPPeerState = iota
	KCPPeerStateClosing
	KCPPeerStateClosed
)

// KCPPeer shares the underlying connection fields with AsyncClientPeer. The
// identical underlying struct permits pointer conversion without copying state,
// while keeping ID and Flag as declared fields for existing keyed literals.
type KCPPeer network.AsyncClientPeer

// NewKCPPeer 创建KCP peer
func NewKCPPeer(conn *_kcp.UDPSession, proc *network.Processor) *KCPPeer {
	peer := network.NewStreamClientPeer(&kcpConnWrapper{conn: conn, lastActive: time.Now().Unix()}, proc)
	return (*KCPPeer)(peer.AsyncClientPeer)
}

// GetState 获取peer状态
func (p *KCPPeer) GetState() KCPPeerState {
	return KCPPeerState((*network.AsyncClientPeer)(p).GetState())
}

// Close 关闭连接
func (p *KCPPeer) Close() {
	(*network.AsyncClientPeer)(p).Close()
}

// SendMessage 发送protobuf消息
func (p *KCPPeer) SendMessage(msg proto.Message, msgid int32) error {
	return (*network.AsyncClientPeer)(p).SendMessage(msg, msgid)
}

// SendMessageBuffer 发送原始数据
func (p *KCPPeer) SendMessageBuffer(data []byte) error {
	if len(data) > maxKCPMessageLength+8 {
		return errors.New("message too large")
	}
	return (*network.AsyncClientPeer)(p).SendMessageBuffer(data)
}

// TransmitMsg 转发消息
func (p *KCPPeer) TransmitMsg(msg *network.Message) error {
	return (*network.AsyncClientPeer)(p).TransmitMsg(msg)
}

// RemoteAddr 返回远程地址
func (p *KCPPeer) RemoteAddr() net.Addr {
	if p.Connection != nil {
		return p.Connection.RemoteAddr()
	}
	return nil
}

// GetStats 获取统计信息
func (p *KCPPeer) GetStats() (bytesRead, bytesWritten uint64, lastActive time.Time) {
	if conn, ok := p.Connection.(*kcpConnWrapper); ok {
		return atomic.LoadUint64(&conn.bytesRead), atomic.LoadUint64(&conn.bytesWritten),
			time.Unix(atomic.LoadInt64(&conn.lastActive), 0)
	}
	return (*network.AsyncClientPeer)(p).GetStats()
}

// CheckAfter 超时检查
func (p *KCPPeer) CheckAfter(t time.Duration) {
	(*network.AsyncClientPeer)(p).CheckAfter(t)
}

// ToClientPeer 转换为兼容的ClientPeer（用于消息处理）
// Returns the same shared peer without copying mutable business state.
func (p *KCPPeer) ToClientPeer() *network.ClientPeer {
	return (*network.AsyncClientPeer)(p).ToClientPeer()
}

// ============ KCP服务器 ============

// AsyncKCPServer 异步KCP服务器
type AsyncKCPServer struct {
	listener    *_kcp.Listener
	processor   *network.Processor
	config      *KCPConfig
	running     int32
	started     int32
	peers       sync.Map
	wg          sync.WaitGroup
	acceptWG    sync.WaitGroup
	lifecycleMu sync.Mutex
	acceptCount uint64
	connCount   uint64
	stopChan    chan struct{}
	stopOnce    sync.Once
}

// NewAsyncKCPServer 创建异步KCP服务器（使用默认配置）
func NewAsyncKCPServer(host string) (*AsyncKCPServer, error) {
	return NewAsyncKCPServerWithConfig(host, DefaultKCPConfig())
}

// NewAsyncKCPServerWithConfig 创建异步KCP服务器（使用自定义配置）
func NewAsyncKCPServerWithConfig(host string, config *KCPConfig) (*AsyncKCPServer, error) {
	if config == nil {
		config = DefaultKCPConfig()
	}

	// 创建监听器，支持FEC
	lis, err := _kcp.ListenWithOptions(host, nil, config.DataShards, config.ParityShards)
	if err != nil {
		return nil, err
	}

	// 设置DSCP
	if err := lis.SetDSCP(config.DSCP); err != nil {
		lis.Close()
		return nil, err
	}

	// 设置缓冲区大小
	if err := lis.SetReadBuffer(config.ReadBuffer); err != nil {
		lis.Close()
		return nil, err
	}
	if err := lis.SetWriteBuffer(config.WriteBuffer); err != nil {
		lis.Close()
		return nil, err
	}

	return &AsyncKCPServer{
		listener: lis,
		config:   config,
		stopChan: make(chan struct{}),
	}, nil
}

// SetProcessor 设置消息处理器
func (s *AsyncKCPServer) SetProcessor(proc *network.Processor) {
	s.processor = proc
}

// GetConfig 获取当前配置
func (s *AsyncKCPServer) GetConfig() *KCPConfig {
	return s.config
}

// StartAsync 启动异步KCP服务器
func (s *AsyncKCPServer) StartAsync() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	select {
	case <-s.stopChan:
		return errors.New("server is stopped")
	default:
	}
	if s.processor == nil {
		return errors.New("processor not set")
	}

	if !atomic.CompareAndSwapInt32(&s.started, 0, 1) {
		return errors.New("server already started or stopped")
	}
	atomic.StoreInt32(&s.running, 1)
	s.acceptWG.Add(1)

	// KCP的accept循环在goroutine中运行
	go s.acceptLoop()

	return nil
}

// acceptLoop KCP连接接受循环
func (s *AsyncKCPServer) acceptLoop() {
	defer s.acceptWG.Done()
	for atomic.LoadInt32(&s.running) == 1 {
		if err := s.acceptOne(); err != nil {
			if atomic.LoadInt32(&s.running) == 1 {
				base.Zap().Sugar().Errorf("kcp accept error: %v", err)
				// 避免在极端错误下 CPU 占用过高
				time.Sleep(10 * time.Millisecond)
			}
			continue
		}
	}
	base.Zap().Sugar().Infof("kcp accept loop exited")
}

// acceptOne 接受一个KCP连接
func (s *AsyncKCPServer) acceptOne() error {
	conn, err := s.listener.AcceptKCP()
	if err != nil {
		return err
	}
	if atomic.LoadInt32(&s.running) == 0 {
		conn.Close()
		return errors.New("server is stopped")
	}

	base.Zap().Sugar().Infof("kcp remote address: %s", conn.RemoteAddr().String())

	// 使用配置设置KCP参数
	s.setupKCPConn(conn)

	// 创建KCP专用peer
	peer := NewKCPPeer(conn, s.processor)

	// 启动连接处理goroutine
	atomic.AddUint64(&s.acceptCount, 1)
	atomic.AddUint64(&s.connCount, 1)
	s.wg.Add(1)
	s.peers.Store(peer, struct{}{})
	go s.handleKCPConnection(peer)

	// 发送连接事件
	event := &network.Event{
		ID:   network.AddEvent,
		Peer: peer.ToClientPeer(),
	}

	select {
	case s.processor.EventChan <- event:
	default:
		base.Zap().Sugar().Warnf("event queue full, dropping add event")
	}

	return nil
}

// setupKCPConn 使用配置设置KCP连接参数
func (s *AsyncKCPServer) setupKCPConn(conn *_kcp.UDPSession) {
	conn.SetStreamMode(s.config.StreamMode)
	conn.SetWriteDelay(s.config.WriteDelay)
	conn.SetNoDelay(s.config.NoDelay, s.config.Interval, s.config.Resend, s.config.NC)
	conn.SetMtu(s.config.MTU)
	conn.SetWindowSize(s.config.SndWnd, s.config.RcvWnd)
	conn.SetACKNoDelay(s.config.ACKNoDelay)
}

// handleKCPConnection 处理KCP连接
func (s *AsyncKCPServer) handleKCPConnection(peer *KCPPeer) {
	defer s.wg.Done()
	defer s.peers.Delete(peer)
	defer func() {
		// 确保连接关闭
		peer.Close()

		// 减少连接计数
		atomic.AddUint64(&s.connCount, ^uint64(0))

		// 发送移除事件（修复：之前缺少这个通知）
		if s.processor != nil {
			event := &network.Event{
				ID:   network.RemoveEvent,
				Peer: peer.ToClientPeer(),
			}
			select {
			case s.processor.EventChan <- event:
			default:
				base.Zap().Sugar().Warnf("event queue full, dropping remove event")
			}
		}

		// 记录统计信息
		bytesRead, bytesWritten, lastActive := peer.GetStats()
		base.Zap().Sugar().Infof("kcp connection closed: addr=%v, read=%d, written=%d, last_active=%v",
			peer.RemoteAddr(), bytesRead, bytesWritten, lastActive)
	}()

	// 使用专用的消息读取器
	reader := network.NewAsyncMessageReader()
	defer reader.Release()

	// 使用专用的KCP缓冲区池
	for peer.GetState() == KCPPeerStateConnected {
		buffer := getKCPBuffer()

		n, err := peer.Connection.Read(buffer)
		if err != nil {
			putKCPBuffer(buffer)
			if peer.GetState() == KCPPeerStateConnected {
				base.Zap().Sugar().Infof("kcp read error: %v", err)
			}
			break
		}

		if n == 0 {
			putKCPBuffer(buffer)
			continue
		}

		// 处理接收到的数据
		messages, err := reader.FeedData(buffer[:n])
		putKCPBuffer(buffer) // 立即归还缓冲区

		if err != nil {
			base.Zap().Sugar().Warnf("kcp message parse error: %v", err)
			break
		}

		// 处理解析出的消息
		for _, zcMsg := range messages {
			if !network.DeliverZeroCopyMessage(s.processor, peer.ToClientPeer(), zcMsg) {
				base.Zap().Sugar().Warnf("kcp message queue full, dropping message")
			}
		}
	}
}

// Stop 停止KCP服务器

// 通知停止

// GetStats 获取服务器统计信息
func (s *AsyncKCPServer) GetStats() (acceptCount, connCount uint64) {
	return atomic.LoadUint64(&s.acceptCount), atomic.LoadUint64(&s.connCount)
}

// IsRunning 检查服务器是否在运行
func (s *AsyncKCPServer) IsRunning() bool {
	return atomic.LoadInt32(&s.running) == 1
}

// ============ KCP连接包装器（用于兼容net.Conn接口） ============

// kcpConnWrapper 将KCP连接包装为net.Conn接口
type kcpConnWrapper struct {
	conn         *_kcp.UDPSession
	bytesRead    uint64
	bytesWritten uint64
	lastActive   int64
}

func (w *kcpConnWrapper) Read(b []byte) (int, error) {
	n, err := w.conn.Read(b)
	if n > 0 {
		atomic.AddUint64(&w.bytesRead, uint64(n))
		atomic.StoreInt64(&w.lastActive, time.Now().Unix())
	}
	return n, err
}

func (w *kcpConnWrapper) Write(b []byte) (int, error) {
	n, err := w.conn.Write(b)
	if n > 0 {
		atomic.AddUint64(&w.bytesWritten, uint64(n))
	}
	return n, err
}

func (w *kcpConnWrapper) Close() error {
	if w.conn == nil {
		return nil
	}
	return w.conn.Close()
}

func (w *kcpConnWrapper) LocalAddr() net.Addr {
	if w.conn == nil {
		return nil
	}
	return w.conn.LocalAddr()
}

func (w *kcpConnWrapper) RemoteAddr() net.Addr {
	if w.conn == nil {
		return nil
	}
	return w.conn.RemoteAddr()
}

func (w *kcpConnWrapper) SetDeadline(t time.Time) error {
	return w.conn.SetDeadline(t)
}

func (w *kcpConnWrapper) SetReadDeadline(t time.Time) error {
	return w.conn.SetReadDeadline(t)
}

func (w *kcpConnWrapper) SetWriteDeadline(t time.Time) error {
	return w.conn.SetWriteDeadline(t)
}

// ============ 兼容性接口 ============

// Server kcp server (兼容旧接口)
type Server struct {
	*AsyncKCPServer
	Listener *_kcp.Listener // 保持兼容性
}

// NewKCPServer 创建KCP服务器 (兼容旧接口)
func NewKCPServer(host string) (*Server, error) {
	asyncServer, err := NewAsyncKCPServer(host)
	if err != nil {
		return nil, err
	}

	return &Server{
		AsyncKCPServer: asyncServer,
		Listener:       asyncServer.listener,
	}, nil
}

// NewKCPServerWithConfig 创建KCP服务器（自定义配置，兼容旧接口）
func NewKCPServerWithConfig(host string, config *KCPConfig) (*Server, error) {
	asyncServer, err := NewAsyncKCPServerWithConfig(host, config)
	if err != nil {
		return nil, err
	}

	return &Server{
		AsyncKCPServer: asyncServer,
		Listener:       asyncServer.listener,
	}, nil
}

// BlockAccept 阻塞收消息 (兼容旧接口)
func (s *Server) BlockAccept(proc *network.Processor) {
	s.SetProcessor(proc)

	if err := s.StartAsync(); err != nil {
		base.Zap().Sugar().Errorf("start async kcp server error: %v", err)
		return
	}

	base.Zap().Sugar().Infof("kcp server started in async mode")

	// 修复：使用阻塞式select避免CPU占用
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for atomic.LoadInt32(&s.running) == 1 {
		select {
		case <-s.stopChan:
			base.Zap().Sugar().Infof("kcp server received stop signal")
			return
		case <-ticker.C:
			// 定时检查，保持活跃但不占用CPU
		}
	}

	base.Zap().Sugar().Infof("kcp block accept exited")
}

// BlockAcceptOne 接受一个连接 (兼容旧接口)
func (s *Server) BlockAcceptOne(proc *network.Processor) error {
	s.SetProcessor(proc)

	if atomic.LoadInt32(&s.running) == 0 {
		if err := s.StartAsync(); err != nil {
			return err
		}
	}

	// 等待连接计数增加
	initialCount := atomic.LoadUint64(&s.connCount)
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	for atomic.LoadUint64(&s.connCount) == initialCount {
		select {
		case <-s.stopChan:
			return errors.New("server stopped")
		case <-ticker.C:
			if atomic.LoadInt32(&s.running) == 0 {
				return errors.New("server stopped")
			}
		}
	}

	return nil
}

// ============ 辅助函数（保持向后兼容） ============

// setupKcp 设置KCP连接参数（使用默认配置，向后兼容）
func setupKcp(conn *_kcp.UDPSession) {
	config := DefaultKCPConfig()
	conn.SetStreamMode(config.StreamMode)
	conn.SetWriteDelay(config.WriteDelay)
	conn.SetNoDelay(config.NoDelay, config.Interval, config.Resend, config.NC)
	conn.SetMtu(config.MTU)
	conn.SetWindowSize(config.SndWnd, config.RcvWnd)
	conn.SetACKNoDelay(config.ACKNoDelay)
}
