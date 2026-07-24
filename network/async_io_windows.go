//go:build windows

package network

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Windows uses Go”s net runtime, which is already backed by IOCP. Re-associating
// a net.TCPConn with a second completion port is invalid, so this adapter turns
// blocking net.Conn reads into reactor callbacks with one managed goroutine per
// connection.
type windowsRegistration struct {
	conn    net.Conn
	handler AsyncIOHandler
	stop    chan struct{}
	once    sync.Once
}

func (r *windowsRegistration) close() {
	r.once.Do(func() { close(r.stop) })
}

func (r *windowsRegistration) stopped() bool {
	select {
	case <-r.stop:
		return true
	default:
		return false
	}
}

type EpollReactor struct {
	handlers  map[int]*windowsRegistration
	mu        sync.RWMutex
	state     int32
	stop      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// Compatibility aliases for code that named the former Windows implementation.
// I/O completion is now owned by Go's runtime IOCP poller.
type IOCPReactor = EpollReactor

type IOCPOverlapped struct{}

func NewEpollReactor() (*EpollReactor, error) {
	return &EpollReactor{
		handlers: make(map[int]*windowsRegistration),
		stop:     make(chan struct{}),
	}, nil
}

func (r *EpollReactor) AddFd(fd int, events uint32, handler AsyncIOHandler) error {
	if atomic.LoadInt32(&r.state) == 2 {
		return errors.New("reactor is closed")
	}
	conn := getConnFromFd(fd)
	if conn == nil {
		return errors.New("connection not found for fd")
	}

	reg := &windowsRegistration{
		conn:    conn,
		handler: handler,
		stop:    make(chan struct{}),
	}

	r.mu.Lock()
	if _, exists := r.handlers[fd]; exists {
		r.mu.Unlock()
		return errors.New("fd already registered")
	}
	r.handlers[fd] = reg
	r.mu.Unlock()

	if events&EpollIn != 0 {
		r.wg.Add(1)
		go r.readLoop(fd, reg)
	}
	return nil
}

func (r *EpollReactor) ModifyFd(fd int, events uint32) error {
	r.mu.RLock()
	_, exists := r.handlers[fd]
	r.mu.RUnlock()
	if !exists {
		return errors.New("handler not found")
	}
	// Writes are performed by the peer writer goroutine. Go''s net package
	// handles writable readiness through the runtime IOCP poller.
	return nil
}

func (r *EpollReactor) RemoveFd(fd int) error {
	r.mu.Lock()
	reg := r.handlers[fd]
	delete(r.handlers, fd)
	r.mu.Unlock()
	if reg != nil {
		reg.close()
	}
	return nil
}

func (r *EpollReactor) readLoop(fd int, reg *windowsRegistration) {
	defer r.wg.Done()

	for !reg.stopped() && atomic.LoadInt32(&r.state) != 2 {
		buffer := getBuffer()
		n, err := reg.conn.Read(buffer.Data())
		if n > 0 {
			buffer.SetLen(n)
			if readErr := reg.handler.OnRead(fd, buffer.Bytes()); readErr != nil {
				buffer.Release()
				if !reg.stopped() {
					reg.handler.OnError(fd, readErr)
				}
				return
			}
		}
		buffer.Release()

		if err != nil {
			if !reg.stopped() {
				reg.handler.OnError(fd, err)
			}
			return
		}
		if n == 0 {
			if !reg.stopped() {
				reg.handler.OnClose(fd)
			}
			return
		}
	}
}

func (r *EpollReactor) Run() {
	if !atomic.CompareAndSwapInt32(&r.state, 0, 1) {
		return
	}
	<-r.stop
}

func (r *EpollReactor) Stop() {
	r.closeOnce.Do(func() {
		atomic.StoreInt32(&r.state, 2)
		close(r.stop)

		r.mu.RLock()
		regs := make([]*windowsRegistration, 0, len(r.handlers))
		for _, reg := range r.handlers {
			regs = append(regs, reg)
		}
		r.mu.RUnlock()
		for _, reg := range regs {
			reg.close()
			_ = reg.conn.SetReadDeadline(time.Now())
		}
	})
}

func (r *EpollReactor) Close() error {
	r.Stop()
	r.wg.Wait()
	return nil
}

var connMap sync.Map

func setConnForFd(fd int, conn net.Conn) {
	connMap.Store(fd, conn)
}

func getConnFromFd(fd int) net.Conn {
	if value, ok := connMap.Load(fd); ok {
		return value.(net.Conn)
	}
	return nil
}

func removeConnFromFd(fd int) {
	connMap.Delete(fd)
}

func setNonblock(fd int) error {
	return nil
}
