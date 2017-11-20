package network

import (
	"net/http"

	"github.com/liangpengcheng/qcontinuum/base"

	"golang.org/x/net/websocket"
)

// WebSocketServer websocket服务器
type WebSocketServer struct {
	Connection *websocket.Conn
}

// NewWebSocket 新建一个websocket处理,这个是golang系统http建立的http服务的socket，访问在/ws下
func NewWebSocket(proc *Processor) {
	http.Handle("/ws", websocket.Handler(
		func(ws *websocket.Conn) {
			base.LogInfo("new webclient connected :%s", ws.RemoteAddr().String())
			peer := &ClientPeer{
				Connection: ws,
				Proc:       proc,
			}
			event := &Event{
				ID:   AddEvent,
				Peer: peer,
			}
			proc.EventChan <- event
			go peer.ConnectionHandler()
		}))
}
