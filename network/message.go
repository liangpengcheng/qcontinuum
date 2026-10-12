package network

import (
	"errors"

	"sync"

	"github.com/golang/protobuf/proto"
)

var maxMessageLength = 1024 * 1024 * 100 // 100MB

var ErrMessageReaderClosed = errors.New("message reader is closed")

// MessageHead the message head (零拷贝设计)
type MessageHead struct {
	Length int32
	ID     int32
}

// ZeroCopyMessage 零拷贝消息结构
type ZeroCopyMessage struct {
	Head   MessageHead
	Buffer *Buffer
	Offset int
}

// NewZeroCopyMessage 创建零拷贝消息
func NewZeroCopyMessage() *ZeroCopyMessage {
	return &ZeroCopyMessage{
		Buffer: nil, // 不预分配Buffer，由使用者设置
		Offset: 0,
	}
}

// GetBody 获取消息体（零拷贝）
func (m *ZeroCopyMessage) GetBody() []byte {
	if m.Buffer == nil {
		return nil
	}
	bodySize := int(m.Head.Length)
	if m.Offset+bodySize > m.Buffer.Len() {
		return nil
	}
	return m.Buffer.Data()[m.Offset : m.Offset+bodySize]
}

// Release 释放消息
func (m *ZeroCopyMessage) Release() {
	if m.Buffer != nil {
		m.Buffer.Release()
		m.Buffer = nil
	}
	m.Offset = 0
}

// ReadHeadFromBuffer 从缓冲区零拷贝读取消息头

// 直接从内存读取，避免拷贝

// WriteHeadToBuffer 零拷贝写入消息头到缓冲区

// 再次验证容量（双重检查）

// 直接内存写入

// AsyncMessageReader 异步消息读取器
type AsyncMessageReader struct {
	mu           sync.Mutex // Serializes parsing and release of the owned buffer.
	buffer       *Buffer
	headerParsed bool
	currentHead  MessageHead
	bytesNeeded  int
}

// NewAsyncMessageReader 创建异步消息读取器
func NewAsyncMessageReader() *AsyncMessageReader {
	return &AsyncMessageReader{
		buffer:      GetBuffer(),
		bytesNeeded: 8, // 先读取8字节头部
	}
}

// FeedData 向读取器投递数据
func (r *AsyncMessageReader) FeedData(data []byte) (result []*ZeroCopyMessage, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.buffer == nil {
		return nil, ErrMessageReaderClosed
	}
	var messages []*ZeroCopyMessage
	defer func() {
		if err == nil {
			return
		}
		for _, msg := range messages {
			msg.Release()
		}
		messages = nil
	}()

	// 检查输入数据大小
	if len(data) == 0 {
		return messages, nil
	}

	// 检查单次数据大小是否合理
	if len(data) > maxMessageLength {
		return nil, errors.New("input data too large")
	}

	// 确保缓冲区足够大 - 使用新的错误处理
	requiredSpace := len(data)
	if r.buffer.Len()+requiredSpace > r.buffer.Cap() {
		if err := r.buffer.Grow(requiredSpace); err != nil {
			return nil, err
		}
	}

	// 验证容量足够（边界检查）
	if r.buffer.Len()+len(data) > r.buffer.Cap() {
		return nil, ErrInsufficientSize
	}

	// 零拷贝追加数据
	copy(r.buffer.Data()[r.buffer.Len():], data)
	r.buffer.SetLen(r.buffer.Len() + len(data))

	for {
		if !r.headerParsed {
			// 尝试解析消息头
			if r.buffer.Len() >= 8 {
				head, err := ReadHeadFromBuffer(r.buffer.Data())
				if err != nil {
					return nil, err
				}

				// 验证消息长度
				if head.Length < 0 || head.Length > int32(maxMessageLength) {
					return nil, errors.New("invalid message length")
				}

				r.currentHead = head
				r.headerParsed = true
				r.bytesNeeded = int(head.Length)

				// 边界检查：确保移除操作安全
				if r.buffer.Len() < 8 {
					return nil, errors.New("buffer underflow")
				}

				// 移除已解析的头部
				copy(r.buffer.Data(), r.buffer.Data()[8:r.buffer.Len()])
				r.buffer.SetLen(r.buffer.Len() - 8)
			} else {
				break
			}
		}

		if r.headerParsed {
			// 检查是否有完整消息体
			if r.buffer.Len() >= r.bytesNeeded {
				// 创建零拷贝消息
				msg := NewZeroCopyMessage()
				msg.Head = r.currentHead

				// 真正的零拷贝：重用已有的缓冲区数据
				// 增加缓冲区引用计数以防止被释放
				msg.Buffer = r.buffer
				msg.Buffer.AddRef()
				msg.Offset = 0 // 数据从缓冲区开始处开始

				// 如果缓冲区中还有更多数据，需要为下一个消息创建新的缓冲区
				if r.buffer.Len() > r.bytesNeeded {
					// 创建新的缓冲区来存储剩余数据
					remainingData := r.buffer.Len() - r.bytesNeeded
					newBuffer := GetBuffer()

					// 使用新的错误处理方式
					if err := newBuffer.EnsureSpace(remainingData); err != nil {
						// 清理已创建的消息
						msg.Release()
						return nil, err
					}

					// 边界检查
					if remainingData > 0 && r.bytesNeeded+remainingData > r.buffer.Len() {
						msg.Release()
						newBuffer.Release()
						return nil, errors.New("invalid buffer state")
					}

					copy(newBuffer.Data(), r.buffer.Data()[r.bytesNeeded:r.buffer.Len()])
					newBuffer.SetLen(remainingData)

					// 更新缓冲区长度到当前消息的大小
					r.buffer.SetLen(r.bytesNeeded)

					// 切换到新的缓冲区
					r.buffer = newBuffer
				} else {
					// 缓冲区中正好是一个完整消息，获取新的缓冲区
					r.buffer.SetLen(r.bytesNeeded)
					r.buffer = GetBuffer()
				}

				messages = append(messages, msg)
				// Transfer the reader's ownership of the old buffer to msg.
				msg.Buffer.Release()

				// 重置状态
				r.headerParsed = false
				r.bytesNeeded = 8
			} else {
				break
			}
		}
	}

	return messages, nil
}

