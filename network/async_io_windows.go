//go:build windows
// +build windows

package network

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/liangpengcheng/qcontinuum/base"
)

// Windows使用IOCP (I/O Completion Ports)

const (
	// IOCP操作类型
	IOCP_OP_READ  = 1
	IOCP_OP_WRITE = 2
	IOCP_OP_CLOSE = 3
)

// IOCPOverlapped IOCP重叠结构
type IOCPOverlapped struct {
	overlapped syscall.Overlapped
	opType     uint32
	fd         int
	buffer     *Buffer
	handler    AsyncIOHandler
}

// IOCPReactor Windows IOCP反应器
type IOCPReactor struct {
	iocp     syscall.Handle
	handlers map[int]AsyncIOHandler
	mu       sync.RWMutex
	running  int32

	// 操作池，避免频繁分配
	opPool sync.Pool
}

// NewEpollReactor 创建IOCP反应器（在Windows上使用IOCP）
func NewEpollReactor() (*EpollReactor, error) {
	// 创建IOCP句柄
	iocp, err := syscall.CreateIoCompletionPort(syscall.InvalidHandle, 0, 0, 0)
	if err != nil {
		return nil, err
	}

	reactor := &IOCPReactor{
		iocp:     iocp,
		handlers: make(map[int]AsyncIOHandler),
		opPool: sync.Pool{
			New: func() interface{} {
				return &IOCPOverlapped{}
			},
		},
	}

	// 包装为EpollReactor接口
	return &EpollReactor{reactor}, nil
}

// EpollReactor 兼容接口（内部使用IOCP）
type EpollReactor struct {
	*IOCPReactor
}

// AddFd 添加文件描述符到IOCP
func (r *EpollReactor) AddFd(fd int, events uint32, handler AsyncIOHandler) error {
	r.mu.Lock()
	r.handlers[fd] = handler
	r.mu.Unlock()

	// 获取socket句柄
	conn := getConnFromFd(fd)
	if conn == nil {
		return errors.New("connection not found for fd")
	}

	// 获取socket句柄
	socketHandle, err := getSocketHandle(conn)
	if err != nil {
		return err
	}

	// 将socket关联到IOCP
	_, err = syscall.CreateIoCompletionPort(socketHandle, r.iocp, uint32(fd), 0)
	if err != nil {
		return err
	}

	// 如果需要读事件，立即投递一个读操作
	if events&EpollIn != 0 {
		r.postRead(fd, handler)
	}

	return nil
}

// ModifyFd 修改文件描述符事件
func (r *EpollReactor) ModifyFd(fd int, events uint32) error {
	// IOCP不需要修改事件，通过重新投递操作来实现
	r.mu.RLock()
	handler, exists := r.handlers[fd]
	r.mu.RUnlock()

	if !exists {
		return errors.New("handler not found")
	}

	if events&EpollIn != 0 {
		r.postRead(fd, handler)
	}

	return nil
}

// RemoveFd 从IOCP移除文件描述符
func (r *EpollReactor) RemoveFd(fd int) error {
	r.mu.Lock()
	delete(r.handlers, fd)
	r.mu.Unlock()

	// IOCP会自动处理socket关闭时的清理
	return nil
}

// postRead 投递异步读操作
func (r *IOCPReactor) postRead(fd int, handler AsyncIOHandler) {
	conn := getConnFromFd(fd)
	if conn == nil {
		return
	}

	socketHandle, err := getSocketHandle(conn)
	if err != nil {
		handler.OnError(fd, err)
		return
	}

	// 从池中获取操作结构
	op := r.opPool.Get().(*IOCPOverlapped)
	op.opType = IOCP_OP_READ
	op.fd = fd
	op.handler = handler
	op.buffer = GetBuffer()

	// 扩展缓冲区
	if op.buffer.Cap() < 32*1024 {
		op.buffer.Grow(32 * 1024)
	}

	// 投递WSARecv操作
	var bytesReceived uint32
	var flags uint32
	wsaBuf := syscall.WSABuf{
		Len: uint32(op.buffer.Cap()),
		Buf: (*byte)(unsafe.Pointer(&op.buffer.Data()[0])),
	}

	err = syscall.WSARecv(socketHandle, &wsaBuf, 1, &bytesReceived, &flags, &op.overlapped, nil)
	if err != nil && err != syscall.ERROR_IO_PENDING {
		// 立即失败
		op.buffer.Release()
		r.opPool.Put(op)
		handler.OnError(fd, err)
	}
}

