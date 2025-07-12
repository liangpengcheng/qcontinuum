package network

/*
Buffer生命周期管理指导：

1. Buffer所有权规则：
   - 谁分配，谁释放：使用getBuffer()获取Buffer的函数负责调用putBuffer()释放
   - 引用计数：当Buffer被多个对象共享时，使用AddRef()增加引用计数
   - 自动释放：Buffer.Release()会自动处理引用计数，当引用计数为0时归还到池中

2. 典型使用模式：

   模式1 - 简单使用：
   ```go
   buffer := getBuffer()
   defer buffer.Release()
   // 使用buffer...
   ```

   模式2 - 共享Buffer：
   ```go
   buffer := getBuffer()
   defer buffer.Release()

   // 如果需要在其他地方使用这个buffer：
   buffer.AddRef()
   someStruct.buffer = buffer
   // someStruct负责调用buffer.Release()
   ```

   模式3 - 零拷贝消息：
   ```go
   msg := NewZeroCopyMessage()
   msg.Buffer = existingBuffer
   msg.Buffer.AddRef() // 增加引用计数
   // 当msg.Release()时，会自动减少引用计数
   ```

3. 禁止的操作：
   - 不要将[]byte转换为*Buffer：putBuffer((*Buffer)(unsafe.Pointer(&data[0])))
   - 不要手动管理Buffer的data字段
   - 不要在不同的goroutine间共享Buffer而不使用引用计数

4. 调试建议：
   - 使用-race标志检测竞态条件
   - 定期检查Buffer池的使用情况
   - 监控内存使用，确保没有Buffer泄露
*/

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
	// 统计信息（用于调试）
	allocCount int64 // 分配次数
	freeCount  int64 // 释放次数
	hitCount   int64 // 缓存命中次数
	missCount  int64 // 缓存未命中次数
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
	obj := globalBufferPool.pool.Get()
	if obj == nil {
		// 池中没有可用的Buffer，创建新的
		atomic.AddInt64(&globalBufferPool.missCount, 1)
		buf := NewBuffer(defaultBufferSize)
		buf.Reset()
		atomic.AddInt64(&globalBufferPool.allocCount, 1)
		return buf
	}

	buf := obj.(*Buffer)
	buf.Reset()
	atomic.AddInt64(&globalBufferPool.hitCount, 1)
	atomic.AddInt64(&globalBufferPool.allocCount, 1)
	return buf
}

// putBuffer 归还缓冲区到池
func putBuffer(buf *Buffer) {
	if buf == nil {
		return
	}
	if buf.cap > maxBufferSize {
		return // 丢弃过大的缓冲区
	}
	atomic.AddInt64(&globalBufferPool.freeCount, 1)
	globalBufferPool.pool.Put(buf)
}

// GetBufferStats 获取缓冲区池统计信息（用于调试）
func GetBufferStats() (alloc, free, hit, miss int64) {
	return atomic.LoadInt64(&globalBufferPool.allocCount),
		atomic.LoadInt64(&globalBufferPool.freeCount),
		atomic.LoadInt64(&globalBufferPool.hitCount),
		atomic.LoadInt64(&globalBufferPool.missCount)
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