// Release 释放读取器
func (r *AsyncMessageReader) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.buffer != nil {
		r.buffer.Release()
		r.buffer = nil
	}
}

// ZeroCopyMessageWriter 零拷贝消息写入器
type ZeroCopyMessageWriter struct {
	writeQueue  *RingBuffer
	writing     int32
	closed      int32
	queuedBytes int64

	pendingMu   sync.Mutex
	pending     *writeRequest
	lifecycleMu sync.Mutex
	wg          sync.WaitGroup

	onWriteInterest func(bool) error
	onError         func(error)
	onBytes         func(int)
}

// NewZeroCopyMessageWriter 创建零拷贝消息写入器
func NewZeroCopyMessageWriter() *ZeroCopyMessageWriter {
	return &ZeroCopyMessageWriter{
		writeQueue: NewRingBuffer(1024),
	}
}

// WriteMessage 异步写入消息
func (w *ZeroCopyMessageWriter) WriteMessage(fd int, msg proto.Message, msgID int32) error {
	// 兼容旧接口：明确使用protobuf进行序列化
	return w.WriteMessageWithCodec(fd, msg, msgID, ProtobufCodec{})
}

// WriteMessageWithCodec 使用指定Codec序列化并写入
func (w *ZeroCopyMessageWriter) WriteMessageWithCodec(fd int, v interface{}, msgID int32, codec Codec) error {
	if codec == nil {
		codec = DefaultCodec()
	}
	data, err := codec.Marshal(v)
	if err != nil {
		return err
	}
	return w.WriteBody(fd, data, msgID)
}

// WriteBody 直接写入已编码的消息体（零拷贝队列）
func (w *ZeroCopyMessageWriter) WriteBody(fd int, body []byte, msgID int32) error {
	if len(body) > maxMessageLength {
		return ErrBufferTooLarge
	}
	// 创建带头部的完整消息
	buffer := GetBuffer()
	totalLen := len(body)

	if err := buffer.EnsureSpace(totalLen + 8 - buffer.Len()); err != nil {
		buffer.Release()
		return err
	}

	// 写入头部
	head := MessageHead{
		Length: int32(totalLen),
		ID:     msgID,
	}
	if err := WriteHeadToBuffer(buffer, head); err != nil {
		buffer.Release()
		return err
	}

	// 边界检查并写入消息体
	if buffer.Len()+len(body) > buffer.Cap() {
		buffer.Release()
		return ErrInsufficientSize
	}

	copy(buffer.Data()[8:], body)
	buffer.SetLen(totalLen + 8)

	// 加入写入队列
	writeReq := &writeRequest{
		fd:     fd,
		buffer: buffer,
	}

	if !w.enqueue(writeReq) {
		buffer.Release()
		return errors.New("write queue full")
	}

	// 尝试异步写入
	w.tryAsyncWrite()

	return nil
}

type writeRequest struct {
	fd        int
	buffer    *Buffer
	offset    int
	accounted int32
	released  int32
}

// tryAsyncWrite 尝试异步写入

// 已经在写入中

// doWrite 执行实际写入 - 平台特定实现在message_unix.go和message_windows.go中
