package network

import (
	"errors"
	"io"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/golang/protobuf/proto"
	"github.com/liangpengcheng/qcontinuum/base"
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
	bodySize := int(m.Head.Length)
	if m.Offset+bodySize > m.Buffer.len {
		return nil
	}
	return m.Buffer.data[m.Offset : m.Offset+bodySize]
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
func WriteHeadToBuffer(buffer *Buffer, head MessageHead) {
	if buffer.cap-buffer.len < 8 {
		buffer.Grow(8)
	}

	// 直接内存写入
	*(*MessageHead)(unsafe.Pointer(&buffer.data[buffer.len])) = head
	buffer.len += 8
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
		buffer:      getBuffer(),
		bytesNeeded: 8, // 先读取8字节头部
	}
}

// FeedData 向读取器投递数据
func (r *AsyncMessageReader) FeedData(data []byte) ([]*ZeroCopyMessage, error) {
	var messages []*ZeroCopyMessage

	// 确保缓冲区足够大
	if r.buffer.len+len(data) > r.buffer.cap {
		r.buffer.Grow(len(data))
	}

	// 零拷贝追加数据
	copy(r.buffer.data[r.buffer.len:], data)
	r.buffer.len += len(data)

	for {
		if !r.headerParsed {
			// 尝试解析消息头
			if r.buffer.len >= 8 {
				head, err := ReadHeadFromBuffer(r.buffer.data)
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

				// 移除已解析的头部
				copy(r.buffer.data, r.buffer.data[8:r.buffer.len])
				r.buffer.len -= 8
			} else {
				break
			}
		}

		if r.headerParsed {
			// 检查是否有完整消息体
			if r.buffer.len >= r.bytesNeeded {
				// 创建零拷贝消息
				msg := NewZeroCopyMessage()
				msg.Head = r.currentHead

				// 真正的零拷贝：重用已有的缓冲区数据
				// 增加缓冲区引用计数以防止被释放
				msg.Buffer = r.buffer
				msg.Buffer.AddRef()
				msg.Offset = 0 // 数据从缓冲区开始处开始

				// 如果缓冲区中还有更多数据，需要为下一个消息创建新的缓冲区
				if r.buffer.len > r.bytesNeeded {
					// 创建新的缓冲区来存储剩余数据
					remainingData := r.buffer.len - r.bytesNeeded
					newBuffer := getBuffer()
					if newBuffer.cap < remainingData {
						newBuffer.Grow(remainingData)
					}
					copy(newBuffer.data, r.buffer.data[r.bytesNeeded:r.buffer.len])
					newBuffer.len = remainingData

					// 更新缓冲区长度到当前消息的大小
					r.buffer.len = r.bytesNeeded

					// 切换到新的缓冲区
					r.buffer = newBuffer
				} else {
					// 缓冲区中正好是一个完整消息，获取新的缓冲区
					r.buffer.len = r.bytesNeeded
					r.buffer = getBuffer()
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
	buffer := getBuffer()
	totalLen := len(data)

	if buffer.cap < totalLen+8 {
		buffer.Grow(totalLen + 8)
	}

	// 写入头部
	head := MessageHead{
		Length: int32(totalLen),
		ID:     msgID,
	}
	WriteHeadToBuffer(buffer, head)

	// 写入消息体
	copy(buffer.data[8:], data)
	buffer.len = totalLen + 8

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

// doWrite 执行实际写入
func (w *ZeroCopyMessageWriter) doWrite(req *writeRequest) {
	defer req.buffer.Release()

	for req.offset < req.buffer.len {
		n, err := syscall.Write(req.fd, req.buffer.data[req.offset:req.buffer.len])
		if err != nil {
			if err == syscall.EAGAIN {
				// 暂时无法写入，重新加入队列
				if !w.writeQueue.Push(unsafe.Pointer(req)) {
					base.Zap().Sugar().Warnf("failed to requeue write request")
				}
				return
			}
			base.Zap().Sugar().Warnf("write error: %v", err)
			return
		}
		req.offset += n
	}
}

// ReadHead read the message head (兼容旧接口)
func ReadHead(src []byte) MessageHead {
	head, _ := ReadHeadFromBuffer(src)
	return head
}

// ReadFromConnect 读取消息 (兼容旧接口)
func ReadFromConnect(conn io.Reader, length int) ([]byte, error) {
	buffer := getBuffer()
	defer buffer.Release()

	if buffer.cap < length {
		buffer.Grow(length)
	}

	n, err := io.ReadFull(conn, buffer.data[:length])
	if err != nil {
		return nil, err
	}

	// 返回数据拷贝以保持兼容性
	result := make([]byte, n)
	copy(result, buffer.data[:n])
	return result, nil
}

// GetMessageBuffer 把msg转成buffer (兼容旧接口)
func GetMessageBuffer(msg proto.Message, id int32) ([]byte, error) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}

	buffer := getBuffer()
	defer buffer.Release()

	totalLen := len(data) + 8
	if buffer.cap < totalLen {
		buffer.Grow(totalLen)
	}

	// 写入头部
	head := MessageHead{
		Length: int32(len(data)),
		ID:     id,
	}
	WriteHeadToBuffer(buffer, head)

	// 写入消息体
	copy(buffer.data[8:], data)
	buffer.len = totalLen

	// 返回拷贝以保持兼容性
	result := make([]byte, totalLen)
	copy(result, buffer.data[:totalLen])
	return result, nil
}
