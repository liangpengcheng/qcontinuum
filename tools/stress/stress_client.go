package main

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gorilla/websocket"
	_kcp "github.com/xtaci/kcp-go/v5"
	"github.com/liangpengcheng/qcontinuum/network"
)

// ClientStats 客户端统计
type ClientStats struct {
	TotalSent     uint64
	TotalReceived uint64
	TotalLatency  int64 // 纳秒
	Errors        uint64
	StartTime     time.Time
}

func (s *ClientStats) AddLatency(d time.Duration) {
	atomic.AddInt64(&s.TotalLatency, int64(d))
}

// runClient 启动客户端压力测试
func runClient(tcpPort, wsPort, kcpPort, clientCount, rate, msgSize int, testConcurrent bool) {
	stats := &ClientStats{StartTime: time.Now()}

	fmt.Printf("开始压力测试: TCP:%d, WS:%d, KCP:%d, 每个协议并发:%d, 速率:%d/s, 大小:%d\n",
		tcpPort, wsPort, kcpPort, clientCount, rate, msgSize)

	var wg sync.WaitGroup

	// 启动TCP客户端
	for i := 0; i < clientCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runTCPClient(fmt.Sprintf("127.0.0.1:%d", tcpPort), rate, msgSize, stats, testConcurrent)
		}(i)
	}

	// 启动WebSocket客户端
	for i := 0; i < clientCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runWSClient(fmt.Sprintf("ws://127.0.0.1:%d/ws", wsPort), rate, msgSize, stats, testConcurrent)
		}(i)
	}

	// 启动KCP客户端
	for i := 0; i < clientCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runKCPClient(fmt.Sprintf("127.0.0.1:%d", kcpPort), rate, msgSize, stats, testConcurrent)
		}(i)
	}

	// 启动统计打印
	go printClientStats(stats)

	wg.Wait()
}

func printClientStats(stats *ClientStats) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastSent, lastReceived uint64

	for range ticker.C {
		sent := atomic.LoadUint64(&stats.TotalSent)
		received := atomic.LoadUint64(&stats.TotalReceived)
		latency := atomic.LoadInt64(&stats.TotalLatency)
		errs := atomic.LoadUint64(&stats.Errors)

		sQPS := sent - lastSent
		rQPS := received - lastReceived

		avgLatency := time.Duration(0)
		if received > 0 {
			avgLatency = time.Duration(latency / int64(received))
		}

		fmt.Printf("\r[客户端统计] 发送QPS:%d | 接收QPS:%d | 平均延迟:%v | 总发送:%d | 总接收:%d | 错误:%d    ",
			sQPS, rQPS, avgLatency, sent, received, errs)

		lastSent = sent
		lastReceived = received
	}
}

func runTCPClient(addr string, rate, msgSize int, stats *ClientStats, testConcurrent bool) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		atomic.AddUint64(&stats.Errors, 1)
		return
	}
	defer conn.Close()

	handleClientConn(conn, rate, msgSize, stats, testConcurrent)
}

func runWSClient(url string, rate, msgSize int, stats *ClientStats, testConcurrent bool) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		atomic.AddUint64(&stats.Errors, 1)
		return
	}
	defer conn.Close()

	wsConn := &WSNetConn{conn: conn}
	handleClientConn(wsConn, rate, msgSize, stats, testConcurrent)
}

func runKCPClient(addr string, rate, msgSize int, stats *ClientStats, testConcurrent bool) {
	conn, err := _kcp.DialWithOptions(addr, nil, 0, 0)
	if err != nil {
		atomic.AddUint64(&stats.Errors, 1)
		return
	}
	defer conn.Close()

	handleClientConn(conn, rate, msgSize, stats, testConcurrent)
}

