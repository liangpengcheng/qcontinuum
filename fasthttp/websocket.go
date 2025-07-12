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

				// 为WebSocket创建专用的peer
				peer := network.NewWebSocketClientPeer(wsConn, proc)

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

				// 使用零拷贝消息读取器处理WebSocket消息
				reader := network.NewAsyncMessageReader()
				defer reader.Release()
				
				for {
					mt, content, err := ws.ReadMessage()
					if err != nil {
						//base.Zap().Sugar().Errorf("read websocket message error %v", err)
						return
					}
					
					if len(content) < 8 {
						base.Zap().Sugar().Warnf("message too short: %d bytes", len(content))
						continue
					}
					
					if mt == websocket.BinaryMessage {
						// 直接将WebSocket消息投递给零拷贝读取器
						messages, err := reader.FeedData(content)
						if err != nil {
							base.Zap().Sugar().Warnf("message parse error: %v", err)
							continue
						}
						
						// 处理解析出的零拷贝消息
						for _, zcMsg := range messages {
							msg := &network.Message{
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
