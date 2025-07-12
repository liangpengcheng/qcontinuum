package fasthttp

import (
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/fasthttp/router"
	"github.com/liangpengcheng/qcontinuum/base"
	"github.com/liangpengcheng/qcontinuum/network"
	"github.com/valyala/fasthttp"

	"github.com/fasthttp/websocket"
)

var upgrader = websocket.FastHTTPUpgrader{
	CheckOrigin: func(ctx *fasthttp.RequestCtx) bool {
		return true
	},
}

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

	notfound := r.NotFound
	r.NotFound = func(ctx *fasthttp.RequestCtx) {
		if base.String(ctx.Path()) == path {
			defer func() {
				if err := recover(); err != nil {
					base.Zap().Sugar().Errorf("%v", err)
				}
			}()
			err := upgrader.Upgrade(ctx, func(ws *websocket.Conn) {
				defer ws.Close()
				base.Zap().Sugar().Infof("new webclient connected :%s", ws.RemoteAddr().String())

				// 创建WebSocket连接包装器
				wsConn := &WebSocketPeer{Connection: ws}

				// 创建reactor
				reactor, err := network.NewEpollReactor()
				if err != nil {
					base.Zap().Sugar().Errorf("failed to create reactor: %v", err)
					ws.Close()
					return
				}

				// 创建异步peer
				asyncPeer, err := network.NewAsyncClientPeer(wsConn, proc, reactor)
				if err != nil {
					base.Zap().Sugar().Errorf("failed to create async peer: %v", err)
					reactor.Close()
					ws.Close()
					return
				}

				peer := &network.ClientPeer{AsyncClientPeer: asyncPeer}
				event := &network.Event{
					ID:   network.AddEvent,
					Peer: peer,
				}
				proc.EventChan <- event
				defer func() {
					leaveEvent := &network.Event{
						ID:   network.RemoveEvent,
						Peer: peer,
					}
					proc.EventChan <- leaveEvent
				}()
				
				// 启动reactor
				go reactor.Run()
				
				// 使用传统的消息读取循环（为了保持兼容性）
				for {
					mt, content, err := ws.ReadMessage()
					if err != nil {

						//base.Zap().Sugar().Errorf("read websocket message error %v", err)
						return
					}
					hb := content[:8]
					body := content[8:]
					if mt == websocket.BinaryMessage {
						h := network.ReadHead(hb)
						if h.ID > 10000000 || h.Length < 0 || h.Length > 1024*1024*10 {
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

					} else {
						base.Zap().Sugar().Warnf("unsupport message type")
					}
				}
				//peer.ConnectionHandler()
			})
			base.CheckError(err, "websocket")
		} else {
			if notfound != nil {
				notfound(ctx)
			} else {
				ctx.SetStatusCode(http.StatusNotFound)
			}
		}
	}
}
