package network

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

// 异步I/O事件类型
const (
	EventRead  = 1 << iota
	EventWrite
	EventError
	EventClose
)

// IOEvent I/O事件结构
type IOEvent struct {
	fd     int
	events uint32
	data   unsafe.Pointer
}

// AsyncIOHandler 异步I/O处理器接口
type AsyncIOHandler interface {
	OnRead(fd int, data []byte) error
	OnWrite(fd int) error
	OnError(fd int, err error)
	OnClose(fd int)
}

// IOReactorPool reactor池接口
type IOReactorPool struct {
	reactors []*EpollReactor
	next     uint64
}

// NewIOReactorPool 创建reactor池
func NewIOReactorPool(size int) (*IOReactorPool, error) {
	if size <= 0 {
		size = runtime.NumCPU()
	}

	pool := &IOReactorPool{
		reactors: make([]*EpollReactor, size),
	}

	for i := 0; i < size; i++ {
		reactor, err := NewEpollReactor()
		if err != nil {
			// 清理已创建的reactor
			for j := 0; j < i; j++ {
				pool.reactors[j].Close()
			}
			return nil, err
		}
		pool.reactors[i] = reactor
		go reactor.Run()
	}

	return pool, nil
}

// GetReactor 获取下一个reactor
func (p *IOReactorPool) GetReactor() *EpollReactor {
	if len(p.reactors) == 0 {
		return nil
	}
	n := atomic.AddUint64(&p.next, 1)
	return p.reactors[n%uint64(len(p.reactors))]
}

// Close 关闭所有reactor
func (p *IOReactorPool) Close() error {
	for _, reactor := range p.reactors {
		if reactor != nil {
			reactor.Close()
		}
	}
	return nil
}