//go:build linux

package network

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/liangpengcheng/qcontinuum/base"
)

type EpollReactor struct {
	epfd      int
	events    []syscall.EpollEvent
	handlers  map[int]AsyncIOHandler
	mu        sync.RWMutex
	state     int32
	closeOnce sync.Once
}

func NewEpollReactor() (*EpollReactor, error) {
	epfd, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		return nil, err
	}
	return &EpollReactor{
		epfd:     epfd,
		events:   make([]syscall.EpollEvent, 1024),
		handlers: make(map[int]AsyncIOHandler),
	}, nil
}

func (r *EpollReactor) AddFd(fd int, events uint32, handler AsyncIOHandler) error {
	if fd < 0 {
		return errors.New("invalid fd")
	}
	if atomic.LoadInt32(&r.state) == 2 {
		return errors.New("reactor is closed")
	}

	event := syscall.EpollEvent{Events: events, Fd: int32(fd)}
	r.mu.Lock()
	if _, exists := r.handlers[fd]; exists {
		r.mu.Unlock()
		return errors.New("fd already registered")
	}
	r.handlers[fd] = handler
	err := syscall.EpollCtl(r.epfd, syscall.EPOLL_CTL_ADD, fd, &event)
	if err != nil {
		delete(r.handlers, fd)
	}
	r.mu.Unlock()
	return err
}

func (r *EpollReactor) ModifyFd(fd int, events uint32) error {
	r.mu.RLock()
	_, exists := r.handlers[fd]
	r.mu.RUnlock()
	if !exists {
		return errors.New("handler not found")
	}
	event := syscall.EpollEvent{Events: events, Fd: int32(fd)}
	return syscall.EpollCtl(r.epfd, syscall.EPOLL_CTL_MOD, fd, &event)
}

func (r *EpollReactor) RemoveFd(fd int) error {
	r.mu.Lock()
	_, exists := r.handlers[fd]
	delete(r.handlers, fd)
	r.mu.Unlock()
	if !exists {
		return nil
	}
	err := syscall.EpollCtl(r.epfd, syscall.EPOLL_CTL_DEL, fd, nil)
	if err == syscall.ENOENT || err == syscall.EBADF {
		return nil
	}
	return err
}

func (r *EpollReactor) Run() {
	if !atomic.CompareAndSwapInt32(&r.state, 0, 1) {
		return
	}

	for atomic.LoadInt32(&r.state) == 1 {
		n, err := syscall.EpollWait(r.epfd, r.events, 100)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			if atomic.LoadInt32(&r.state) == 2 || err == syscall.EBADF {
				return
			}
			base.Zap().Sugar().Errorf("epoll wait error: %v", err)
			return
		}

		for i := 0; i < n; i++ {
			event := r.events[i]
			fd := int(event.Fd)

			r.mu.RLock()
			handler := r.handlers[fd]
			r.mu.RUnlock()
			if handler == nil {
				continue
			}

			if event.Events&syscall.EPOLLIN != 0 {
				r.drainRead(fd, handler)
			}

			r.mu.RLock()
			handler = r.handlers[fd]
			r.mu.RUnlock()
			if handler == nil {
				continue
			}

			if event.Events&syscall.EPOLLOUT != 0 {
				if err := handler.OnWrite(fd); err != nil {
					handler.OnError(fd, err)
				}
			}
			if event.Events&syscall.EPOLLERR != 0 {
				socketErr := syscall.ECONNRESET
				if value, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ERROR); err == nil && value != 0 {
					socketErr = syscall.Errno(value)
				}
				handler.OnError(fd, socketErr)
			} else if event.Events&(syscall.EPOLLHUP|syscall.EPOLLRDHUP) != 0 {
				handler.OnClose(fd)
			}
		}
	}
}

func (r *EpollReactor) drainRead(fd int, handler AsyncIOHandler) {
	for {
		buffer := getBuffer()
		n, err := syscall.Read(fd, buffer.Data())
		if n > 0 {
			buffer.SetLen(n)
			readErr := handler.OnRead(fd, buffer.Bytes())
			buffer.Release()
			if readErr != nil {
				handler.OnError(fd, readErr)
				return
			}
		} else {
			buffer.Release()
		}

		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				return
			}
			handler.OnError(fd, err)
			return
		}
		if n == 0 {
			handler.OnClose(fd)
			return
		}
	}
}

func (r *EpollReactor) Stop() {
	r.closeOnce.Do(func() {
		atomic.StoreInt32(&r.state, 2)
		syscall.Close(r.epfd)
	})
}

func (r *EpollReactor) Close() error {
	r.Stop()
	return nil
}

func setNonblock(fd int) error {
	if fd < 0 {
		return nil
	}
	return syscall.SetNonblock(fd, true)
}

func setConnForFd(fd int, conn net.Conn) {}
func getConnFromFd(fd int) net.Conn      { return nil }
func removeConnFromFd(fd int)            {}
