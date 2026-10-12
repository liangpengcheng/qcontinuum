package network

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/liangpengcheng/qcontinuum/base"
	"golang.org/x/net/websocket"
)

type WebSocketPeer struct {
	Connection *websocket.Conn
	writeMu    sync.Mutex
}

func (ws *WebSocketPeer) Write(data []byte) (int, error) {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	if err := websocket.Message.Send(ws.Connection, data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (ws *WebSocketPeer) Read(data []byte) (int, error) {
	return ws.Connection.Read(data)
}

func (ws *WebSocketPeer) Close() error                      { return ws.Connection.Close() }
func (ws *WebSocketPeer) LocalAddr() net.Addr               { return ws.Connection.LocalAddr() }
func (ws *WebSocketPeer) RemoteAddr() net.Addr              { return ws.Connection.RemoteAddr() }
func (ws *WebSocketPeer) SetDeadline(t time.Time) error     { return ws.Connection.SetDeadline(t) }
func (ws *WebSocketPeer) SetReadDeadline(t time.Time) error { return ws.Connection.SetReadDeadline(t) }
func (ws *WebSocketPeer) SetWriteDeadline(t time.Time) error {
	return ws.Connection.SetWriteDeadline(t)
}

func NewWebSocket(path string, proc *Processor) {
	http.Handle(path, websocket.Handler(func(ws *websocket.Conn) {
		if proc == nil {
			_ = ws.Close()
			return
		}

		base.Zap().Sugar().Infof("new webclient connected: %s", ws.RemoteAddr())
		peer := NewWebSocketClientPeer(&WebSocketPeer{Connection: ws}, proc)
		if !proc.TryPostEvent(&Event{ID: AddEvent, Peer: peer}) {
			peer.Close()
			return
		}
		defer peer.Close()

		reader := NewAsyncMessageReader()
		defer reader.Release()

		for {
			var data []byte
			if err := websocket.Message.Receive(ws, &data); err != nil {
				return
			}
			messages, err := reader.FeedData(data)
			if err != nil {
				base.Zap().Sugar().Warnf("websocket message parse error: %v", err)
				return
			}
			for i, message := range messages {
				if !DeliverZeroCopyMessage(proc, peer, message) {
					for _, remaining := range messages[i+1:] {
						remaining.Release()
					}
					base.Zap().Sugar().Warnf("websocket message queue full")
					return
				}
			}
		}
	}))
}
