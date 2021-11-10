package ginhttp

import (
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/liangpengcheng/qcontinuum/base"
	"github.com/liangpengcheng/qcontinuum/network"
)

type WebSocketPeer struct {
	Connection *websocket.Conn
}

func (ws *WebSocketPeer) Write(data []byte) (n int, err error) {
	return len(data), ws.Connection.WriteMessage(websocket.BinaryMessage, data)
}
func (ws *WebSocketPeer) Read(msg []byte) (n int, err error) {

	var r io.Reader
	_, r, err = ws.Connection.NextReader()
	if err != nil {
		return 0, err
	}
	n, err = io.ReadAtLeast(r, msg, len(msg))

	if err == nil {
		return
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

func SetupWebsocket(router *gin.Engine, proc *network.Processor) {

	var upgrader = websocket.Upgrader{
		// 解决跨域问题
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	} // use default options
	router.GET("/ws", func(c *gin.Context) {
		ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			base.LogError("upgrade:", err)
			return
		}
		defer ws.Close()
		base.LogInfo("new webclient connected :%s", ws.RemoteAddr().String())
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
}
