package network

import (
	"errors"
	"net"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/golang/protobuf/proto"
	"github.com/liangpengcheng/qcontinuum/base"
)

// PeerState peer状态
type PeerState int32

const (
	PeerStateConnected PeerState = iota
	PeerStateClosing
	PeerStateClosed
)

// AsyncClientPeer 异步客户端peer
type AsyncClientPeer struct {
	Connection   net.Conn
	fd           int
	Flag         int32
	ID           int64
	state        int32          // PeerState
	redirectProc unsafe.Pointer // *Processor, 使用unsafe.Pointer实现无锁
	Proc         *Processor

	// 异步I/O组件
	reader  *AsyncMessageReader
	writer  *ZeroCopyMessageWriter
	reactor *EpollReactor

	// 统计信息
	bytesRead    uint64
	bytesWritten uint64
	lastActive   int64 // unix timestamp
}

// NewWebSocketClientPeer 创建WebSocket客户端peer（无文件描述符）
func NewWebSocketClientPeer(conn net.Conn, proc *Processor) *ClientPeer {
	peer := &AsyncClientPeer{
		Connection: conn,
		fd:         -1, // WebSocket使用虚拟fd
		Proc:       proc,
		ID:         0,
		state:      int32(PeerStateConnected),
		lastActive: time.Now().Unix(),
		reader:     NewAsyncMessageReader(),
		writer:     NewZeroCopyMessageWriter(),
		reactor:    nil, // WebSocket不使用reactor
	}

	return &ClientPeer{AsyncClientPeer: peer}
}

// NewAsyncClientPeer 创建异步客户端peer
func NewAsyncClientPeer(conn net.Conn, proc *Processor, reactor *EpollReactor) (*AsyncClientPeer, error) {
	// 获取文件描述符
	var fd int

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		file, fileErr := tcpConn.File()
		if fileErr != nil {
			return nil, fileErr
		}
		fd = int(file.Fd())
		file.Close() // 关闭文件句柄，但保留fd
	} else {
		// 对于非TCP连接，使用虚拟fd
		fd = -1
	}

	// 设置非阻塞（仅对真实的TCP连接）
	if fd != -1 {
		if err := setNonblock(fd); err != nil {
			return nil, err
		}
		// 在macOS上注册连接映射
		setConnForFd(fd, conn)
	}

	peer := &AsyncClientPeer{
		Connection: conn,
		fd:         fd,
		Proc:       proc,
		reader:     NewAsyncMessageReader(),
		writer:     NewZeroCopyMessageWriter(),
		reactor:    reactor,
		lastActive: time.Now().Unix(),
	}

	atomic.StoreInt32(&peer.state, int32(PeerStateConnected))

	return peer, nil
}

// GetState 获取peer状态
func (peer *AsyncClientPeer) GetState() PeerState {
	return PeerState(atomic.LoadInt32(&peer.state))
}

// setState 设置peer状态
func (peer *AsyncClientPeer) setState(state PeerState) {
	atomic.StoreInt32(&peer.state, int32(state))
}

// CheckAfter 超时检查
func (peer *AsyncClientPeer) CheckAfter(t time.Duration) {
	go func() {
		time.AfterFunc(t, func() {
			if peer.Proc != nil && peer.ID == 0 {
				peer.Proc.FuncChan <- func() {
					base.Zap().Sugar().Warnf("auth timeout %v", peer.Connection)
					peer.Close()
				}
			}
		})
	}()
}

