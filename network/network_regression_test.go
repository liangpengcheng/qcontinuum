package network

import (
	"encoding/binary"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

func TestRingBufferMPSCNoLoss(t *testing.T) {
	const producers = 8
	const perProducer = 5000
	const total = producers * perProducer

	queue := NewRingBuffer(1024)
	var produced sync.WaitGroup
	produced.Add(producers)

	for producer := 0; producer < producers; producer++ {
		go func(base int) {
			defer produced.Done()
			for i := 0; i < perProducer; i++ {
				value := new(int)
				*value = base + i
				for !queue.Push(unsafePointer(value)) {
					runtime.Gosched()
				}
			}
		}(producer * perProducer)
	}

	seen := make(map[int]struct{}, total)
	for len(seen) < total {
		ptr := queue.Pop()
		if ptr == nil {
			runtime.Gosched()
			continue
		}
		value := *(*int)(ptr)
		if _, exists := seen[value]; exists {
			t.Fatalf("duplicate queue value %d", value)
		}
		seen[value] = struct{}{}
	}
	produced.Wait()

	if queue.Size() != 0 {
		t.Fatalf("queue should be empty, size=%d", queue.Size())
	}
}

func TestAsyncMessageReaderTransfersBufferOwnership(t *testing.T) {
	reader := NewAsyncMessageReader()
	defer reader.Release()

	data := make([]byte, messageHeaderSize+4)
	binary.LittleEndian.PutUint32(data[0:4], 4)
	binary.LittleEndian.PutUint32(data[4:8], 7)
	copy(data[8:], "test")

	messages, err := reader.FeedData(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %d", len(messages))
	}

	buffer := messages[0].Buffer
	if refs := atomic.LoadInt32(&buffer.refs); refs != 1 {
		t.Fatalf("message should own the only buffer reference, refs=%d", refs)
	}
	messages[0].Release()
	if refs := atomic.LoadInt32(&buffer.refs); refs != 0 {
		t.Fatalf("released message buffer should have zero references, refs=%d", refs)
	}
}

func TestAsyncMessageReaderReleasesCompletedMessagesOnLaterError(t *testing.T) {
	reader := NewAsyncMessageReader()
	defer reader.Release()

	firstBuffer := reader.buffer
	wire := make([]byte, messageHeaderSize*2)
	binary.LittleEndian.PutUint32(wire[0:4], 0)
	binary.LittleEndian.PutUint32(wire[4:8], 1)
	binary.LittleEndian.PutUint32(wire[8:12], uint32(maxMessageLength+1))
	binary.LittleEndian.PutUint32(wire[12:16], 2)

	messages, err := reader.FeedData(wire)
	if err == nil {
		t.Fatal("expected invalid message length error")
	}
	if messages != nil {
		t.Fatalf("messages must not escape an unsuccessful parse, got %d", len(messages))
	}
	if refs := atomic.LoadInt32(&firstBuffer.refs); refs != 0 {
		t.Fatalf("completed message buffer leaked after later parse error, refs=%d", refs)
	}
}

func TestReadMessageHandlesFragmentedReads(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	wire := make([]byte, messageHeaderSize+5)
	binary.LittleEndian.PutUint32(wire[0:4], 5)
	binary.LittleEndian.PutUint32(wire[4:8], 42)
	copy(wire[8:], "hello")

	go func() {
		for _, value := range wire {
			_, _ = client.Write([]byte{value})
		}
	}()

	head, body, err := ReadMessage(server)
	if err != nil {
		t.Fatal(err)
	}
	if head.ID != 42 || head.Length != 5 || string(body) != "hello" {
		t.Fatalf("unexpected message: head=%+v body=%q", head, body)
	}
}

func TestTCPServerStopIsIdempotent(t *testing.T) {
	server, err := NewTCP4Server("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.SetProcessor(NewProcessor())
	if err := server.StartAsync(); err != nil {
		t.Fatal(err)
	}
	server.Stop()
	server.Stop()
}

// unsafePointer is isolated so queue tests never synthesize pointers from
// integers; every queued pointer refers to a live Go allocation.
func unsafePointer(value *int) unsafe.Pointer {
	return unsafe.Pointer(value)
}