// postWrite 投递异步写操作
func (r *IOCPReactor) postWrite(fd int, handler AsyncIOHandler, data []byte) {
	conn := getConnFromFd(fd)
	if conn == nil {
		return
	}

	socketHandle, err := getSocketHandle(conn)
	if err != nil {
		handler.OnError(fd, err)
		return
	}

	// 从池中获取操作结构
	op := r.opPool.Get().(*IOCPOverlapped)
	op.opType = IOCP_OP_WRITE
	op.fd = fd
	op.handler = handler
	op.buffer = GetBuffer()

	// 复制数据到缓冲区
	if op.buffer.Cap() < len(data) {
		op.buffer.Grow(len(data))
	}
	copy(op.buffer.Data(), data)
	op.buffer.SetLen(len(data))

	// 投递WSASend操作
	var bytesSent uint32
	wsaBuf := syscall.WSABuf{
		Len: uint32(op.buffer.Len()),
		Buf: (*byte)(unsafe.Pointer(&op.buffer.Data()[0])),
	}

	err = syscall.WSASend(socketHandle, &wsaBuf, 1, &bytesSent, 0, &op.overlapped, nil)
	if err != nil && err != syscall.ERROR_IO_PENDING {
		// 立即失败
		op.buffer.Release()
		r.opPool.Put(op)
		handler.OnError(fd, err)
	}
}

// Run 运行IOCP事件循环
func (r *EpollReactor) Run() {
	atomic.StoreInt32(&r.running, 1)
	defer atomic.StoreInt32(&r.running, 0)

	for atomic.LoadInt32(&r.running) == 1 {
		var bytesTransferred uint32
		var completionKey uint32
		var overlapped *syscall.Overlapped

		// 等待I/O完成事件
		err := syscall.GetQueuedCompletionStatus(r.iocp, &bytesTransferred, &completionKey, &overlapped, 100)

		if err != nil {
			// 检查是否是超时错误
			if errno, ok := err.(syscall.Errno); ok && errno == 258 { // WAIT_TIMEOUT
				continue // 超时，继续循环
			}
			base.Zap().Sugar().Errorf("IOCP error: %v", err)
			continue
		}

		if overlapped == nil {
			continue
		}

		// 从overlapped结构恢复操作信息
		op := (*IOCPOverlapped)(unsafe.Pointer(overlapped))
		fd := int(completionKey)

		// 处理完成的操作
		r.handleCompletion(op, fd, bytesTransferred, err)
	}
}

// handleCompletion 处理I/O完成事件
func (r *IOCPReactor) handleCompletion(op *IOCPOverlapped, fd int, bytesTransferred uint32, err error) {
	defer func() {
		if op.buffer != nil {
			op.buffer.Release()
		}
		r.opPool.Put(op)
	}()

	handler := op.handler
	if handler == nil {
		return
	}

	switch op.opType {
	case IOCP_OP_READ:
		if err != nil {
			handler.OnError(fd, err)
			return
		}

		if bytesTransferred == 0 {
			handler.OnClose(fd)
			return
		}

		// 设置缓冲区长度并调用处理器
		op.buffer.SetLen(int(bytesTransferred))
		if err := handler.OnRead(fd, op.buffer.Data()[:bytesTransferred]); err != nil {
			handler.OnError(fd, err)
			return
		}

		// 继续投递读操作
		r.postRead(fd, handler)

	case IOCP_OP_WRITE:
		if err != nil {
			handler.OnError(fd, err)
			return
		}

		// 写完成，通知处理器
		handler.OnWrite(fd)

	case IOCP_OP_CLOSE:
		handler.OnClose(fd)
	}
}

// Stop 停止IOCP事件循环
func (r *EpollReactor) Stop() {
	atomic.StoreInt32(&r.running, 0)

	// 向IOCP投递一个退出信号
	syscall.PostQueuedCompletionStatus(r.iocp, 0, 0, nil)
}

// Close 关闭IOCP reactor
func (r *EpollReactor) Close() error {
	r.Stop()
	if r.iocp != syscall.InvalidHandle {
		return syscall.CloseHandle(r.iocp)
	}
	return nil
}

// getSocketHandle 从net.Conn获取socket句柄
func getSocketHandle(conn net.Conn) (syscall.Handle, error) {
	// 尝试获取底层的socket句柄
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		file, err := tcpConn.File()
		if err != nil {
			return syscall.InvalidHandle, err
		}
		defer file.Close()
		return syscall.Handle(file.Fd()), nil
	}

	return syscall.InvalidHandle, errors.New("unsupported connection type")
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

// setNonblock Windows IOCP不需要设置非阻塞
func setNonblock(fd int) error {
	// IOCP本身就是异步的，不需要设置非阻塞
	return nil
}