// Close 关闭连接
func (peer *AsyncClientPeer) Close() {
	if !atomic.CompareAndSwapInt32(&peer.state, int32(PeerStateConnected), int32(PeerStateClosing)) {
		return // 已经在关闭或已关闭
	}

	// 从reactor移除
	if peer.reactor != nil && peer.fd != -1 {
		peer.reactor.RemoveFd(peer.fd)
	}

	// 清理连接映射（macOS）
	if peer.fd != -1 {
		removeConnFromFd(peer.fd)
	}

	// 关闭连接
	if peer.Connection != nil {
		peer.Connection.Close()
		peer.Connection = nil
	}

	// 释放资源
	if peer.reader != nil {
		peer.reader.Release()
		peer.reader = nil
	}

	if peer.writer != nil {
		// 清空写入队列中的缓冲区
		for {
			ptr := peer.writer.writeQueue.Pop()
			if ptr == nil {
				break
			}
			req := (*writeRequest)(ptr)
			if req.buffer != nil {
				req.buffer.Release()
			}
		}
		peer.writer = nil
	}

	atomic.StoreInt32(&peer.state, int32(PeerStateClosed))
}

// Redirect 重新设置处理器（无锁实现）
func (peer *AsyncClientPeer) Redirect(proc *Processor) {
	atomic.StorePointer(&peer.redirectProc, unsafe.Pointer(proc))
}

// getProcessor 获取当前处理器
func (peer *AsyncClientPeer) getProcessor() *Processor {
	if redirectProc := atomic.LoadPointer(&peer.redirectProc); redirectProc != nil {
		newProc := (*Processor)(redirectProc)
		// 原子性地清除redirectProc并更新Proc
		if atomic.CompareAndSwapPointer(&peer.redirectProc, redirectProc, nil) {
			peer.Proc = newProc
		}
	}
	return peer.Proc
}

// SendMessage 发送消息（异步）
func (peer *AsyncClientPeer) SendMessage(msg proto.Message, msgid int32) error {
	if peer.GetState() != PeerStateConnected {
		return errors.New("connection is not connected")
	}

	// 检查是否为WebSocket连接（fd = -1表示虚拟连接）
	if peer.fd == -1 {
		// 对于WebSocket连接，直接序列化并发送
		data, err := proto.Marshal(msg)
		if err != nil {
			return err
		}

		// 创建带头部的完整消息
		buffer := GetBuffer()
		defer buffer.Release()

		totalLen := len(data) + 8
		if err := buffer.EnsureSpace(totalLen - buffer.Len()); err != nil {
			return err
		}

		// 写入头部
		head := MessageHead{
			Length: int32(len(data)),
			ID:     msgid,
		}
		if err := WriteHeadToBuffer(buffer, head); err != nil {
			return err
		}

		// 验证容量并写入消息体
		if buffer.Len()+len(data) > buffer.Cap() {
			return ErrInsufficientSize
		}

		copy(buffer.Data()[8:], data)
		buffer.SetLen(totalLen)

		// 直接通过连接发送
		_, err = peer.Connection.Write(buffer.Bytes())
		atomic.AddUint64(&peer.bytesWritten, uint64(totalLen))
		return err
	}

	// 对于真实的TCP/UDP连接，使用异步写入器
	return peer.writer.WriteMessage(peer.fd, msg, msgid)
}

// SendMessageBuffer 发送缓冲区（异步）
func (peer *AsyncClientPeer) SendMessageBuffer(data []byte) error {
	if peer.GetState() != PeerStateConnected {
		return errors.New("connection is not connected")
	}

	// 检查是否为WebSocket连接（fd = -1表示虚拟连接）
	if peer.fd == -1 {
		// 对于WebSocket连接，直接发送
		_, err := peer.Connection.Write(data)
		atomic.AddUint64(&peer.bytesWritten, uint64(len(data)))
		return err
	}

	// 对于真实的TCP/UDP连接，使用异步写入器
	// 创建写入请求
	buffer := GetBuffer()
	if err := buffer.EnsureSpace(len(data)); err != nil {
		buffer.Release()
		return err
	}

	// 边界检查
	if len(data) > buffer.Cap() {
		buffer.Release()
		return ErrInsufficientSize
	}

	copy(buffer.Data(), data)
	buffer.SetLen(len(data))

	writeReq := &writeRequest{
		fd:     peer.fd,
		buffer: buffer,
	}

	if !peer.writer.writeQueue.Push(unsafe.Pointer(writeReq)) {
		buffer.Release()
		return errors.New("write queue full")
	}

	peer.writer.tryAsyncWrite()
	// 注意：buffer的所有权已经转移到writeRequest，会在doWrite中释放
	return nil
}

