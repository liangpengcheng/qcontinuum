package kcp

import (
	"errors"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/liangpengcheng/qcontinuum/base"
	"github.com/liangpengcheng/qcontinuum/network"
	_kcp "github.com/xtaci/kcp-go"
)

// AsyncKCPServer 异步KCP服务器
type AsyncKCPServer struct {
	listener    *_kcp.Listener
	reactorPool *network.IOReactorPool
	processor   *network.Processor
	running     int32
	acceptCount uint64
	connCount   uint64
}

// NewAsyncKCPServer 创建异步KCP服务器
func NewAsyncKCPServer(host string) (*AsyncKCPServer, error) {
	lis, err := _kcp.ListenWithOptions(host, nil, 0, 0)
	if err != nil {
		return nil, err
	}

	// 默认DSCP
	if err := lis.SetDSCP(0); err != nil {
		lis.Close()
		return nil, err
	}
	if err := lis.SetReadBuffer(4194304); err != nil {
		lis.Close()
		return nil, err
	}
	if err := lis.SetWriteBuffer(4194304); err != nil {
		lis.Close()
		return nil, err
	}

	// 创建reactor池
	reactorPool, err := network.NewIOReactorPool(0)
	if err != nil {
		lis.Close()
		return nil, err
	}

	return &AsyncKCPServer{
		listener:    lis,
		reactorPool: reactorPool,
	}, nil
}

// SetProcessor 设置消息处理器
func (s *AsyncKCPServer) SetProcessor(proc *network.Processor) {
	s.processor = proc
}

// StartAsync 启动异步KCP服务器
func (s *AsyncKCPServer) StartAsync() error {
	atomic.StoreInt32(&s.running, 1)

	// KCP的accept循环在goroutine中运行
	go s.acceptLoop()

	return nil
}

