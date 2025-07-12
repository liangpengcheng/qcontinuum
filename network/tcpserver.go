package network

import (
	"errors"
	"net"
	"sync/atomic"

	"github.com/liangpengcheng/qcontinuum/base"
)

// AsyncTCPServer 异步TCP服务器
type AsyncTCPServer struct {
	listener     *net.TCPListener
	reactorPool  *IOReactorPool
	processor    *Processor
	fd           int
	running      int32
	acceptCount  uint64
	connCount    uint64
}

// NewAsyncTCP4Server 创建异步TCP服务器
func NewAsyncTCP4Server(bindAddress string) (*AsyncTCPServer, error) {
	serverAddr, err := net.ResolveTCPAddr("tcp4", bindAddress)
	if err != nil {
		return nil, err
	}
	
	listener, err := net.ListenTCP("tcp", serverAddr)
	if err != nil {
		return nil, err
	}
	
	// 获取监听socket的文件描述符
	var fd int
	if file, err := listener.File(); err == nil {
		fd = int(file.Fd())
		file.Close() // 关闭文件句柄，但保留fd
		
		// 设置非阻塞（仅在支持的平台上）
		if err := setNonblock(fd); err != nil {
			listener.Close()
			return nil, err
		}
	} else {
		// 如果无法获取fd，使用-1作为标记
		fd = -1
	}
	
	// 创建reactor池
	reactorPool, err := NewIOReactorPool(0) // 0表示使用CPU核心数
	if err != nil {
		listener.Close()
		return nil, err
	}
	
	server := &AsyncTCPServer{
		listener:    listener,
		reactorPool: reactorPool,
		fd:          fd,
	}
	
	return server, nil
}

// SetProcessor 设置消息处理器
func (s *AsyncTCPServer) SetProcessor(proc *Processor) {
	s.processor = proc
}

// StartAsync 启动异步服务器
func (s *AsyncTCPServer) StartAsync() error {
	if s.processor == nil {
		return errors.New("processor not set")
	}
	
	atomic.StoreInt32(&s.running, 1)
	
	// 获取一个reactor来处理accept事件
	reactor := s.reactorPool.GetReactor()
	
	// 注册accept处理
	return reactor.AddFd(s.fd, EpollIn, s)
}

// Stop 停止服务器
func (s *AsyncTCPServer) Stop() {
	atomic.StoreInt32(&s.running, 0)
	
	if s.listener != nil {
		s.listener.Close()
	}
	
	if s.reactorPool != nil {
		s.reactorPool.Close()
	}
}

// OnRead 实现AsyncIOHandler接口 - 处理accept事件
func (s *AsyncTCPServer) OnRead(fd int, data []byte) error {
	// 对于监听socket，read事件表示有新连接
	for atomic.LoadInt32(&s.running) == 1 {
		// 使用非阻塞accept - 在macOS上需要使用标准库
		conn, err := s.listener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				break // 没有更多连接
			}
			base.Zap().Sugar().Errorf("accept error: %v", err)
			return err
		}
		
		// 设置TCP选项
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetNoDelay(true)
			tcpConn.SetKeepAlive(true)
		}
		
		// 选择一个reactor处理这个连接
		reactor := s.reactorPool.GetReactor()
		
		// 创建异步peer
		peer, err := NewAsyncClientPeer(conn, s.processor, reactor)
		if err != nil {
			conn.Close()
			base.Zap().Sugar().Errorf("create peer error: %v", err)
			continue
		}
		
		// 启动异步I/O
		if err := peer.StartAsyncIO(); err != nil {
			peer.Close()
			base.Zap().Sugar().Errorf("start async IO error: %v", err)
			continue
		}
		
		// 发送连接事件
		event := &Event{
			ID:   AddEvent,
			Peer: &ClientPeer{AsyncClientPeer: peer},
		}
		
		select {
		case s.processor.EventChan <- event:
		default:
			base.Zap().Sugar().Warnf("event queue full, dropping add event")
		}
		
		atomic.AddUint64(&s.acceptCount, 1)
		atomic.AddUint64(&s.connCount, 1)
		
		base.Zap().Sugar().Debugf("accepted connection from %v", conn.RemoteAddr())
	}
	
	return nil
}

// OnWrite 实现AsyncIOHandler接口
func (s *AsyncTCPServer) OnWrite(fd int) error {
	// 监听socket通常不需要处理写事件
	return nil
}

// OnError 实现AsyncIOHandler接口
func (s *AsyncTCPServer) OnError(fd int, err error) {
	base.Zap().Sugar().Errorf("server socket error: %v", err)
}

// OnClose 实现AsyncIOHandler接口
func (s *AsyncTCPServer) OnClose(fd int) {
	base.Zap().Sugar().Infof("server socket closed")
}

// GetStats 获取服务器统计信息
func (s *AsyncTCPServer) GetStats() (acceptCount, connCount uint64) {
	return atomic.LoadUint64(&s.acceptCount), atomic.LoadUint64(&s.connCount)
}

// ============ 兼容性接口 ============

// Server tcp server (兼容旧接口)
type Server struct {
	*AsyncTCPServer
	Listener *net.TCPListener // 保持兼容性
}

// NewTCP4Server 创建TCP服务器 (兼容旧接口)
func NewTCP4Server(bindAddress string) (*Server, error) {
	asyncServer, err := NewAsyncTCP4Server(bindAddress)
	if err != nil {
		return nil, err
	}
	
	return &Server{
		AsyncTCPServer: asyncServer,
		Listener:       asyncServer.listener,
	}, nil
}

// BlockAccept 阻塞接受连接 (兼容旧接口)
func (s *Server) BlockAccept(proc *Processor) {
	s.SetProcessor(proc)
	
	if err := s.StartAsync(); err != nil {
		base.Zap().Sugar().Errorf("start async server error: %v", err)
		return
	}
	
	base.Zap().Sugar().Infof("server started in async mode")
	
	// 保持运行直到停止
	for atomic.LoadInt32(&s.running) == 1 {
		// 在异步模式下，这里只需要等待
		// 实际的连接处理由reactor完成
		select {
		case <-proc.EventChan:
			// 处理事件，但不在这里处理，由processor处理
		default:
			// 短暂休眠避免CPU占用
			//time.Sleep(10 * time.Millisecond)
		}
	}
}

// BlockAcceptOne 接受一个连接 (兼容旧接口)
func (s *Server) BlockAcceptOne(proc *Processor) error {
	if s.AsyncTCPServer == nil {
		return errors.New("async server not initialized")
	}
	
	// 在异步模式下，这个方法的行为有所不同
	// 我们设置processor并启动异步接受，然后等待第一个连接
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

// ReadMessage 读取消息 (兼容旧接口)
func ReadMessage(conn net.Conn) (*MessageHead, []byte, error) {
	// 先读取消息头
	headerBuf := make([]byte, 8)
	_, err := conn.Read(headerBuf)
	if err != nil {
		return nil, nil, err
	}
	
	head, err := ReadHeadFromBuffer(headerBuf)
	if err != nil {
		return nil, nil, err
	}
	
	// 验证消息长度
	if head.ID > 10000000 || head.Length < 0 || head.Length > int32(maxMessageLength) {
		base.Zap().Sugar().Warnf("message error: id(%d),len(%d)", head.ID, head.Length)
		return nil, nil, errors.New("message not in range")
	}
	
	// 读取消息体
	if head.Length == 0 {
		return &head, []byte{}, nil
	}
	
	bodyBuf := make([]byte, head.Length)
	_, err = conn.Read(bodyBuf)
	if err != nil {
		return nil, nil, err
	}
	
	return &head, bodyBuf, nil
}
