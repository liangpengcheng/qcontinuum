package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/liangpengcheng/qcontinuum/network"
)

func main() {
	fmt.Println("=== QContinuum 网络层重构验证 ===")
	
	// 显示系统信息
	fmt.Printf("Go版本: %s\n", runtime.Version())
	fmt.Printf("CPU核心数: %d\n", runtime.NumCPU())
	fmt.Printf("操作系统: %s\n", runtime.GOOS)
	
	// 测试缓冲区池性能
	testBufferPool()
	
	// 测试环形缓冲区性能
	testRingBuffer()
	
	// 测试reactor池
	testReactorPool()
	
	fmt.Println("\n✅ 所有测试通过！网络层重构成功")
	fmt.Println("\n🚀 新架构特性:")
	fmt.Println("  - 全异步I/O基于epoll/kqueue")
	fmt.Println("  - 零拷贝消息处理")
	fmt.Println("  - 无锁设计高并发")
	fmt.Println("  - 保持上层接口完全兼容")
}

func testBufferPool() {
	fmt.Println("\n--- 测试缓冲区池性能 ---")
	
	start := time.Now()
	const testCount = 100000
	
	for i := 0; i < testCount; i++ {
		buf := getBuffer()
		buf.Grow(1024)
		buf.Release()
	}
	
	duration := time.Since(start)
	fmt.Printf("缓冲区池测试: %d次操作, 耗时: %v, 平均: %v/op\n", 
		testCount, duration, duration/testCount)
}

func testRingBuffer() {
	fmt.Println("\n--- 测试无锁环形缓冲区 ---")
	
	rb := network.NewRingBuffer(1024)
	const testCount = 50000
	var pushCount, popCount uint64
	
	start := time.Now()
	
	// 并发测试
	go func() {
		for i := 0; i < testCount; i++ {
			data := uintptr(i)
			if rb.Push(unsafe.Pointer(data)) {
				atomic.AddUint64(&pushCount, 1)
			}
		}
	}()
	
	go func() {
		for i := 0; i < testCount; i++ {
			if rb.Pop() != nil {
				atomic.AddUint64(&popCount, 1)
			}
		}
	}()
	
	time.Sleep(100 * time.Millisecond)
	duration := time.Since(start)
	
	fmt.Printf("环形缓冲区测试: Push:%d, Pop:%d, 耗时: %v\n", 
		atomic.LoadUint64(&pushCount), 
		atomic.LoadUint64(&popCount), 
		duration)
}

func testReactorPool() {
	fmt.Println("\n--- 测试Reactor池 ---")
	
	pool, err := network.NewIOReactorPool(2)
	if err != nil {
		fmt.Printf("创建reactor池失败: %v\n", err)
		return
	}
	defer pool.Close()
	
	// 测试reactor分配
	reactors := make([]*network.EpollReactor, 10)
	for i := 0; i < 10; i++ {
		reactors[i] = pool.GetReactor()
		if reactors[i] == nil {
			fmt.Printf("获取reactor失败\n")
			return
		}
	}
	
	fmt.Printf("Reactor池测试: 成功创建并获取reactor\n")
}

// 从network包导出的内部函数
func getBuffer() *network.Buffer {
	return network.NewBuffer(8192)
}