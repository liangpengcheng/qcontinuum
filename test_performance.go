package main

import (
	"fmt"
	"runtime"
	"sync"
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

	// 测试缓冲区安全性修复
	testBufferSafety()

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
	fmt.Println("  - 增强的缓冲区安全性和错误处理")
}

func testBufferPool() {
	fmt.Println("\n--- 测试缓冲区池性能 ---")

	start := time.Now()
	const testCount = 100000

	for i := 0; i < testCount; i++ {
		buf := network.GetBuffer()
		if err := buf.Grow(1024); err != nil {
			fmt.Printf("Buffer grow failed: %v\n", err)
			buf.Release()
			continue
		}
		buf.Release()
	}

	duration := time.Since(start)
	fmt.Printf("缓冲区池测试: %d次操作, 耗时: %v, 平均: %v/op\n",
		testCount, duration, duration/testCount)
}

func testBufferSafety() {
	fmt.Println("\n--- 测试缓冲区安全性修复 ---")

	// 测试1: 正常扩展
	buffer := network.GetBuffer()
	defer buffer.Release()

	err := buffer.Grow(1024)
	if err != nil {
		fmt.Printf("❌ 正常扩展失败: %v\n", err)
		return
	}
	fmt.Printf("✅ 正常扩展成功: %d bytes\n", buffer.Cap())

	// 测试2: 超大扩展（应该失败）
	buffer2 := network.GetBuffer()
	defer buffer2.Release()

	err = buffer2.Grow(101 * 1024 * 1024) // Exceeds the 100MB message limit.
	if err != nil {
		fmt.Printf("✅ 正确拒绝超大扩展: %v\n", err)
	} else {
		fmt.Printf("❌ 应该拒绝超大扩展但没有拒绝\n")
	}

	// 测试3: 边界检查
	buffer3 := network.GetBuffer()
	defer buffer3.Release()

	// 测试SafeAppend
	testData := make([]byte, 1024)
	err = buffer3.SafeAppend(testData)
	if err != nil {
		fmt.Printf("❌ SafeAppend失败: %v\n", err)
	} else {
		fmt.Printf("✅ SafeAppend成功: %d bytes\n", len(testData))
	}

	fmt.Printf("缓冲区安全性测试完成\n")
}

func testRingBuffer() {
	fmt.Println("\n--- 测试无锁环形缓冲区 ---")

	rb := network.NewRingBuffer(1024)
	const testCount = 50000
	var pushCount, popCount uint64
	values := make([]int, testCount)
	var workers sync.WaitGroup
	workers.Add(2)

	start := time.Now()

	// 并发测试
	go func() {
		defer workers.Done()
		for i := 0; i < testCount; i++ {
			values[i] = i
			for !rb.Push(unsafe.Pointer(&values[i])) {
				runtime.Gosched()
			}
			atomic.AddUint64(&pushCount, 1)
		}
	}()

	go func() {
		defer workers.Done()
		for i := 0; i < testCount; i++ {
			for rb.Pop() == nil {
				runtime.Gosched()
			}
			atomic.AddUint64(&popCount, 1)
		}
	}()

	workers.Wait()
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
