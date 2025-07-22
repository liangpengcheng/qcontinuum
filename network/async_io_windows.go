//go:build windows
// +build windows

package network

import (
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Windows使用select模拟epoll

// SelectReactor Windows select反应器
type SelectReactor struct {
	handlers map[int]AsyncIOHandler
	mu       sync.RWMutex
	running  int32
	stopCh   chan struct{}
}

// NewEpollReactor 创建select反应器（在Windows上使用select）
func NewEpollReactor() (*EpollReactor, error) {
	reactor := &SelectReactor{
		handlers: make(map[int]AsyncIOHandler),
		stopCh:   make(chan struct{}),
	}

	// 包装为EpollReactor接口
	return &EpollReactor{reactor}, nil
}

// EpollReactor 兼容接口（内部使用select）
type EpollReactor struct {
	*SelectReactor
}

// AddFd 添加文件描述符到select
func (r *EpollReactor) AddFd(fd int, events uint32, handler AsyncIOHandler) error {
	r.mu.Lock()
	r.handlers[fd] = handler
	r.mu.Unlock()
	return nil
}

// ModifyFd 修改文件描述符事件
func (r *EpollReactor) ModifyFd(fd int, events uint32) error {
	// Windows select不需要修改事件
	return nil
}

// RemoveFd 从select移除文件描述符
func (r *EpollReactor) RemoveFd(fd int) error {
	r.mu.Lock()
	delete(r.handlers, fd)
	r.mu.Unlock()
	return nil
}

// Run 运行事件循环
func (r *EpollReactor) Run() {
	atomic.StoreInt32(&r.running, 1)
	defer atomic.StoreInt32(&r.running, 0)

	for atomic.LoadInt32(&r.running) == 1 {
		select {
		case <-r.stopCh:
			return
		default:
			// Windows上的简化处理：定期检查连接状态
			r.mu.RLock()
			handlers := make(map[int]AsyncIOHandler)
			for fd, handler := range r.handlers {
				handlers[fd] = handler
			}
			r.mu.RUnlock()

			for fd, handler := range handlers {
				// 尝试读取数据
				conn := getConnFromFd(fd)
				if conn != nil {
					buffer := GetBuffer()

					// 设置短超时以避免阻塞
					conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
					n, err := conn.Read(buffer.Data())
					conn.SetReadDeadline(time.Time{}) // 清除超时

					if err != nil {
						if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
							// 超时是正常的，继续
							buffer.Release()
							continue
						}
						handler.OnError(fd, err)
						buffer.Release()
						continue
					}

					if n == 0 {
						handler.OnClose(fd)
						buffer.Release()
						continue
					}

					buffer.SetLen(n)
					handler.OnRead(fd, buffer.Data()[:n])
					buffer.Release()
				}
			}

			// 短暂休眠避免CPU占用过高
			time.Sleep(1 * time.Millisecond)
		}
	}
}

// Stop 停止事件循环
func (r *EpollReactor) Stop() {
	atomic.StoreInt32(&r.running, 0)
	select {
	case r.stopCh <- struct{}{}:
	default:
	}
}

// Close 关闭reactor
func (r *EpollReactor) Close() error {
	r.Stop()
	return nil
}

// Windows连接映射
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

// setNonblock Windows上的非阻塞设置
func setNonblock(fd int) error {
	if fd == -1 {
		return nil // 虚拟fd，跳过
	}

	// Windows上设置非阻塞需要特殊处理
	// 这里我们简化处理，依赖于连接映射
	return nil
}
