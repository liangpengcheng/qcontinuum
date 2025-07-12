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

			// 为WebSocket创建简化的peer（不使用异步I/O）
			peer := &ClientPeer{
				AsyncClientPeer: &AsyncClientPeer{
					Connection: wsPeer,
					fd:         -1,
					Proc:       proc,
					ID:         0,
					state:      int32(PeerStateConnected),
					lastActive: time.Now().Unix(),
				},
			}

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

			// 使用传统的消息读取循环（类似fasthttp实现）
			buffer := make([]byte, 65536)
			for {
				n, err := wsPeer.Read(buffer)
				if err != nil {
					base.Zap().Sugar().Infof("websocket read error: %v", err)
					return
				}

				if n < 8 {
					base.Zap().Sugar().Warnf("message too short: %d bytes", n)
					continue
				}

				hb := buffer[:8]
				body := buffer[8:n]
				h := ReadHead(hb)

				if h.ID > 10000000 || h.Length < 0 || h.Length > 1024*1024*100 {
					base.Zap().Sugar().Warnf("message error: id(%d),len(%d)", h.ID, h.Length)
					continue
				}

				msg := &Message{
					Peer: peer,
					Head: h,
					Body: body,
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
			}
		}))
}
