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
	state        int32 // PeerState
	redirectProc unsafe.Pointer // *Processor, 使用unsafe.Pointer实现无锁
	Proc         *Processor
	
	// 异步I/O组件
	reader *AsyncMessageReader
	writer *ZeroCopyMessageWriter
	reactor *EpollReactor
	
	// 统计信息
	bytesRead    uint64
	bytesWritten uint64
	lastActive   int64 // unix timestamp
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
	if peer.reactor != nil {
		peer.reactor.RemoveFd(peer.fd)
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
	
	return peer.writer.WriteMessage(peer.fd, msg, msgid)
}

// SendMessageBuffer 发送缓冲区（异步）
func (peer *AsyncClientPeer) SendMessageBuffer(data []byte) error {
	if peer.GetState() != PeerStateConnected {
		return errors.New("connection is not connected")
	}
	
	// 创建写入请求
	buffer := getBuffer()
	if buffer.cap < len(data) {
		buffer.Grow(len(data))
	}
	
	copy(buffer.data, data)
	buffer.len = len(data)
	
	writeReq := &writeRequest{
		fd:     peer.fd,
		buffer: buffer,
	}
	
	if !peer.writer.writeQueue.Push(unsafe.Pointer(writeReq)) {
		buffer.Release()
		return errors.New("write queue full")
	}
	
	peer.writer.tryAsyncWrite()
	return nil
}

// TransmitMsg 转发消息（异步）
func (peer *AsyncClientPeer) TransmitMsg(msg *Message) error {
	if peer.GetState() != PeerStateConnected {
		return errors.New("connection is not connected")
	}
	
	// 创建完整消息缓冲区
	buffer := getBuffer()
	totalLen := int(msg.Head.Length) + 8
	
	if buffer.cap < totalLen {
		buffer.Grow(totalLen)
	}
	
	// 写入头部
	WriteHeadToBuffer(buffer, msg.Head)
	
	// 写入消息体
	copy(buffer.data[8:], msg.Body)
	buffer.len = totalLen
	
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
	
	// 释放输入数据缓冲区
	putBuffer((*Buffer)(unsafe.Pointer(&data[0])))
	
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
	base.Zap().Sugar().Warnf("peer error: %v", err)
	peer.Close()
	
	// 发送移除事件
	event := &Event{
		ID:   RemoveEvent,
		Peer: &ClientPeer{AsyncClientPeer: peer},
	}
	
	if peer.Proc != nil {
		select {
		case peer.Proc.EventChan <- event:
		default:
			base.Zap().Sugar().Warnf("event queue full")
		}
	}
}

// OnClose 实现AsyncIOHandler接口 - 处理关闭事件
func (peer *AsyncClientPeer) OnClose(fd int) {
	base.Zap().Sugar().Infof("peer connection closed")
	peer.Close()
	
	// 发送移除事件
	event := &Event{
		ID:   RemoveEvent,
		Peer: &ClientPeer{AsyncClientPeer: peer},
	}
	
	if peer.Proc != nil {
		select {
		case peer.Proc.EventChan <- event:
		default:
			base.Zap().Sugar().Warnf("event queue full")
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
