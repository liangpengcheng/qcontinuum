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

		// 为WebSocket创建专用的peer
		peer := network.NewWebSocketClientPeer(wsConnection, proc)

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

		// 使用零拷贝消息读取器
		reader := network.NewAsyncMessageReader()
		defer reader.Release()

		for {
			_, wsConnection.IOReader, err = ws.NextReader()
			if err != nil {
				base.Zap().Sugar().Errorf("websocket read error: %v", err)
				return
			}

			// 使用缓冲区池读取数据
			buffer := network.GetBuffer() // 从池中获取缓冲区

			// 扩展缓冲区以适应可能的大消息
			if buffer.Cap() < 64*1024 {
				if err := buffer.Grow(64*1024 - buffer.Cap()); err != nil {
					buffer.Release()
					base.Zap().Sugar().Warnf("gin websocket buffer grow failed: %v", err)
					return
				}
			}

			// 读取WebSocket消息到缓冲区
			n, err := wsConnection.IOReader.Read(buffer.Data())
			if err != nil && err != io.EOF {
				buffer.Release()
				base.Zap().Sugar().Errorf("websocket read data error: %v", err)
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
		}
	})
}
