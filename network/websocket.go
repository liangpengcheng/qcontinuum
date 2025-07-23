package network

import (
	"net"
	"net/http"
	"time"

	"github.com/liangpengcheng/qcontinuum/base"

	"golang.org/x/net/websocket"
)

type WebSocketPeer struct {
	Connection *websocket.Conn
}

func (ws *WebSocketPeer) Write(data []byte) (n int, err error) {
	return len(data), websocket.Message.Send(ws.Connection, data)
}
func (ws *WebSocketPeer) Read(msg []byte) (n int, err error) {
	return ws.Connection.Read(msg)
}

func (ws *WebSocketPeer) Close() error {
	return ws.Connection.Close()
}
func (ws *WebSocketPeer) LocalAddr() net.Addr {
	return ws.Connection.LocalAddr()
}
func (ws *WebSocketPeer) RemoteAddr() net.Addr {
	return ws.Connection.RemoteAddr()
}

// SetDeadline sets the connection's network read & write deadlines.
func (ws *WebSocketPeer) SetDeadline(t time.Time) error {
	return ws.Connection.SetDeadline(t)
}

// SetReadDeadline sets the connection's network read deadline.
func (ws *WebSocketPeer) SetReadDeadline(t time.Time) error {
	return ws.Connection.SetReadDeadline(t)
}

// SetWriteDeadline sets the connection's network write deadline.
func (ws *WebSocketPeer) SetWriteDeadline(t time.Time) error {
	return ws.Connection.SetWriteDeadline(t)
}

// WebSocketServer websocket服务器
type WebSocketServer struct {
	Connection *WebSocketPeer
}

// NewWebSocket 新建一个websocket处理,这个是golang系统http建立的http服务的socket，访问在/ws下
func NewWebSocket(path string, proc *Processor) {
	http.Handle(path, websocket.Handler(
		func(ws *websocket.Conn) {
			base.Zap().Sugar().Infof("new webclient connected :%s", ws.RemoteAddr().String())

			// 创建WebSocket连接的包装器
			wsPeer := &WebSocketPeer{Connection: ws}

			// 为WebSocket创建专用的peer
			peer := NewWebSocketClientPeer(wsPeer, proc)

			event := &Event{
				ID:   AddEvent,
				Peer: peer,
			}
			proc.EventChan <- event
			defer func() {
				leaveEvent := &Event{
					ID:   RemoveEvent,
					Peer: peer,
				}
				proc.EventChan <- leaveEvent
			}()

			// 使用零拷贝消息读取器
			reader := NewAsyncMessageReader()
			defer reader.Release()

			for {
				// 使用缓冲区池读取数据
				buffer := GetBuffer()

				// 扩展缓冲区以适应可能的大消息
				if buffer.Cap() < 64*1024 {
					if err := buffer.Grow(64*1024 - buffer.Cap()); err != nil {
						buffer.Release()
						base.Zap().Sugar().Warnf("websocket buffer grow failed: %v", err)
						return
					}
				}

				// 读取WebSocket消息到缓冲区
				n, err := wsPeer.Read(buffer.Data())
				if err != nil {
					buffer.Release()
					base.Zap().Sugar().Infof("websocket read error: %v", err)
					return
				}

				if n == 0 {
					buffer.Release()
					continue
				}

				// 投递给零拷贝消息读取器
				messages, err := reader.FeedData(buffer.Data()[:n])
				buffer.Release() // 立即释放缓冲区

				if err != nil {
					base.Zap().Sugar().Warnf("message parse error: %v", err)
					continue
				}

				// 处理解析出的零拷贝消息
				for _, zcMsg := range messages {
					msg := &Message{
						Peer: peer,
						Head: zcMsg.Head,
						Body: zcMsg.GetBody(), // 零拷贝获取消息体
					}

					if proc.ImmediateMode {
						if cb, ok := proc.CallbackMap[msg.Head.ID]; ok {
							cb(msg)
						} else if proc.UnHandledHandler != nil {
							proc.UnHandledHandler(msg)
						}
					} else {
						proc.MessageChan <- msg
					}

					// 释放零拷贝消息
					zcMsg.Release()
				}
			}
		}))
}