// TransmitMsg 转发消息（异步）
func (peer *AsyncClientPeer) TransmitMsg(msg *Message) error {
	if peer.GetState() != PeerStateConnected {
		return errors.New("connection is not connected")
	}

	// 创建完整消息缓冲区
	buffer := GetBuffer()
	totalLen := int(msg.Head.Length) + 8

	if err := buffer.EnsureSpace(totalLen); err != nil {
		buffer.Release()
		return err
	}

	// 写入头部
	if err := WriteHeadToBuffer(buffer, msg.Head); err != nil {
		buffer.Release()
		return err
	}

	// 边界检查并写入消息体
	if buffer.Len()+len(msg.Body) > buffer.Cap() {
		buffer.Release()
		return ErrInsufficientSize
	}

	copy(buffer.Data()[8:], msg.Body)
	buffer.SetLen(totalLen)

	// 检查是否为WebSocket连接（fd = -1表示虚拟连接）
	if peer.fd == -1 {
		// 对于WebSocket连接，直接发送
		_, err := peer.Connection.Write(buffer.Bytes())
		atomic.AddUint64(&peer.bytesWritten, uint64(totalLen))
		buffer.Release()
		return err
	}

	// 对于真实的TCP/UDP连接，使用异步写入器
	// 创建写入请求
	writeReq := &writeRequest{
		fd:     peer.fd,
		buffer: buffer,
	}

	if !peer.writer.writeQueue.Push(unsafe.Pointer(writeReq)) {
		buffer.Release()
		return errors.New("write queue full")
	}

	peer.writer.tryAsyncWrite()
	// 注意：buffer的所有权已经转移到writeRequest，会在doWrite中释放
	return nil
}

// StartAsyncIO 开始异步I/O处理
func (peer *AsyncClientPeer) StartAsyncIO() error {
	// 将peer注册到reactor
	return peer.reactor.AddFd(peer.fd, EpollIn|EpollOut|EpollET, peer)
}

// OnRead 实现AsyncIOHandler接口 - 处理读事件
func (peer *AsyncClientPeer) OnRead(fd int, data []byte) error {
	if peer.GetState() != PeerStateConnected {
		return errors.New("peer not connected")
	}

	atomic.StoreInt64(&peer.lastActive, time.Now().Unix())
	atomic.AddUint64(&peer.bytesRead, uint64(len(data)))

	// 将数据投递给消息读取器
	messages, err := peer.reader.FeedData(data)
	if err != nil {
		base.Zap().Sugar().Warnf("message parse error: %v", err)
		peer.Close()
		return err
	}

	// 处理解析出的消息
	proc := peer.getProcessor()
	for _, zcMsg := range messages {
		// 转换为兼容的Message格式
		msg := &Message{
			Peer: &ClientPeer{AsyncClientPeer: peer},
			Head: zcMsg.Head,
			Body: zcMsg.GetBody(),
		}

		// 处理消息
		if proc.ImmediateMode {
			if cb, ok := proc.CallbackMap[msg.Head.ID]; ok {
				cb(msg)
			} else if proc.UnHandledHandler != nil {
				proc.UnHandledHandler(msg)
			}
		} else {
			select {
			case proc.MessageChan <- msg:
			default:
				base.Zap().Sugar().Warnf("message queue full, dropping message")
			}
		}

		// 释放零拷贝消息
		zcMsg.Release()
	}

	// 注意：data是[]byte参数，不是*Buffer对象
	// Buffer的释放应该由调用者（epoll循环）负责
	// 这里不应该释放buffer

	return nil
}

