package fasthttp

import (
	"errors"
	"net"
	"time"

	"github.com/fasthttp/router"
	"github.com/liangpengcheng/qcontinuum/base"
	"github.com/liangpengcheng/qcontinuum/network"
	"github.com/valyala/fasthttp"

	"github.com/fasthttp/websocket"
)

var upgrader = websocket.FastHTTPUpgrader{}

type WebSocketPeer struct {
	Connection *websocket.Conn
}

func (ws *WebSocketPeer) Write(data []byte) (n int, err error) {
	return len(data), ws.Connection.WriteMessage(websocket.BinaryMessage, data)
}
func (ws *WebSocketPeer) Read(msg []byte) (n int, err error) {
	mt, content, err := ws.Connection.ReadMessage()
	if mt == websocket.BinaryMessage {
		copy(msg, content)
		return len(content), nil
	} else {
		return 0, errors.New("can't deal this kind  of message")
	}
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
	ws.Connection.SetReadDeadline(t)
	return ws.Connection.SetWriteDeadline(t)
}

// SetReadDeadline sets the connection's network read deadline.
func (ws *WebSocketPeer) SetReadDeadline(t time.Time) error {
	return ws.Connection.SetReadDeadline(t)
}

// SetWriteDeadline sets the connection's network write deadline.
func (ws *WebSocketPeer) SetWriteDeadline(t time.Time) error {
	return ws.Connection.SetWriteDeadline(t)
}
func SetupWebsocket(proc *network.Processor, path string, r *router.Router) {
	notallowed := r.MethodNotAllowed
	r.MethodNotAllowed = func(ctx *fasthttp.RequestCtx) {
		if base.String(ctx.Path()) == path {
			err := upgrader.Upgrade(ctx, func(ws *websocket.Conn) {
				defer ws.Close()
				peer := &network.ClientPeer{
					Connection: &WebSocketPeer{
						Connection: ws,
					},
					Proc: proc,
				}
				event := &network.Event{
					ID:   network.AddEvent,
					Peer: peer,
				}
				proc.EventChan <- event
				peer.ConnectionHandler()
			})
			base.CheckError(err, "websocket")
		} else {
			notallowed(ctx)
		}
	}
}
