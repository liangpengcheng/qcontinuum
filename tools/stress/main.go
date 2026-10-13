package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/gin-gonic/gin"
	"github.com/liangpengcheng/qcontinuum/ginhttp"
	"github.com/liangpengcheng/qcontinuum/kcp"
	"github.com/liangpengcheng/qcontinuum/network"
)

const (
	MsgIDEchoRequest  int32 = 1001
	MsgIDEchoResponse int32 = 1002
	MsgIDBroadcast    int32 = 1003
)

// ServerStats 服务器统计
type ServerStats struct {
	TotalConnections  uint64 // 总连接数
	ActiveConnections int64  // 活跃连接数
	TotalReceived     uint64 // 总接收消息数
	TotalSent         uint64 // 总发送消息数
	Errors            uint64 // 错误数
	startTime         time.Time
}

// PeerManager 管理所有连接的peer
type PeerManager struct {
	sync.RWMutex
	peers map[*network.ClientPeer]bool
}

func NewPeerManager() *PeerManager {
	return &PeerManager{
		peers: make(map[*network.ClientPeer]bool),
	}
}

func (pm *PeerManager) Add(peer *network.ClientPeer) {
	pm.Lock()
	defer pm.Unlock()
	pm.peers[peer] = true
}

func (pm *PeerManager) Remove(peer *network.ClientPeer) {
	pm.Lock()
	defer pm.Unlock()
	delete(pm.peers, peer)
}

func (pm *PeerManager) Broadcast(msgID int32, data []byte) int {
	pm.RLock()
	defer pm.RUnlock()

	count := 0
	for peer := range pm.peers {
		if peer != nil {
			err := peer.SendMessageBuffer(data)
			if err == nil {
				count++
			}
		}
	}
	return count
}

func (pm *PeerManager) Count() int {
	pm.RLock()
	defer pm.RUnlock()
	return len(pm.peers)
}

// GetAllPeers 获取所有peer的快照
func (pm *PeerManager) GetAllPeers() []*network.ClientPeer {
	pm.RLock()
	defer pm.RUnlock()
	result := make([]*network.ClientPeer, 0, len(pm.peers))
	for peer := range pm.peers {
		result = append(result, peer)
	}
	return result
}

// StressServer 压力测试服务器
type StressServer struct {
	tcpServer     *network.Server
	ginServer     *http.Server
	kcpServer     *kcp.AsyncKCPServer
	processor     *network.Processor
	stopping      int32
	stats         *ServerStats
	peerManager   *PeerManager
	immediateMode bool
}

func NewStressServer(immediateMode bool) *StressServer {
	return &StressServer{
		stats:         &ServerStats{startTime: time.Now()},
		peerManager:   NewPeerManager(),
		immediateMode: immediateMode,
	}
}

func (s *StressServer) setupProcessor() *network.Processor {
	var proc *network.Processor
	if s.immediateMode {
		proc = network.NewProcessor()
		proc.ImmediateMode = true
	} else {
		proc = network.NewProcessorWithLoopTime(10 * time.Millisecond)
		proc.ImmediateMode = false
	}

	// Echo处理：收到什么回什么
	proc.AddCallback(MsgIDEchoRequest, func(msg *network.Message) {
		atomic.AddUint64(&s.stats.TotalReceived, 1)

		// 构造响应，使用响应ID
		responseHead := network.MessageHead{
			Length: int32(len(msg.Body)),
			ID:     MsgIDEchoResponse,
		}

		// 构造完整消息
		responseData := make([]byte, 8+len(msg.Body))
		*(*network.MessageHead)(unsafe.Pointer(&responseData[0])) = responseHead
		copy(responseData[8:], msg.Body)

		err := msg.Peer.SendMessageBuffer(responseData)
		if err != nil {
			atomic.AddUint64(&s.stats.Errors, 1)
		} else {
			atomic.AddUint64(&s.stats.TotalSent, 1)
		}
	})

	// 广播处理
	proc.AddCallback(MsgIDBroadcast, func(msg *network.Message) {
		atomic.AddUint64(&s.stats.TotalReceived, 1)

		// 向所有客户端广播
		count := s.peerManager.Broadcast(MsgIDBroadcast, msg.Body)
		atomic.AddUint64(&s.stats.TotalSent, uint64(count))
	})

	// 连接事件
	proc.AddEventCallback(network.AddEvent, func(event *network.Event) {
		atomic.AddUint64(&s.stats.TotalConnections, 1)
		atomic.AddInt64(&s.stats.ActiveConnections, 1)
		s.peerManager.Add(event.Peer)
		if atomic.LoadInt32(&s.stopping) != 0 {
			event.Peer.Close()
		}
	})

	// 断开事件
	proc.AddEventCallback(network.RemoveEvent, func(event *network.Event) {
		atomic.AddInt64(&s.stats.ActiveConnections, -1)
		s.peerManager.Remove(event.Peer)
	})

	// Protobuf 回显（供 SDK 联调与压测，消息号 2001 段；与 feat/csharp-sdk 一致）
	registerProtobufEcho(proc, s.stats)

	s.processor = proc
	return proc
}

