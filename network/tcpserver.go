package network

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/liangpengcheng/qcontinuum/base"
)

type AsyncTCPServer struct {
	listener    *net.TCPListener
	reactorPool *IOReactorPool
	processor   *Processor

	running     int32
	started     int32
	acceptCount uint64
	connCount   uint64

	peers            sync.Map
	wg               sync.WaitGroup
	shutdown         chan struct{}
	stopOnce         sync.Once
	reactorCloseOnce sync.Once
}

func NewAsyncTCP4Server(bindAddress string) (*AsyncTCPServer, error) {
	serverAddr, err := net.ResolveTCPAddr("tcp4", bindAddress)
	if err != nil {
		return nil, err
	}

	listener, err := net.ListenTCP("tcp4", serverAddr)
	if err != nil {
		return nil, err
	}

	reactorPool, err := NewIOReactorPool(0)
	if err != nil {
		listener.Close()
		return nil, err
	}

	return &AsyncTCPServer{
		listener:    listener,
		reactorPool: reactorPool,
		shutdown:    make(chan struct{}),
	}, nil
}

func (s *AsyncTCPServer) SetProcessor(proc *Processor) {
	s.processor = proc
}

func (s *AsyncTCPServer) StartAsync() error {
	if s.processor == nil {
		return errors.New("processor not set")
	}
	if !atomic.CompareAndSwapInt32(&s.started, 0, 1) {
		if atomic.LoadInt32(&s.running) == 1 {
			return errors.New("server already started")
		}
		return errors.New("server cannot be restarted")
	}

	atomic.StoreInt32(&s.running, 1)
	go s.acceptLoop()
	return nil
}

func (s *AsyncTCPServer) acceptLoop() {
	for atomic.LoadInt32(&s.running) == 1 {
		conn, err := s.listener.AcceptTCP()
		if err != nil {
			if atomic.LoadInt32(&s.running) == 0 {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			base.Zap().Sugar().Errorf("accept error: %v", err)
			s.initiateStop()
			return
		}

		if err := conn.SetNoDelay(true); err != nil {
			base.Zap().Sugar().Warnf("set TCP_NODELAY failed: %v", err)
		}
		if err := conn.SetKeepAlive(true); err != nil {
			base.Zap().Sugar().Warnf("set keepalive failed: %v", err)
		}

		atomic.AddUint64(&s.acceptCount, 1)
		reactor := s.reactorPool.GetReactor()
		peer, err := NewAsyncClientPeer(conn, s.processor, reactor)
		if err != nil {
			conn.Close()
			base.Zap().Sugar().Errorf("create peer error: %v", err)
			continue
		}

		peer.onClosed = func() {
			s.peers.Delete(peer)
			atomic.AddUint64(&s.connCount, ^uint64(0))
			s.wg.Done()
		}

		s.wg.Add(1)
		s.peers.Store(peer, struct{}{})
		atomic.AddUint64(&s.connCount, 1)

		event := &Event{ID: AddEvent, Peer: &ClientPeer{AsyncClientPeer: peer}}
		select {
		case s.processor.EventChan <- event:
		case <-s.shutdown:
			peer.Close()
			return
		}

		if err := peer.StartAsyncIO(); err != nil {
			peer.Close()
			base.Zap().Sugar().Errorf("start async IO error: %v", err)
			continue
		}
	}
}

func (s *AsyncTCPServer) initiateStop() {
	s.stopOnce.Do(func() {
		atomic.StoreInt32(&s.running, 0)
		close(s.shutdown)
		if s.listener != nil {
			s.listener.Close()
		}
	})
}

func (s *AsyncTCPServer) closeAllPeers() {
	s.peers.Range(func(key, _ interface{}) bool {
		key.(*AsyncClientPeer).Close()
		return true
	})
}

func (s *AsyncTCPServer) closeReactors() {
	s.reactorCloseOnce.Do(func() {
		if s.reactorPool != nil {
			s.reactorPool.Close()
		}
	})
}

func (s *AsyncTCPServer) Stop() {
	s.initiateStop()
	s.closeAllPeers()
	s.wg.Wait()
	s.closeReactors()
}

func (s *AsyncTCPServer) GracefulStop(timeout time.Duration) error {
	s.initiateStop()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.closeReactors()
		return nil
	case <-time.After(timeout):
		s.closeAllPeers()
		s.wg.Wait()
		s.closeReactors()
		return fmt.Errorf("graceful shutdown timed out after %v", timeout)
	}
}

func (s *AsyncTCPServer) IsShutdown() bool {
	select {
	case <-s.shutdown:
		return true
	default:
		return false
	}
}

// AsyncIOHandler compatibility methods. Listener acceptance is intentionally
// handled by the dedicated blocking acceptLoop, not by a connection reactor.
func (s *AsyncTCPServer) OnRead(fd int, data []byte) error { return nil }
func (s *AsyncTCPServer) OnWrite(fd int) error             { return nil }
func (s *AsyncTCPServer) OnError(fd int, err error) {
	base.Zap().Sugar().Errorf("server socket error: %v", err)
}
func (s *AsyncTCPServer) OnClose(fd int) {}

func (s *AsyncTCPServer) GetStats() (acceptCount, connCount uint64) {
	return atomic.LoadUint64(&s.acceptCount), atomic.LoadUint64(&s.connCount)
}

type Server struct {
	*AsyncTCPServer
	Listener *net.TCPListener
}

func NewTCP4Server(bindAddress string) (*Server, error) {
	asyncServer, err := NewAsyncTCP4Server(bindAddress)
	if err != nil {
		return nil, err
	}
	return &Server{AsyncTCPServer: asyncServer, Listener: asyncServer.listener}, nil
}

func (s *Server) BlockAccept(proc *Processor) {
	if s == nil || s.AsyncTCPServer == nil {
		base.Zap().Sugar().Error("async server not initialized")
		return
	}
	s.SetProcessor(proc)
	if err := s.StartAsync(); err != nil {
		base.Zap().Sugar().Errorf("start async server error: %v", err)
		return
	}
	<-s.shutdown
}

func (s *Server) BlockAcceptOne(proc *Processor) error {
	if s == nil || s.AsyncTCPServer == nil {
		return errors.New("async server not initialized")
	}
	s.SetProcessor(proc)
	if atomic.LoadInt32(&s.started) == 0 {
		if err := s.StartAsync(); err != nil {
			return err
		}
	}

	initialCount := atomic.LoadUint64(&s.acceptCount)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for atomic.LoadUint64(&s.acceptCount) == initialCount {
		select {
		case <-s.shutdown:
			return errors.New("server stopped")
		case <-ticker.C:
		}
	}
	return nil
}

func ReadMessage(conn net.Conn) (*MessageHead, []byte, error) {
	headerBuf := make([]byte, 8)
	if _, err := io.ReadFull(conn, headerBuf); err != nil {
		return nil, nil, err
	}

	head, err := ReadHeadFromBuffer(headerBuf)
	if err != nil {
		return nil, nil, err
	}
	if head.ID > 10000000 || head.Length < 0 || head.Length > int32(maxMessageLength) {
		return nil, nil, fmt.Errorf("message not in range: id=%d len=%d", head.ID, head.Length)
	}

	bodyBuf := make([]byte, int(head.Length))
	if len(bodyBuf) > 0 {
		if _, err := io.ReadFull(conn, bodyBuf); err != nil {
			return nil, nil, err
		}
	}
	return &head, bodyBuf, nil
}
