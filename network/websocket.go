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
			peer := &ClientPeer{
				Connection: &WebSocketPeer{
					Connection: ws,
				},
				Proc: proc,
			}
			event := &Event{
				ID:   AddEvent,
				Peer: peer,
			}
			proc.EventChan <- event
			peer.ConnectionHandler()

		}))
}
