//go:build darwin
// +build darwin

package network

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/liangpengcheng/qcontinuum/base"
)

// macOS使用kqueue实现

// KqueueReactor kqueue反应器
type KqueueReactor struct {
	kq       int
	events   []syscall.Kevent_t
	handlers map[int]AsyncIOHandler
	mu       sync.RWMutex
	running  int32
}

// NewEpollReactor 创建kqueue反应器（在macOS上使用kqueue）
func NewEpollReactor() (*EpollReactor, error) {
	kq, err := syscall.Kqueue()
	if err != nil {
		return nil, err
	}

	reactor := &KqueueReactor{
		kq:       kq,
		events:   make([]syscall.Kevent_t, 1024),
		handlers: make(map[int]AsyncIOHandler),
	}

	// 包装为EpollReactor接口
	return &EpollReactor{reactor}, nil
}

// EpollReactor 兼容接口（内部使用kqueue）
type EpollReactor struct {
	*KqueueReactor
}

// AddFd 添加文件描述符到kqueue
func (r *EpollReactor) AddFd(fd int, events uint32, handler AsyncIOHandler) error {
	r.mu.Lock()
	r.handlers[fd] = handler
	r.mu.Unlock()

	// kqueue使用不同的事件类型
	var kevents []syscall.Kevent_t

	if events&EpollIn != 0 {
		kev := syscall.Kevent_t{
			Ident:  uint64(fd),
			Filter: syscall.EVFILT_READ,
			Flags:  syscall.EV_ADD | syscall.EV_ENABLE,
		}
		kevents = append(kevents, kev)
	}

	if events&EpollOut != 0 {
		kev := syscall.Kevent_t{
			Ident:  uint64(fd),
			Filter: syscall.EVFILT_WRITE,
			Flags:  syscall.EV_ADD | syscall.EV_ENABLE,
		}
		kevents = append(kevents, kev)
	}

	_, err := syscall.Kevent(r.kq, kevents, nil, nil)
	return err
}

// ModifyFd 修改文件描述符事件
func (r *EpollReactor) ModifyFd(fd int, events uint32) error {
	// kqueue需要先删除再添加
	r.RemoveFd(fd)
	r.mu.RLock()
	handler := r.handlers[fd]
	r.mu.RUnlock()

	if handler != nil {
		return r.AddFd(fd, events, handler)
	}
	return nil
}

// RemoveFd 从kqueue移除文件描述符
func (r *EpollReactor) RemoveFd(fd int) error {
	r.mu.Lock()
	delete(r.handlers, fd)
	r.mu.Unlock()

	kevents := []syscall.Kevent_t{
		{
			Ident:  uint64(fd),
			Filter: syscall.EVFILT_READ,
			Flags:  syscall.EV_DELETE,
		},
		{
			Ident:  uint64(fd),
			Filter: syscall.EVFILT_WRITE,
			Flags:  syscall.EV_DELETE,
		},
	}

	syscall.Kevent(r.kq, kevents, nil, nil) // 忽略错误
	return nil
}

// Run 运行事件循环
func (r *EpollReactor) Run() {
	atomic.StoreInt32(&r.running, 1)
	defer atomic.StoreInt32(&r.running, 0)

	timeout := &syscall.Timespec{Sec: 0, Nsec: 100000000} // 100ms

	for atomic.LoadInt32(&r.running) == 1 {
		n, err := syscall.Kevent(r.kq, nil, r.events, timeout)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			base.Zap().Sugar().Errorf("kevent error: %v", err)
			break
		}

		for i := 0; i < n; i++ {
			event := r.events[i]
			fd := int(event.Ident)

			r.mu.RLock()
			handler, exists := r.handlers[fd]
			r.mu.RUnlock()

			if !exists {
				continue
			}

			if event.Filter == syscall.EVFILT_READ {
				// 读事件 - 使用简化的读取方式
				buffer := getBuffer()

				// 在macOS上，直接读取数据
				conn := getConnFromFd(fd)
				if conn != nil {
					n, err := conn.Read(buffer.data[:buffer.cap])
					if err != nil {
						if err != syscall.EAGAIN {
							handler.OnError(fd, err)
						}
						putBuffer(buffer)
						continue
					}
					if n == 0 {
						handler.OnClose(fd)
						putBuffer(buffer)
						continue
					}
					buffer.len = n
					handler.OnRead(fd, buffer.data[:n])
					// 处理完后释放buffer
					putBuffer(buffer)
				} else {
					putBuffer(buffer)
				}
			}

			if event.Filter == syscall.EVFILT_WRITE {
				handler.OnWrite(fd)
			}

			if event.Flags&syscall.EV_ERROR != 0 {
				handler.OnError(fd, errors.New("kqueue error"))
			}
		}
	}
}

// Stop 停止事件循环
func (r *EpollReactor) Stop() {
	atomic.StoreInt32(&r.running, 0)
}

// Close 关闭reactor
func (r *EpollReactor) Close() error {
	r.Stop()
	return syscall.Close(r.kq)
}

// 简化的连接管理（用于macOS兼容）
var connMap sync.Map

func setConnForFd(fd int, conn net.Conn) {
	connMap.Store(fd, conn)
}

func getConnFromFd(fd int) net.Conn {
	if v, ok := connMap.Load(fd); ok {
		return v.(net.Conn)
	}
	return nil
}

func removeConnFromFd(fd int) {
	connMap.Delete(fd)
}

// setNonblock 设置文件描述符为非阻塞
func setNonblock(fd int) error {
	if fd == -1 {
		return nil // 虚拟fd，跳过
	}
	return syscall.SetNonblock(fd, true)
}
