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
	IOReader   io.Reader
}

func (ws *WebSocketPeer) Write(data []byte) (n int, err error) {
	return len(data), ws.Connection.WriteMessage(websocket.BinaryMessage, data)
}
func (ws *WebSocketPeer) Read(msg []byte) (n int, err error) {

	n, err = io.ReadAtLeast(ws.IOReader, msg, len(msg))

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
			base.Zap().Sugar().Errorf("upgrade:", err)
			return
		}
		defer ws.Close()
		base.Zap().Sugar().Infof("new webclient connected :%s", ws.RemoteAddr().String())
		wsConnection := &WebSocketPeer{
			Connection: ws,
		}

		// 为WebSocket创建简化的peer（不使用异步I/O）
		peer := &network.ClientPeer{
			AsyncClientPeer: &network.AsyncClientPeer{
				Connection: wsConnection,
				ID:         0,
				Proc:       proc,
			},
		}

		event := &network.Event{
			ID:   network.AddEvent,
			Peer: peer,
		}
		defer func() {
			leaveEvent := &network.Event{
				ID:   network.RemoveEvent,
				Peer: peer,
			}
			proc.EventChan <- leaveEvent
		}()
		proc.EventChan <- event

		// 使用传统的消息读取循环
		buffer := make([]byte, 65536)
		for {
			_, wsConnection.IOReader, err = ws.NextReader()
			if err != nil {
				base.Zap().Sugar().Errorf("websocket read error: %v", err)
				return
			}

			n, err := io.ReadAtLeast(wsConnection.IOReader, buffer, 8)
			if err != nil {
				base.Zap().Sugar().Errorf("websocket read message error: %v", err)
				return
			}

			hb := buffer[:8]
			body := buffer[8:n]
			h := network.ReadHead(hb)

			if h.ID > 10000000 || h.Length < 0 || h.Length > 1024*1024*100 {
				base.Zap().Sugar().Warnf("message error: id(%d),len(%d)", h.ID, h.Length)
				continue
			}

			msg := &network.Message{
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
	})
}
