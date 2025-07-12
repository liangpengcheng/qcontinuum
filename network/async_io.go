//go:build linux
// +build linux

package network

import (
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/liangpengcheng/qcontinuum/base"
)

// EpollReactor epoll反应器
type EpollReactor struct {
	epfd     int
	events   []syscall.EpollEvent
	handlers map[int]AsyncIOHandler
	mu       sync.RWMutex
	running  int32
}

// NewEpollReactor 创建epoll反应器
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

// AddFd 添加文件描述符到epoll
func (r *EpollReactor) AddFd(fd int, events uint32, handler AsyncIOHandler) error {
	r.mu.Lock()
	r.handlers[fd] = handler
	r.mu.Unlock()

	event := syscall.EpollEvent{
		Events: events,
		Fd:     int32(fd),
	}
	return syscall.EpollCtl(r.epfd, syscall.EPOLL_CTL_ADD, fd, &event)
}

// ModifyFd 修改文件描述符事件
func (r *EpollReactor) ModifyFd(fd int, events uint32) error {
	event := syscall.EpollEvent{
		Events: events,
		Fd:     int32(fd),
	}
	return syscall.EpollCtl(r.epfd, syscall.EPOLL_CTL_MOD, fd, &event)
}

// RemoveFd 从epoll移除文件描述符
func (r *EpollReactor) RemoveFd(fd int) error {
	r.mu.Lock()
	delete(r.handlers, fd)
	r.mu.Unlock()

	return syscall.EpollCtl(r.epfd, syscall.EPOLL_CTL_DEL, fd, nil)
}

// Run 运行事件循环
func (r *EpollReactor) Run() {
	atomic.StoreInt32(&r.running, 1)
	defer atomic.StoreInt32(&r.running, 0)

	for atomic.LoadInt32(&r.running) == 1 {
		n, err := syscall.EpollWait(r.epfd, r.events, 100)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			base.Zap().Sugar().Errorf("epoll wait error: %v", err)
			break
		}

		for i := 0; i < n; i++ {
			event := r.events[i]
			fd := int(event.Fd)

			r.mu.RLock()
			handler, exists := r.handlers[fd]
			r.mu.RUnlock()

			if !exists {
				continue
			}

			if event.Events&syscall.EPOLLIN != 0 {
				// 读事件 - 使用零拷贝读取
				buffer := getBuffer()
				n, err := syscall.Read(fd, buffer.data[:buffer.cap])
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
			}

			if event.Events&syscall.EPOLLOUT != 0 {
				handler.OnWrite(fd)
			}

			if event.Events&(syscall.EPOLLERR|syscall.EPOLLHUP) != 0 {
				handler.OnError(fd, syscall.ECONNRESET)
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
	return syscall.Close(r.epfd)
}