// OnWrite 实现AsyncIOHandler接口 - 处理写事件
func (peer *AsyncClientPeer) OnWrite(fd int) error {
	// 写事件由writer内部处理
	peer.writer.tryAsyncWrite()
	return nil
}

// OnError 实现AsyncIOHandler接口 - 处理错误事件
func (peer *AsyncClientPeer) OnError(fd int, err error) {
	base.Zap().Sugar().Warnf("peer error on fd %d: %v", fd, err)

	// 安全关闭连接
	if peer.GetState() == PeerStateConnected {
		peer.Close()

		// 发送移除事件
		event := &Event{
			ID:   RemoveEvent,
			Peer: &ClientPeer{AsyncClientPeer: peer},
		}

		proc := peer.getProcessor()
		if proc != nil {
			select {
			case proc.EventChan <- event:
			default:
				base.Zap().Sugar().Errorf("event queue full when handling error, dropping remove event")
			}
		}
	}
}

// OnClose 实现AsyncIOHandler接口 - 处理关闭事件
func (peer *AsyncClientPeer) OnClose(fd int) {
	base.Zap().Sugar().Infof("peer connection closed on fd %d", fd)

	// 获取最终统计信息
	bytesRead, bytesWritten, lastActive := peer.GetStats()
	base.Zap().Sugar().Debugf("connection stats: read=%d, written=%d, last_active=%v",
		bytesRead, bytesWritten, lastActive)

	// 安全关闭连接
	if peer.GetState() != PeerStateClosed {
		peer.Close()

		// 发送移除事件
		event := &Event{
			ID:   RemoveEvent,
			Peer: &ClientPeer{AsyncClientPeer: peer},
		}

		proc := peer.getProcessor()
		if proc != nil {
			select {
			case proc.EventChan <- event:
			default:
				base.Zap().Sugar().Errorf("event queue full when handling close, dropping remove event")
			}
		}
	}
}

// GetStats 获取统计信息
func (peer *AsyncClientPeer) GetStats() (bytesRead, bytesWritten uint64, lastActive time.Time) {
	return atomic.LoadUint64(&peer.bytesRead),
		atomic.LoadUint64(&peer.bytesWritten),
		time.Unix(atomic.LoadInt64(&peer.lastActive), 0)
}

// ============ 兼容性接口 ============

// ClientPeer 保持兼容的client connection peer
type ClientPeer struct {
	*AsyncClientPeer
}

func NewTcpConnection(address string, proc *Processor) (client *ClientPeer, err error) {
	socket, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}

	// 创建reactor（这里简化处理，实际应该复用全局reactor）
	reactor, err := NewEpollReactor()
	if err != nil {
		socket.Close()
		return nil, err
	}

	asyncPeer, err := NewAsyncClientPeer(socket, proc, reactor)
	if err != nil {
		socket.Close()
		reactor.Close()
		return nil, err
	}

	client = &ClientPeer{
		AsyncClientPeer: asyncPeer,
	}

	// 启动异步I/O
	if err := asyncPeer.StartAsyncIO(); err != nil {
		client.Close()
		return nil, err
	}

	// 启动reactor（在生产环境中应该复用全局reactor）
	go reactor.Run()

	return client, nil
}

// ConnectionHandler 兼容旧接口的连接处理器
func (peer *ClientPeer) ConnectionHandler() {
	// 新的实现基于异步I/O，这个方法主要用于兼容
	// 实际的I/O处理在AsyncIOHandler的回调中完成

	for peer.GetState() == PeerStateConnected {
		time.Sleep(100 * time.Millisecond)
	}

	base.Zap().Sugar().Infof("connection handler finished")
}

// ConnectionHandlerWithPreFunc 兼容旧接口
func (peer *ClientPeer) ConnectionHandlerWithPreFunc(f func() bool) {
	for peer.GetState() == PeerStateConnected && f() {
		time.Sleep(100 * time.Millisecond)
	}

	base.Zap().Sugar().Infof("connection handler with pre-func finished")
}
