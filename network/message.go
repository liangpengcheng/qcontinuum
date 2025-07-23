package network

import (
	"errors"

	"sync/atomic"
	"unsafe"

	"github.com/golang/protobuf/proto"
)

var maxMessageLength = 1024 * 1024 * 100 // 100MB

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
func ReadHeadFromBuffer(data []byte) (MessageHead, error) {
	if len(data) < 8 {
		return MessageHead{}, errors.New("insufficient data for message head")
	}

	// 直接从内存读取，避免拷贝
	head := (*MessageHead)(unsafe.Pointer(&data[0]))
	return *head, nil
}

// WriteHeadToBuffer 零拷贝写入消息头到缓冲区
func WriteHeadToBuffer(buffer *Buffer, head MessageHead) error {
	if buffer.Cap()-buffer.Len() < 8 {
		if err := buffer.Grow(8); err != nil {
			return err
		}
	}

	// 再次验证容量（双重检查）
	if buffer.Cap()-buffer.Len() < 8 {
		return ErrInsufficientSize
	}

	// 直接内存写入
	*(*MessageHead)(unsafe.Pointer(&buffer.Data()[buffer.Len()])) = head
	buffer.SetLen(buffer.Len() + 8)
	return nil
}

// AsyncMessageReader 异步消息读取器
type AsyncMessageReader struct {
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
func (r *AsyncMessageReader) FeedData(data []byte) ([]*ZeroCopyMessage, error) {
	var messages []*ZeroCopyMessage

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
	if r.buffer != nil {
		r.buffer.Release()
		r.buffer = nil
	}
}

// ZeroCopyMessageWriter 零拷贝消息写入器
type ZeroCopyMessageWriter struct {
	writeQueue *RingBuffer
	writing    int32
}

// NewZeroCopyMessageWriter 创建零拷贝消息写入器
func NewZeroCopyMessageWriter() *ZeroCopyMessageWriter {
	return &ZeroCopyMessageWriter{
		writeQueue: NewRingBuffer(1024),
	}
}

// WriteMessage 异步写入消息
func (w *ZeroCopyMessageWriter) WriteMessage(fd int, msg proto.Message, msgID int32) error {
	// 序列化消息
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	// 创建带头部的完整消息
	buffer := GetBuffer()
	totalLen := len(data)

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
	if buffer.Len()+len(data) > buffer.Cap() {
		buffer.Release()
		return ErrInsufficientSize
	}

	copy(buffer.Data()[8:], data)
	buffer.SetLen(totalLen + 8)

	// 加入写入队列
	writeReq := &writeRequest{
		fd:     fd,
		buffer: buffer,
	}

	if !w.writeQueue.Push(unsafe.Pointer(writeReq)) {
		buffer.Release()
		return errors.New("write queue full")
	}

	// 尝试异步写入
	w.tryAsyncWrite()

	return nil
}

type writeRequest struct {
	fd     int
	buffer *Buffer
	offset int
}

// tryAsyncWrite 尝试异步写入
func (w *ZeroCopyMessageWriter) tryAsyncWrite() {
	if !atomic.CompareAndSwapInt32(&w.writing, 0, 1) {
		return // 已经在写入中
	}

	go func() {
		defer atomic.StoreInt32(&w.writing, 0)

		for {
			ptr := w.writeQueue.Pop()
			if ptr == nil {
				break
			}

			req := (*writeRequest)(ptr)
			w.doWrite(req)
		}
	}()
}

// doWrite 执行实际写入 - 平台特定实现在message_unix.go和message_windows.go中