// acceptLoop KCP连接接受循环
func (s *AsyncKCPServer) acceptLoop() {
	for atomic.LoadInt32(&s.running) == 1 {
		if err := s.acceptOne(); err != nil {
			base.Zap().Sugar().Errorf("kcp accept error: %v", err)
			break
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

	base.Zap().Sugar().Infof("kcp remote address: %s", conn.RemoteAddr().String())
	setupKcp(conn)

	// 将KCP连接包装为标准net.Conn
	netConn := &kcpConnWrapper{conn: conn}

	// 选择一个reactor处理这个连接
	reactor := s.reactorPool.GetReactor()

	// 创建异步peer
	peer, err := network.NewAsyncClientPeer(netConn, s.processor, reactor)
	if err != nil {
		conn.Close()
		return err
	}

	// 由于KCP是UDP连接，需要特殊处理文件描述符
	// 这里我们使用一个特殊的处理方式
	go s.handleKCPConnection(peer, conn)

	// 发送连接事件
	event := &network.Event{
		ID:   network.AddEvent,
		Peer: &network.ClientPeer{AsyncClientPeer: peer},
	}

	select {
	case s.processor.EventChan <- event:
	default:
		base.Zap().Sugar().Warnf("event queue full, dropping add event")
	}

	atomic.AddUint64(&s.acceptCount, 1)
	atomic.AddUint64(&s.connCount, 1)

	return nil
}

// handleKCPConnection 处理KCP连接（特殊处理）
func (s *AsyncKCPServer) handleKCPConnection(peer *network.AsyncClientPeer, conn *_kcp.UDPSession) {
	defer func() {
		peer.Close()
		conn.Close()
		atomic.AddUint64(&s.connCount, ^uint64(0)) // 原子递减
	}()

	// KCP连接的读取循环，使用零拷贝缓冲区池
	reader := network.NewAsyncMessageReader()
	defer reader.Release()

	for peer.GetState() == network.PeerStateConnected {
		// 使用缓冲区池
		buffer := network.GetBuffer()

		// 扩展缓冲区以适应KCP数据包
		if buffer.Cap() < 32*1024 {
			if err := buffer.Grow(32*1024 - buffer.Cap()); err != nil {
				buffer.Release()
				base.Zap().Sugar().Warnf("kcp buffer grow failed: %v", err)
				break
			}
		}

		n, err := conn.Read(buffer.Data())
		if err != nil {
			buffer.Release()
			base.Zap().Sugar().Debugf("kcp read error: %v", err)
			break
		}

		if n == 0 {
			buffer.Release()
			continue
		}

		// 处理接收到的数据
		messages, err := reader.FeedData(buffer.Data()[:n])
		buffer.Release() // 立即释放缓冲区

		if err != nil {
			base.Zap().Sugar().Warnf("kcp message parse error: %v", err)
			break
		}

		// 处理解析出的消息
		for _, zcMsg := range messages {
			msg := &network.Message{
				Peer: &network.ClientPeer{AsyncClientPeer: peer},
				Head: zcMsg.Head,
				Body: zcMsg.GetBody(),
			}

			if s.processor.ImmediateMode {
				if cb, ok := s.processor.CallbackMap[msg.Head.ID]; ok {
					cb(msg)
				} else if s.processor.UnHandledHandler != nil {
					s.processor.UnHandledHandler(msg)
				}
			} else {
				select {
				case s.processor.MessageChan <- msg:
				default:
					base.Zap().Sugar().Warnf("message queue full, dropping message")
				}
			}

			zcMsg.Release()
		}
	}
}

// Stop 停止KCP服务器
func (s *AsyncKCPServer) Stop() {
	atomic.StoreInt32(&s.running, 0)

	if s.listener != nil {
		s.listener.Close()
	}

	if s.reactorPool != nil {
		s.reactorPool.Close()
	}
}

// GetStats 获取服务器统计信息
func (s *AsyncKCPServer) GetStats() (acceptCount, connCount uint64) {
	return atomic.LoadUint64(&s.acceptCount), atomic.LoadUint64(&s.connCount)
}

// kcpConnWrapper 将KCP连接包装为net.Conn接口
type kcpConnWrapper struct {
	conn *_kcp.UDPSession
}

func (w *kcpConnWrapper) Read(b []byte) (int, error) {
	return w.conn.Read(b)
}

func (w *kcpConnWrapper) Write(b []byte) (int, error) {
	return w.conn.Write(b)
}

func (w *kcpConnWrapper) Close() error {
	return w.conn.Close()
}

func (w *kcpConnWrapper) LocalAddr() net.Addr {
	return w.conn.LocalAddr()
}

func (w *kcpConnWrapper) RemoteAddr() net.Addr {
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

// File 返回文件描述符 - KCP连接的特殊实现
func (w *kcpConnWrapper) File() (*os.File, error) {
	// KCP是基于UDP的，没有直接的文件描述符
	// 这里返回一个虚拟的文件描述符用于兼容
	return nil, errors.New("kcp connection does not support file descriptor")
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

// BlockAccept 阻塞收消息 (兼容旧接口)
func (s *Server) BlockAccept(proc *network.Processor) {
	s.SetProcessor(proc)

	if err := s.StartAsync(); err != nil {
		base.Zap().Sugar().Errorf("start async kcp server error: %v", err)
		return
	}

	base.Zap().Sugar().Infof("kcp server started in async mode")

	// 保持运行直到停止
	for atomic.LoadInt32(&s.running) == 1 {
		select {
		case <-proc.EventChan:
			// 处理事件，但不在这里处理，由processor处理
		default:
			// 短暂休眠避免CPU占用
			//time.Sleep(10 * time.Millisecond)
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
	for atomic.LoadUint64(&s.connCount) == initialCount {
		if atomic.LoadInt32(&s.running) == 0 {
			return errors.New("server stopped")
		}
		// 短暂等待
		//time.Sleep(1 * time.Millisecond)
	}

	return nil
}

func setupKcp(conn *_kcp.UDPSession) {
	conn.SetStreamMode(false)
	conn.SetWriteDelay(false)
	// 这个参数需要好好研究
	conn.SetNoDelay(1, 10, 2, 1)
	conn.SetMtu(1400)
	conn.SetWindowSize(4096, 4096)
	conn.SetACKNoDelay(true)
}
