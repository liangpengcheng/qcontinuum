package ginhttp

import (
	"fmt"
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

const (
	websocketMessageHeaderSize = 8
	websocketReadChunkSize     = 8 * 1024
)

// WebSocketConfig controls admission and resource limits for the Gin /ws route.
// Numeric zero values use the defaults below; negative values are invalid.
type WebSocketConfig struct {
	// CheckOrigin defaults to Gorilla's host check when nil. Requests without
	// Origin are allowed; cross-host browser clients need an explicit policy.
	CheckOrigin func(*http.Request) bool
	// ReadLimit defaults to 1 MiB and limits both a WebSocket message's payload
	// (including all continuation frames) and a protocol packet including its
	// 8-byte header. The existing network protocol limit also still applies.
	ReadLimit int64
	// ReadTimeout defaults to 60 seconds for each complete protocol packet,
	// including idle waiting. Partial data and control frames do not renew it.
	ReadTimeout time.Duration
	// HandshakeTimeout defaults to 10 seconds for writing the upgrade response.
	// The HTTP server remains responsible for its own request/header timeouts.
	HandshakeTimeout time.Duration
	// WriteTimeout defaults to 10 seconds for each binary-message write.
	WriteTimeout time.Duration
}

func (config WebSocketConfig) withDefaults() (WebSocketConfig, error) {
	if config.ReadLimit < 0 || (config.ReadLimit > 0 && config.ReadLimit < websocketMessageHeaderSize) {
		return WebSocketConfig{}, fmt.Errorf("websocket ReadLimit must be zero or at least %d bytes", websocketMessageHeaderSize)
	}
	if config.ReadLimit == 0 {
		config.ReadLimit = 1 << 20
	}
	for _, timeout := range []struct {
		name         string
		value        *time.Duration
		defaultValue time.Duration
	}{
		{"ReadTimeout", &config.ReadTimeout, 60 * time.Second},
		{"HandshakeTimeout", &config.HandshakeTimeout, 10 * time.Second},
		{"WriteTimeout", &config.WriteTimeout, 10 * time.Second},
	} {
		if *timeout.value < 0 {
			return WebSocketConfig{}, fmt.Errorf("websocket %s must not be negative", timeout.name)
		}
		if *timeout.value == 0 {
			*timeout.value = timeout.defaultValue
		}
	}
	return config, nil
}

type WebSocketPeer struct {
	Connection   *websocket.Conn
	IOReader     io.Reader
	readMu       sync.Mutex
	writeMu      sync.Mutex
	writeTimeout time.Duration
}

func (ws *WebSocketPeer) Write(data []byte) (int, error) {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	if ws.writeTimeout > 0 {
		if err := ws.Connection.SetWriteDeadline(time.Now().Add(ws.writeTimeout)); err != nil {
			_ = ws.Close()
			return 0, err
		}
	}
	if err := ws.Connection.WriteMessage(websocket.BinaryMessage, data); err != nil {
		_ = ws.Close()
		return 0, err
	}
	return len(data), nil
}

func (ws *WebSocketPeer) Read(data []byte) (int, error) {
	ws.readMu.Lock()
	defer ws.readMu.Unlock()
	if len(data) == 0 {
		return 0, nil
	}

	for {
		if ws.IOReader == nil {
			messageType, reader, err := ws.Connection.NextReader()
			if err != nil {
				return 0, err
			}
			if messageType != websocket.BinaryMessage {
				if _, err := io.Copy(io.Discard, reader); err != nil {
					return 0, err
				}
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
	return ws.SetWriteDeadline(t)
}
func (ws *WebSocketPeer) SetReadDeadline(t time.Time) error { return ws.Connection.SetReadDeadline(t) }
func (ws *WebSocketPeer) SetWriteDeadline(t time.Time) error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	return ws.Connection.SetWriteDeadline(t)
}

// SetupWebsocket registers /ws with the safe defaults in WebSocketConfig.
func SetupWebsocket(router *gin.Engine, proc *network.Processor) {
	if err := SetupWebsocketWithConfig(router, proc, WebSocketConfig{}); err != nil {
		panic(err) // The zero configuration always has valid defaults.
	}
}

// SetupWebsocketWithConfig validates config before registering /ws. The policy
// callback may be called concurrently and must be safe for concurrent use.
func SetupWebsocketWithConfig(router *gin.Engine, proc *network.Processor, config WebSocketConfig) error {
	config, err := config.withDefaults()
	if err != nil {
		return err
	}
	upgrader := websocket.Upgrader{
		CheckOrigin:      config.CheckOrigin,
		HandshakeTimeout: config.HandshakeTimeout,
	}

	router.GET("/ws", func(c *gin.Context) {
		ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			base.Zap().Sugar().Errorf("websocket upgrade failed: %v", err)
			return
		}

		ws.SetReadLimit(config.ReadLimit)
		transport := &WebSocketPeer{Connection: ws, writeTimeout: config.WriteTimeout}
		peer := network.NewWebSocketClientPeer(transport, proc)
		if proc == nil || !proc.TryPostEvent(&network.Event{ID: network.AddEvent, Peer: peer}) {
			peer.Close()
			return
		}
		defer peer.Close()

		reader := network.NewAsyncMessageReader()
		defer reader.Release()

		feed := func(data []byte) bool {
			messages, err := reader.FeedData(data)
			if err != nil {
				base.Zap().Sugar().Warnf("websocket message parse error: %v", err)
				return false
			}
			for i, message := range messages {
				if !network.DeliverZeroCopyMessage(proc, peer, message) {
					for _, remaining := range messages[i+1:] {
						remaining.Release()
					}
					base.Zap().Sugar().Warnf("websocket message queue full")
					return false
				}
			}
			return true
		}

		var header [websocketMessageHeaderSize]byte
		var chunk [websocketReadChunkSize]byte
		for {
			// Keep one absolute deadline across the entire protocol packet, even
			// if it spans several WebSocket messages or contains control frames.
			if err := transport.SetReadDeadline(time.Now().Add(config.ReadTimeout)); err != nil {
				return
			}
			if _, err := io.ReadFull(transport, header[:]); err != nil {
				return
			}
			head, err := network.ReadHeadFromBuffer(header[:])
			if err != nil || head.Length < 0 || int64(head.Length) > config.ReadLimit-websocketMessageHeaderSize {
				base.Zap().Sugar().Warn("websocket invalid protocol message length")
				return
			}
			// Feed the header alone so the network parser also validates its own
			// protocol limit before any message body is read or buffered.
			if !feed(header[:]) {
				return
			}
			for remaining := int64(head.Length); remaining > 0; {
				size := int64(len(chunk))
				if remaining < size {
					size = remaining
				}
				if _, err := io.ReadFull(transport, chunk[:size]); err != nil {
					return
				}
				if !feed(chunk[:size]) {
					return
				}
				remaining -= size
			}
		}
	})
	return nil
}
