package ginhttp

import (
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/liangpengcheng/qcontinuum/base"
	"github.com/liangpengcheng/qcontinuum/network"
)

type WebSocketPeer struct {
	Connection *websocket.Conn
	IOReader   io.Reader
	readMu     sync.Mutex
	writeMu    sync.Mutex
}

func (ws *WebSocketPeer) Write(data []byte) (int, error) {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	if err := ws.Connection.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (ws *WebSocketPeer) Read(data []byte) (int, error) {
	ws.readMu.Lock()
	defer ws.readMu.Unlock()

	for {
		if ws.IOReader == nil {
			messageType, reader, err := ws.Connection.NextReader()
			if err != nil {
				return 0, err
			}
			if messageType != websocket.BinaryMessage {
				_, _ = io.Copy(io.Discard, reader)
				continue
			}
			ws.IOReader = reader
		}

		n, err := ws.IOReader.Read(data)
		if err == io.EOF {
			ws.IOReader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (ws *WebSocketPeer) Close() error         { return ws.Connection.Close() }
func (ws *WebSocketPeer) LocalAddr() net.Addr  { return ws.Connection.LocalAddr() }
func (ws *WebSocketPeer) RemoteAddr() net.Addr { return ws.Connection.RemoteAddr() }
func (ws *WebSocketPeer) SetDeadline(t time.Time) error {
	if err := ws.Connection.SetReadDeadline(t); err != nil {
		return err
	}
	return ws.Connection.SetWriteDeadline(t)
}
func (ws *WebSocketPeer) SetReadDeadline(t time.Time) error { return ws.Connection.SetReadDeadline(t) }
func (ws *WebSocketPeer) SetWriteDeadline(t time.Time) error {
	return ws.Connection.SetWriteDeadline(t)
}

func SetupWebsocket(router *gin.Engine, proc *network.Processor) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	router.GET("/ws", func(c *gin.Context) {
		ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			base.Zap().Sugar().Errorf("websocket upgrade failed: %v", err)
			return
		}

		peer := network.NewWebSocketClientPeer(&WebSocketPeer{Connection: ws}, proc)
		if proc == nil || !proc.TryPostEvent(&network.Event{ID: network.AddEvent, Peer: peer}) {
			peer.Close()
			return
		}
		defer func() {
			peer.Close()
			if !proc.TryPostEvent(&network.Event{ID: network.RemoveEvent, Peer: peer}) {
				base.Zap().Sugar().Warnf("websocket remove event queue full")
			}
		}()

		reader := network.NewAsyncMessageReader()
		defer reader.Release()

		for {
			messageType, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if messageType != websocket.BinaryMessage {
				continue
			}

			messages, err := reader.FeedData(data)
			if err != nil {
				base.Zap().Sugar().Warnf("websocket message parse error: %v", err)
				return
			}
			for i, message := range messages {
				if !network.DeliverZeroCopyMessage(proc, peer, message) {
					for _, remaining := range messages[i+1:] {
						remaining.Release()
					}
					base.Zap().Sugar().Warnf("websocket message queue full")
					return
				}
			}
		}
	})
}