func handleClientConn(conn net.Conn, rate, msgSize int, stats *ClientStats, testConcurrent bool) {
	stop := make(chan struct{})
	
	// 接收协程
	go func() {
		buf := make([]byte, 64*1024)
		for {
			// 先读头部
			n, err := conn.Read(buf[:8])
			if err != nil {
				close(stop)
				return
			}
			if n < 8 {
				continue
			}

			head, _ := network.ReadHeadFromBuffer(buf[:8])
			bodyLen := int(head.Length)
			
			if bodyLen > 0 {
				// 检查缓冲区容量是否足够
				requiredSize := 8 + bodyLen
				if cap(buf) < requiredSize {
					newBuf := make([]byte, requiredSize)
					copy(newBuf, buf[:8])
					buf = newBuf
				}

				totalRead := 0
				for totalRead < bodyLen {
					n, err := conn.Read(buf[8+totalRead : 8+bodyLen])
					if err != nil {
						close(stop)
						return
					}
					totalRead += n
				}
			}

			// 计算延迟
			if head.ID == MsgIDEchoResponse {
				sendTimeNano := int64(0)
				fmt.Sscanf(string(buf[8:8+bodyLen]), "%d", &sendTimeNano)
				if sendTimeNano > 0 {
					// 仅当是较小消息包时才计算延迟，因为大包解析可能会有问题或者格式不同
					// 这里假设我们一直用同样的格式
					stats.AddLatency(time.Since(time.Unix(0, sendTimeNano)))
				}
				atomic.AddUint64(&stats.TotalReceived, 1)
			} else if head.ID == MsgIDBroadcast {
				atomic.AddUint64(&stats.TotalReceived, 1)
			}
		}
	}()

	// 发送函数
	sendFunc := func() {
		now := time.Now().UnixNano()
		payload := fmt.Sprintf("%d", now)
		
		// 填充额外数据以达到目标大小
		if len(payload) < msgSize {
			padding := make([]byte, msgSize-len(payload))
			payload += string(padding)
		}

		body := []byte(payload)
		
		head := network.MessageHead{
			Length: int32(len(body)),
			ID:     MsgIDEchoRequest,
		}
		
		msg := make([]byte, 8+len(body))
		// 使用unsafe.Pointer直接写入头部，模拟高性能写入
		*(*network.MessageHead)(unsafe.Pointer(&msg[0])) = head
		copy(msg[8:], body)
		
		_, err := conn.Write(msg)
		if err != nil {
			atomic.AddUint64(&stats.Errors, 1)
		} else {
			atomic.AddUint64(&stats.TotalSent, 1)
		}
	}

	// 并发发送测试
	if testConcurrent {
		for i := 0; i < 5; i++ {
			go func() {
				ticker := time.NewTicker(time.Second / time.Duration(rate/5))
				for {
					select {
					case <-ticker.C:
						sendFunc()
					case <-stop:
						ticker.Stop()
						return
					}
				}
			}()
		}
	}

	// 主发送逻辑
	ticker := time.NewTicker(time.Second / time.Duration(rate))
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sendFunc()
		case <-stop:
			return
		}
	}
}

// WSNetConn 包装gorilla/websocket为net.Conn接口
type WSNetConn struct {
	conn *websocket.Conn
	writeMu sync.Mutex // 发送锁
	reader  struct {
		sync.Mutex
		data []byte
	}
}

func (c *WSNetConn) Read(b []byte) (n int, err error) {
	c.reader.Lock()
	defer c.reader.Unlock()

	if len(c.reader.data) == 0 {
		mt, data, err := c.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		if mt != websocket.BinaryMessage {
			return 0, fmt.Errorf("wrong message type")
		}
		c.reader.data = data
	}

	n = copy(b, c.reader.data)
	c.reader.data = c.reader.data[n:]
	return n, nil
}

func (c *WSNetConn) Write(b []byte) (n int, err error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	err = c.conn.WriteMessage(websocket.BinaryMessage, b)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *WSNetConn) Close() error {
	return c.conn.Close()
}

func (c *WSNetConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *WSNetConn) RemoteAddr() net.Addr               { return c.conn.RemoteAddr() }
func (c *WSNetConn) SetDeadline(t time.Time) error {
	c.conn.SetReadDeadline(t)
	return c.conn.SetWriteDeadline(t)
}
func (c *WSNetConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *WSNetConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }
