package network

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	defaultBufferSize = 8192
	maxBufferSize     = 1024 * 1024
	poolSize          = 1024
)

// Buffer 零拷贝缓冲区
type Buffer struct {
	data []byte
	len  int
	cap  int
	refs int32 // 引用计数
}

// NewBuffer 创建新缓冲区
func NewBuffer(size int) *Buffer {
	return &Buffer{
		data: make([]byte, size),
		cap:  size,
		refs: 1,
	}
}

// Bytes 返回有效数据
func (b *Buffer) Bytes() []byte {
	return b.data[:b.len]
}

// Data 返回底层数据数组（用于直接写入）
func (b *Buffer) Data() []byte {
	return b.data
}

// Cap 返回缓冲区容量
func (b *Buffer) Cap() int {
	return b.cap
}

// Len 返回有效数据长度
func (b *Buffer) Len() int {
	return b.len
}

// SetLen 设置有效数据长度
func (b *Buffer) SetLen(n int) {
	if n >= 0 && n <= b.cap {
		b.len = n
	}
}

// AddRef 增加引用计数
func (b *Buffer) AddRef() {
	atomic.AddInt32(&b.refs, 1)
}

// Release 释放引用
func (b *Buffer) Release() {
	if atomic.AddInt32(&b.refs, -1) == 0 {
		putBuffer(b)
	}
}

// Reset 重置缓冲区
func (b *Buffer) Reset() {
	b.len = 0
	atomic.StoreInt32(&b.refs, 1)
}

// Grow 扩展缓冲区
func (b *Buffer) Grow(n int) {
	if b.cap-b.len >= n {
		return
	}
	
	newCap := b.cap * 2
	for newCap < b.len+n {
		newCap *= 2
	}
	
	if newCap > maxBufferSize {
		newCap = maxBufferSize
	}
	
	newData := make([]byte, newCap)
	copy(newData, b.data[:b.len])
	b.data = newData
	b.cap = newCap
}

// BufferPool 无锁缓冲区池
type BufferPool struct {
	pool sync.Pool
}

var globalBufferPool = &BufferPool{
	pool: sync.Pool{
		New: func() interface{} {
			return NewBuffer(defaultBufferSize)
		},
	},
}

// GetBuffer 从池中获取缓冲区（公共API）
func GetBuffer() *Buffer {
	return getBuffer()
}

// getBuffer 从池中获取缓冲区（内部API）
func getBuffer() *Buffer {
	buf := globalBufferPool.pool.Get().(*Buffer)
	buf.Reset()
	return buf
}

// putBuffer 归还缓冲区到池
func putBuffer(buf *Buffer) {
	if buf.cap > maxBufferSize {
		return // 丢弃过大的缓冲区
	}
	globalBufferPool.pool.Put(buf)
}

// RingBuffer 无锁环形缓冲区
type RingBuffer struct {
	buffer []unsafe.Pointer
	mask   uint64
	_      [7]uint64 // 避免false sharing
	head   uint64
	_      [7]uint64 // 避免false sharing
	tail   uint64
	_      [7]uint64 // 避免false sharing
}

// NewRingBuffer 创建环形缓冲区
func NewRingBuffer(size uint64) *RingBuffer {
	// 确保size是2的幂
	if size&(size-1) != 0 {
		panic("size must be power of 2")
	}
	
	return &RingBuffer{
		buffer: make([]unsafe.Pointer, size),
		mask:   size - 1,
	}
}

// Push 推入数据
func (rb *RingBuffer) Push(data unsafe.Pointer) bool {
	head := atomic.LoadUint64(&rb.head)
	tail := atomic.LoadUint64(&rb.tail)
	
	if head-tail >= uint64(len(rb.buffer)) {
		return false // 缓冲区满
	}
	
	rb.buffer[head&rb.mask] = data
	atomic.StoreUint64(&rb.head, head+1)
	return true
}

// Pop 弹出数据
func (rb *RingBuffer) Pop() unsafe.Pointer {
	tail := atomic.LoadUint64(&rb.tail)
	head := atomic.LoadUint64(&rb.head)
	
	if tail >= head {
		return nil // 缓冲区空
	}
	
	data := rb.buffer[tail&rb.mask]
	atomic.StoreUint64(&rb.tail, tail+1)
	return data
}

// Size 获取当前大小
func (rb *RingBuffer) Size() uint64 {
	head := atomic.LoadUint64(&rb.head)
	tail := atomic.LoadUint64(&rb.tail)
	return head - tail
}

// MessageBuffer 消息缓冲区
type MessageBuffer struct {
	buffer *Buffer
	offset int
	msgLen int32
	msgID  int32
}

// NewMessageBuffer 创建消息缓冲区
func NewMessageBuffer() *MessageBuffer {
	return &MessageBuffer{
		buffer: getBuffer(),
	}
}

// Reset 重置消息缓冲区
func (mb *MessageBuffer) Reset() {
	if mb.buffer != nil {
		mb.buffer.Release()
	}
	mb.buffer = getBuffer()
	mb.offset = 0
	mb.msgLen = 0
	mb.msgID = 0
}

// GetMessageBytes 获取消息字节（零拷贝）
func (mb *MessageBuffer) GetMessageBytes() []byte {
	return mb.buffer.data[mb.offset : mb.offset+int(mb.msgLen)]
}

// Release 释放消息缓冲区
func (mb *MessageBuffer) Release() {
	if mb.buffer != nil {
		mb.buffer.Release()
		mb.buffer = nil
	}
}