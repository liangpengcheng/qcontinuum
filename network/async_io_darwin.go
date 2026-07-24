//go:build darwin

package network

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/liangpengcheng/qcontinuum/base"
)

type KqueueReactor struct {
	kq        int
	events    []syscall.Kevent_t
	handlers  map[int]AsyncIOHandler
	interests map[int]uint32
	mu        sync.RWMutex
	state     int32
	closeOnce sync.Once
}

type EpollReactor struct {
	*KqueueReactor
}

func NewEpollReactor() (*EpollReactor, error) {
	kq, err := syscall.Kqueue()
	if err != nil {
		return nil, err
	}
	return &EpollReactor{&KqueueReactor{
		kq:        kq,
		events:    make([]syscall.Kevent_t, 1024),
		handlers:  make(map[int]AsyncIOHandler),
		interests: make(map[int]uint32),
	}}, nil
}

func kqueueChange(fd int, filter int16, flags uint16) syscall.Kevent_t {
	return syscall.Kevent_t{
		Ident:  uint64(fd),
		Filter: filter,
		Flags:  flags,
	}
}

func changesFor(fd int, oldEvents, newEvents uint32) []syscall.Kevent_t {
	changes := make([]syscall.Kevent_t, 0, 2)
	if oldEvents&EpollIn != newEvents&EpollIn {
		flags := uint16(syscall.EV_DELETE)
		if newEvents&EpollIn != 0 {
			flags = syscall.EV_ADD | syscall.EV_ENABLE | syscall.EV_CLEAR
		}
		changes = append(changes, kqueueChange(fd, syscall.EVFILT_READ, flags))
	}
	if oldEvents&EpollOut != newEvents&EpollOut {
		flags := uint16(syscall.EV_DELETE)
		if newEvents&EpollOut != 0 {
			flags = syscall.EV_ADD | syscall.EV_ENABLE | syscall.EV_CLEAR
		}
		changes = append(changes, kqueueChange(fd, syscall.EVFILT_WRITE, flags))
	}
	return changes
}

func (r *EpollReactor) AddFd(fd int, events uint32, handler AsyncIOHandler) error {
	if fd < 0 {
		return errors.New("invalid fd")
	}
	if atomic.LoadInt32(&r.state) == 2 {
		return errors.New("reactor is closed")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[fd]; exists {
		return errors.New("fd already registered")
	}
	if _, err := syscall.Kevent(r.kq, changesFor(fd, 0, events), nil, nil); err != nil {
		return err
	}
	r.handlers[fd] = handler
	r.interests[fd] = events
	return nil
}

func (r *EpollReactor) ModifyFd(fd int, events uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[fd]; !exists {
		return errors.New("handler not found")
	}
	oldEvents := r.interests[fd]
	if oldEvents == events {
		return nil
	}
	if _, err := syscall.Kevent(r.kq, changesFor(fd, oldEvents, events), nil, nil); err != nil {
		return err
	}
	r.interests[fd] = events
	return nil
}

func (r *EpollReactor) RemoveFd(fd int) error {
	r.mu.Lock()
	oldEvents, exists := r.interests[fd]
	delete(r.handlers, fd)
	delete(r.interests, fd)
	r.mu.Unlock()
	if !exists {
		return nil
	}
	_, err := syscall.Kevent(r.kq, changesFor(fd, oldEvents, 0), nil, nil)
	if err == syscall.ENOENT || err == syscall.EBADF {
		return nil
	}
	return err
}

func (r *EpollReactor) Run() {
	if !atomic.CompareAndSwapInt32(&r.state, 0, 1) {
		return
	}
	timeout := &syscall.Timespec{Sec: 0, Nsec: 100000000}

	for atomic.LoadInt32(&r.state) == 1 {
		n, err := syscall.Kevent(r.kq, nil, r.events, timeout)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			if atomic.LoadInt32(&r.state) == 2 || err == syscall.EBADF {
				return
			}
			base.Zap().Sugar().Errorf("kevent error: %v", err)
			return
		}

		for i := 0; i < n; i++ {
			event := r.events[i]
			fd := int(event.Ident)
			r.mu.RLock()
			handler := r.handlers[fd]
			r.mu.RUnlock()
			if handler == nil {
				continue
			}

			if event.Filter == syscall.EVFILT_READ {
				r.drainRead(fd, handler)
			} else if event.Filter == syscall.EVFILT_WRITE {
				if err := handler.OnWrite(fd); err != nil {
					handler.OnError(fd, err)
				}
			}
			if event.Flags&syscall.EV_ERROR != 0 {
				handler.OnError(fd, errors.New("kqueue event error"))
			} else if event.Flags&syscall.EV_EOF != 0 {
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
		syscall.Close(r.kq)
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
