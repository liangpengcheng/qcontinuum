package network

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// TestRingBufferConcurrentSafety 测试RingBuffer在并发场景下的安全性
func TestRingBufferConcurrentSafety(t *testing.T) {
	rb := NewRingBuffer(1024)
	const numGoroutines = 10
	const opsPerGoroutine = 10000

	var pushCount, popCount uint64
	var wg sync.WaitGroup

	// 启动多个生产者
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				data := new(int)
				*data = id*opsPerGoroutine + j
				if rb.Push(unsafe.Pointer(data)) {
					atomic.AddUint64(&pushCount, 1)
				}
			}
		}(i)
	}

	// 启动多个消费者
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				if rb.Pop() != nil {
					atomic.AddUint64(&popCount, 1)
				}
			}
		}()
	}

	wg.Wait()

	// 消费剩余数据
	for rb.Pop() != nil {
		atomic.AddUint64(&popCount, 1)
	}

	t.Logf("Push count: %d, Pop count: %d", pushCount, popCount)

	// 验证：Pop数量不应超过Push数量（之前的bug可能导致数据重复读取）
	if popCount != pushCount {
		t.Errorf("Data integrity error: pop count (%d) != push count (%d)", popCount, pushCount)
	}
}

// TestProcessorPanicRecovery 测试Processor的panic恢复机制
func TestProcessorPanicRecovery(t *testing.T) {
	proc := NewProcessor()

	panicCount := 0
	normalCount := 0

	// 注册一个会panic的回调
	proc.AddCallback(1, func(msg *Message) {
		panicCount++
		if panicCount == 1 {
			panic("test panic in callback")
		}
	})

	// 注册一个正常的回调
	proc.AddCallback(2, func(msg *Message) {
		normalCount++
	})

	// 启动processor
	go proc.StartProcess()

	// 发送会导致panic的消息
	proc.MessageChan <- &Message{Head: MessageHead{ID: 1}}

	// 等待处理
	time.Sleep(100 * time.Millisecond)

	// 发送正常消息
	proc.MessageChan <- &Message{Head: MessageHead{ID: 2}}

	// 等待处理
	time.Sleep(100 * time.Millisecond)

	// 再次发送会导致panic的消息（验证processor仍然存活）
	proc.MessageChan <- &Message{Head: MessageHead{ID: 1}}

	// 等待处理
	time.Sleep(100 * time.Millisecond)

	// 停止processor
	proc.EventChan <- &Event{ID: ExitEvent, Param: "test done"}

	time.Sleep(100 * time.Millisecond)

	// 验证：panic后processor应该继续处理消息
	if normalCount != 1 {
		t.Errorf("Expected normalCount=1, got %d", normalCount)
	}
	if panicCount != 2 {
		t.Errorf("Expected panicCount=2, got %d", panicCount)
	}
}

// TestCodecThreadSafety 测试Codec的线程安全性
func TestCodecThreadSafety(t *testing.T) {
	var wg sync.WaitGroup
	const numGoroutines = 100

	// 并发设置和获取默认Codec
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			SetDefaultCodec(ProtobufCodec{})
			_ = DefaultCodec()
		}()
	}

	wg.Wait()

	// 验证最终状态是有效的
	codec := DefaultCodec()
	if codec == nil {
		t.Error("DefaultCodec should not be nil")
	}
}

// TestBufferSafeOperations 测试Buffer的安全操作
func TestBufferSafeOperations(t *testing.T) {
	buffer := GetBuffer()
	defer buffer.Release()

	// 测试SafeAppend
	testData := make([]byte, 1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	err := buffer.SafeAppend(testData)
	if err != nil {
		t.Errorf("SafeAppend failed: %v", err)
	}

	if buffer.Len() != 1024 {
		t.Errorf("Expected len=1024, got %d", buffer.Len())
	}

	// 验证数据正确性
	for i := 0; i < 1024; i++ {
		if buffer.Bytes()[i] != byte(i%256) {
			t.Errorf("Data mismatch at index %d", i)
			break
		}
	}
}

// TestAsyncMessageReaderSafety 测试异步消息读取器的安全性
func TestAsyncMessageReaderSafety(t *testing.T) {
	reader := NewAsyncMessageReader()
	defer reader.Release()

	// 构造一个有效的消息（8字节头 + 4字节消息体）
	// 头部：Length=4, ID=1
	msgData := []byte{
		4, 0, 0, 0, // Length = 4 (little endian)
		1, 0, 0, 0, // ID = 1 (little endian)
		't', 'e', 's', 't', // 消息体
	}

	messages, err := reader.FeedData(msgData)
	if err != nil {
		t.Fatalf("FeedData failed: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messages))
	}

	msg := messages[0]
	if msg.Head.Length != 4 {
		t.Errorf("Expected length=4, got %d", msg.Head.Length)
	}
	if msg.Head.ID != 1 {
		t.Errorf("Expected ID=1, got %d", msg.Head.ID)
	}

	body := msg.GetBody()
	if string(body) != "test" {
		t.Errorf("Expected body='test', got '%s'", string(body))
	}

	msg.Release()
}
