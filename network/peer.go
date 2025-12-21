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

const (
	// 发送超时配置
	sendTimeout    = 3 * time.Second       // 发送超时时间
	sendRetryDelay = 10 * time.Millisecond // 重试间隔
	sendMaxRetries = 300                   // 最大重试次数 (3秒/10ms = 300次)
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
	FD           int
	Flag         int32
	ID           int64
	state        int32          // PeerState
	redirectProc unsafe.Pointer // *Processor, 使用unsafe.Pointer实现无锁
	Proc         *Processor     // 保持兼容性，但应通过原子操作访问
	procPtr      unsafe.Pointer // *Processor, 原子访问的处理器指针

	// 异步I/O组件
	reader  *AsyncMessageReader
	writer  *ZeroCopyMessageWriter
	reactor *EpollReactor

	// 可插拔编解码器（默认protobuf），仅用于新的通用接口
	codec Codec

	// 统计信息
	bytesRead    uint64
	bytesWritten uint64
	lastActive   int64 // unix timestamp
}

// NewWebSocketClientPeer 创建WebSocket客户端peer（无文件描述符）
func NewWebSocketClientPeer(conn net.Conn, proc *Processor) *ClientPeer {
	peer := &AsyncClientPeer{
		Connection: conn,
		FD:         -1, // WebSocket使用虚拟fd
		Proc:       proc,
		procPtr:    unsafe.Pointer(proc), // 原子访问的处理器指针
		ID:         0,
		state:      int32(PeerStateConnected),
		lastActive: time.Now().Unix(),
		reader:     NewAsyncMessageReader(),
		writer:     NewZeroCopyMessageWriter(),
		reactor:    nil, // WebSocket不使用reactor
		codec:      DefaultCodec(),
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
		// 注意：不要调用 file.Close()，否则 FD 会失效
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
		FD:         fd,
		Proc:       proc,
		procPtr:    unsafe.Pointer(proc), // 原子访问的处理器指针
		reader:     NewAsyncMessageReader(),
		writer:     NewZeroCopyMessageWriter(),
		reactor:    reactor,
		lastActive: time.Now().Unix(),
		codec:      DefaultCodec(),
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
	if peer.reactor != nil && peer.FD != -1 {
		peer.reactor.RemoveFd(peer.FD)
	}

	// 清理连接映射（macOS）
	if peer.FD != -1 {
		removeConnFromFd(peer.FD)
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

// pushToWriteQueueWithTimeout 带超时的写队列推送
// 成功返回 true，超时返回 false
func (peer *AsyncClientPeer) pushToWriteQueueWithTimeout(req *writeRequest) bool {
	// 快速路径：直接尝试推送
	if peer.writer.writeQueue.Push(unsafe.Pointer(req)) {
		return true
	}

	// 慢速路径：重试直到超时
	deadline := time.Now().Add(sendTimeout)
	retries := 0

	for retries < sendMaxRetries {
		// 检查连接状态
		if peer.GetState() != PeerStateConnected {
			return false
		}

		// 检查超时
		if time.Now().After(deadline) {
			break
		}

		// 短暂等待后重试
		time.Sleep(sendRetryDelay)
		retries++

		if peer.writer.writeQueue.Push(unsafe.Pointer(req)) {
			if retries > 10 {
				// 记录慢速发送
				base.Zap().Sugar().Debugf("slow send detected: retries=%d, queue_size=%d",
					retries, peer.writer.writeQueue.Size())
			}
			return true
		}
	}

	// 超时失败
	remoteAddr := "unknown"
	if peer.Connection != nil {
		remoteAddr = peer.Connection.RemoteAddr().String()
	}
	base.Zap().Sugar().Errorf("send timeout after %v, queue_size=%d, remote=%s",
		sendTimeout, peer.writer.writeQueue.Size(), remoteAddr)

	// 自动断开连接
	go peer.Close()

	return false
}

// Redirect 重新设置处理器（无锁实现）
func (peer *AsyncClientPeer) Redirect(proc *Processor) {
	atomic.StorePointer(&peer.redirectProc, unsafe.Pointer(proc))
}

// getProcessor 获取当前处理器（完全原子化）
func (peer *AsyncClientPeer) getProcessor() *Processor {
	// 检查是否有重定向的处理器
	if redirectProc := atomic.LoadPointer(&peer.redirectProc); redirectProc != nil {
		newProc := (*Processor)(redirectProc)
		// 原子性地清除redirectProc并更新procPtr
		if atomic.CompareAndSwapPointer(&peer.redirectProc, redirectProc, nil) {
			atomic.StorePointer(&peer.procPtr, redirectProc)
			peer.Proc = newProc // 保持兼容性
		}
	}
	// 从procPtr原子读取
	if procPtr := atomic.LoadPointer(&peer.procPtr); procPtr != nil {
		return (*Processor)(procPtr)
	}
	return peer.Proc // 回退到非原子字段（兼容性）
}

// SendMessage 发送消息（异步，带超时保护）
func (peer *AsyncClientPeer) SendMessage(msg proto.Message, msgid int32) error {
	if peer.GetState() != PeerStateConnected {
		return errors.New("connection is not connected")
	}

	// 检查是否为WebSocket连接（fd = -1表示虚拟连接）
	if peer.FD == -1 {
		// 对于WebSocket连接，使用protobuf兼容的旧接口行为
		return peer.SendWithCodec(msg, msgid, ProtobufCodec{})
	}

	// 对于真实的TCP/UDP连接，使用自定义的带超时逻辑
	// 序列化消息
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	// 创建带头部的完整消息
	buffer := GetBuffer()
	totalLen := len(data) + 8

	if err := buffer.EnsureSpace(totalLen); err != nil {
		buffer.Release()
		return err
	}

	// 写入头部
	head := MessageHead{
		Length: int32(len(data)),
		ID:     msgid,
	}
	if err := WriteHeadToBuffer(buffer, head); err != nil {
		buffer.Release()
		return err
	}

	// 边界检查并写入消息体
	if buffer.Len()+len(data) > buffer.Cap() {
		buffer.Release()
		return ErrInsufficientSize
	}

	copy(buffer.Data()[8:], data)
	buffer.SetLen(totalLen)

	// 创建写入请求
	writeReq := &writeRequest{
		fd:     peer.FD,
		buffer: buffer,
	}

	// 使用带超时的推送
	if !peer.pushToWriteQueueWithTimeout(writeReq) {
		buffer.Release()
		return errors.New("send timeout or connection closed")
	}

	peer.writer.tryAsyncWrite()
	return nil
}

// SendMessageBuffer 发送缓冲区（异步）
func (peer *AsyncClientPeer) SendMessageBuffer(data []byte) error {
	if peer.GetState() != PeerStateConnected {
		return errors.New("connection is not connected")
	}

	// 检查是否为WebSocket连接（fd = -1表示虚拟连接）
	if peer.FD == -1 {
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
		fd:     peer.FD,
		buffer: buffer,
	}

	// 使用带超时的推送
	if !peer.pushToWriteQueueWithTimeout(writeReq) {
		buffer.Release()
		return errors.New("send timeout or connection closed")
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
	if peer.FD == -1 {
		// 对于WebSocket连接，直接发送
		_, err := peer.Connection.Write(buffer.Bytes())
		atomic.AddUint64(&peer.bytesWritten, uint64(totalLen))
		buffer.Release()
		return err
	}

	// 对于真实的TCP/UDP连接，使用异步写入器
	// 创建写入请求
	writeReq := &writeRequest{
		fd:     peer.FD,
		buffer: buffer,
	}

	// 使用带超时的推送
	if !peer.pushToWriteQueueWithTimeout(writeReq) {
		buffer.Release()
		return errors.New("send timeout or connection closed")
	}

	peer.writer.tryAsyncWrite()
	// 注意：buffer的所有权已经转移到writeRequest，会在doWrite中释放
	return nil
}

// ============ 新增的通用接口（保持向后兼容）============

// SetCodec 设置该peer默认使用的Codec。为nil时回退到默认Codec。
func (peer *AsyncClientPeer) SetCodec(c Codec) {
	if c == nil {
		peer.codec = DefaultCodec()
		return
	}
	peer.codec = c
}

// Send 使用peer的默认Codec进行序列化并发送
func (peer *AsyncClientPeer) Send(v interface{}, msgid int32) error {
	return peer.SendWithCodec(v, msgid, peer.codec)
}

// SendWithCodec 使用指定Codec进行序列化并发送
func (peer *AsyncClientPeer) SendWithCodec(v interface{}, msgid int32, codec Codec) error {
	if peer.GetState() != PeerStateConnected {
		return errors.New("connection is not connected")
	}
	if codec == nil {
		codec = DefaultCodec()
	}
	body, err := codec.Marshal(v)
	if err != nil {
		return err
	}

	// 创建带头部的完整消息
	buffer := GetBuffer()
	totalLen := len(body) + 8

	if err := buffer.EnsureSpace(totalLen); err != nil {
		buffer.Release()
		return err
	}

	head := MessageHead{Length: int32(len(body)), ID: msgid}
	if err := WriteHeadToBuffer(buffer, head); err != nil {
		buffer.Release()
		return err
	}
	if buffer.Len()+len(body) > buffer.Cap() {
		buffer.Release()
		return ErrInsufficientSize
	}
	copy(buffer.Data()[8:], body)
	buffer.SetLen(totalLen)

	// WebSocket直接写入，其他则入队
	if peer.FD == -1 {
		_, err = peer.Connection.Write(buffer.Bytes())
		atomic.AddUint64(&peer.bytesWritten, uint64(totalLen))
		buffer.Release()
		return err
	}

	// 对于真实的TCP/UDP连接，使用带超时的推送
	writeReq := &writeRequest{
		fd:     peer.FD,
		buffer: buffer,
	}

	if !peer.pushToWriteQueueWithTimeout(writeReq) {
		buffer.Release()
		return errors.New("send timeout or connection closed")
	}

	peer.writer.tryAsyncWrite()
	return nil
}

// StartAsyncIO 开始异步I/O处理
func (peer *AsyncClientPeer) StartAsyncIO() error {
	// 将peer注册到reactor
	return peer.reactor.AddFd(peer.FD, EpollIn|EpollOut|EpollET, peer)
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
		// 获取消息体数据
		body := zcMsg.GetBody()

		// 在非ImmediateMode下，消息会被发送到channel异步处理
		// 此时需要复制消息体，因为zcMsg.Release()后原数据可能被覆盖
		var msgBody []byte
		if proc.ImmediateMode {
			// 同步处理模式：可以安全使用零拷贝
			msgBody = body
		} else {
			// 异步处理模式：需要复制消息体
			msgBody = make([]byte, len(body))
			copy(msgBody, body)
		}

		// 转换为兼容的Message格式
		msg := &Message{
			Peer: &ClientPeer{AsyncClientPeer: peer},
			Head: zcMsg.Head,
			Body: msgBody,
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