func (s *StressServer) StartTCP(port int) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	server, err := network.NewTCP4Server(addr)
	if err != nil {
		return err
	}
	s.tcpServer = server

	go func() {
		fmt.Printf("[TCP] 服务器启动在 %s (ImmediateMode=%v)\n", addr, s.immediateMode)
		server.BlockAccept(s.processor)
	}()

	return nil
}

func (s *StressServer) StartGinWebSocket(port int) error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	ginhttp.SetupWebsocket(router, s.processor)

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	s.ginServer = &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		fmt.Printf("[WebSocket] Gin服务器启动在 %s\n", addr)
		if err := s.ginServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[WebSocket] 服务器错误: %v\n", err)
		}
	}()

	return nil
}

func (s *StressServer) StartKCP(port int) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	server, err := kcp.NewAsyncKCPServer(addr)
	if err != nil {
		return err
	}

	server.SetProcessor(s.processor)
	s.kcpServer = server

	go func() {
		fmt.Printf("[KCP] 服务器启动在 %s\n", addr)
		if err := server.StartAsync(); err != nil {
			fmt.Printf("[KCP] 服务器错误: %v\n", err)
		}
	}()

	return nil
}

func (s *StressServer) PrintStats() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastReceived, lastSent uint64

	for range ticker.C {
		received := atomic.LoadUint64(&s.stats.TotalReceived)
		sent := atomic.LoadUint64(&s.stats.TotalSent)
		active := atomic.LoadInt64(&s.stats.ActiveConnections)
		errors := atomic.LoadUint64(&s.stats.Errors)

		recvQPS := received - lastReceived
		sendQPS := sent - lastSent

		fmt.Printf("\r[统计] 连接:%d | 接收QPS:%d | 发送QPS:%d | 总接收:%d | 总发送:%d | 错误:%d   ",
			active, recvQPS, sendQPS, received, sent, errors)

		lastReceived = received
		lastSent = sent
	}
}

func (s *StressServer) Stop() {
	atomic.StoreInt32(&s.stopping, 1)
	if s.tcpServer != nil {
		s.tcpServer.Stop()
	}
	if s.ginServer != nil {
		s.ginServer.Close()
	}
	if s.kcpServer != nil {
		s.kcpServer.Stop()
	}
	// HTTP Close does not close hijacked WebSocket connections. Late AddEvent
	// callbacks also close their peer while stopping, so pending adds cannot leak.
	for _, peer := range s.peerManager.GetAllPeers() {
		peer.Close()
	}
}

func runServer(tcpPort, wsPort, kcpPort int, immediateMode bool) {
	server := NewStressServer(immediateMode)
	proc := server.setupProcessor()

	// 启动processor
	processed := make(chan struct{})
	go func() { proc.StartProcess(); close(processed) }()

	// 启动各协议服务器
	if err := server.StartTCP(tcpPort); err != nil {
		fmt.Printf("TCP服务器启动失败: %v\n", err)
	}

	if err := server.StartGinWebSocket(wsPort); err != nil {
		fmt.Printf("WebSocket服务器启动失败: %v\n", err)
	}

	if err := server.StartKCP(kcpPort); err != nil {
		fmt.Printf("KCP服务器启动失败: %v\n", err)
	}

	// 打印统计
	go server.PrintStats()

	// 等待退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n正在关闭服务器...")
	server.Stop()
	proc.EventChan <- &network.Event{ID: network.ExitEvent, Param: "stress server stopped"}
	<-processed
}

// runClient is implemented in stress_client.go
func main() {
	mode := flag.String("mode", "server", "运行模式: server 或 client")
	tcpPort := flag.Int("tcp-port", 9001, "TCP端口")
	wsPort := flag.Int("ws-port", 9002, "WebSocket端口")
	kcpPort := flag.Int("kcp-port", 9003, "KCP端口")
	clients := flag.Int("clients", 10, "每种协议的客户端数量")
	rate := flag.Int("rate", 100, "每个客户端每秒发送消息数")
	msgSize := flag.Int("msg-size", 64, "消息体大小(bytes)")
	immediateMode := flag.Bool("immediate", false, "使用立即模式处理消息")
	testConcurrent := flag.Bool("test-concurrent", false, "测试多goroutine同时向同一peer发消息")
	connectDelay := flag.Int("connect-delay", 0, "每个客户端连接之间的延时(毫秒)，0表示无延时")
	flag.Parse()

	fmt.Println("========================================")
	fmt.Println("  QContinuum 网络压力测试框架")
	fmt.Println("========================================")

	switch *mode {
	case "server":
		fmt.Printf("运行模式: 服务器 (ImmediateMode=%v)\n", *immediateMode)
		runServer(*tcpPort, *wsPort, *kcpPort, *immediateMode)
	case "client":
		fmt.Printf("运行模式: 客户端 (并发数=%d, 速率=%d/s, 消息大小=%d, 连接延时=%dms)\n", *clients, *rate, *msgSize, *connectDelay)
		runClient(*tcpPort, *wsPort, *kcpPort, *clients, *rate, *msgSize, *testConcurrent, *connectDelay)
	default:
		fmt.Println("未知模式，使用 --mode server 或 --mode client")
		os.Exit(1)
	}
}
