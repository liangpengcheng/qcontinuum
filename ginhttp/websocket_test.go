package ginhttp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/liangpengcheng/qcontinuum/network"
)

type websocketTestServer struct {
	server   *httptest.Server
	proc     *network.Processor
	added    chan *network.ClientPeer
	removed  chan *network.ClientPeer
	messages chan network.Message
	addCount atomic.Int32
	rmCount  atomic.Int32
}

func newWebsocketTestServer(t *testing.T, setup func(*gin.Engine, *network.Processor) error, listeners ...net.Listener) *websocketTestServer {
	t.Helper()
	s := &websocketTestServer{
		proc:     network.NewProcessor(),
		added:    make(chan *network.ClientPeer, 8),
		removed:  make(chan *network.ClientPeer, 8),
		messages: make(chan network.Message, 32),
	}
	var peers sync.Map
	s.proc.AddEventCallback(network.AddEvent, func(event *network.Event) {
		peers.Store(event.Peer, struct{}{})
		s.addCount.Add(1)
		s.added <- event.Peer
	})
	s.proc.AddEventCallback(network.RemoveEvent, func(event *network.Event) {
		peers.Delete(event.Peer)
		s.rmCount.Add(1)
		s.removed <- event.Peer
	})
	s.proc.AddCallback(1, func(message *network.Message) {
		copyMessage := *message
		copyMessage.Body = append([]byte(nil), message.Body...)
		s.messages <- copyMessage
	})
	router := gin.New()
	if setup == nil {
		SetupWebsocket(router, s.proc)
	} else if err := setup(router, s.proc); err != nil {
		t.Fatal(err)
	}
	s.server = httptest.NewUnstartedServer(router)
	if len(listeners) > 0 {
		s.server.Listener.Close()
		s.server.Listener = listeners[0]
	}
	s.server.Start()
	done := make(chan struct{})
	go func() {
		s.proc.StartProcess()
		close(done)
	}()
	t.Cleanup(func() {
		s.server.Close()
		peers.Range(func(key, _ interface{}) bool {
			key.(*network.ClientPeer).Close()
			return true
		})
		s.proc.TryPostEvent(&network.Event{ID: network.ExitEvent})
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("processor did not stop")
		}
		if s.addCount.Load() != s.rmCount.Load() {
			t.Errorf("unbalanced lifecycle: add=%d remove=%d", s.addCount.Load(), s.rmCount.Load())
		}
	})
	return s
}

func (s *websocketTestServer) dial(t *testing.T, dialers ...*websocket.Dialer) (*websocket.Conn, *network.ClientPeer) {
	t.Helper()
	dialer := websocket.DefaultDialer
	if len(dialers) > 0 {
		dialer = dialers[0]
	}
	conn, response, err := dialer.Dial("ws"+strings.TrimPrefix(s.server.URL, "http")+"/ws", nil)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetWriteDeadline(time.Now().Add(time.Second))
	select {
	case peer := <-s.added:
		return conn, peer
	case <-time.After(time.Second):
		t.Fatal("connection was not registered")
		return nil, nil
	}
}

func (s *websocketTestServer) awaitRemoval(t *testing.T, peer *network.ClientPeer) {
	t.Helper()
	select {
	case removed := <-s.removed:
		if removed != peer || peer.GetState() != network.PeerStateClosed {
			t.Fatal("removal did not describe the closed connection")
		}
	case <-time.After(time.Second):
		t.Fatal("connection was not closed and unregistered")
	}
}

func websocketWire(body []byte) []byte {
	wire := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(wire[:4], uint32(len(body)))
	binary.LittleEndian.PutUint32(wire[4:8], 1)
	copy(wire[8:], body)
	return wire
}

// Writes an actual masked client frame, allowing a test to withhold the final
// continuation frame instead of completing a message through WriteMessage.
func writeWebsocketFrame(conn *websocket.Conn, opcode byte, final bool, payload []byte) error {
	header := []byte{opcode, 0x80}
	if final {
		header[0] |= 0x80
	}
	switch {
	case len(payload) < 126:
		header[1] |= byte(len(payload))
	case len(payload) <= 65535:
		header[1] |= 126
		header = binary.BigEndian.AppendUint16(header, uint16(len(payload)))
	default:
		header[1] |= 127
		header = binary.BigEndian.AppendUint64(header, uint64(len(payload)))
	}
	mask := [4]byte{1, 2, 3, 4}
	header = append(header, mask[:]...)
	for i, b := range payload {
		header = append(header, b^mask[i%4])
	}
	_, err := conn.UnderlyingConn().Write(header)
	return err
}

func TestWebsocketDefaultOriginPolicy(t *testing.T) {
	for _, origin := range []string{"same host", "absent", "https://untrusted.example", "null", "://invalid"} {
		t.Run(origin, func(t *testing.T) {
			s := newWebsocketTestServer(t, nil)
			header := http.Header{}
			if origin == "same host" {
				header.Set("Origin", s.server.URL)
			} else if origin != "absent" {
				header.Set("Origin", origin)
			}
			conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.server.URL, "http")+"/ws", header)
			if conn != nil {
				conn.Close()
			}
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			if origin == "same host" || origin == "absent" {
				if err != nil {
					t.Fatalf("allowed origin was rejected: %v", err)
				}
			} else if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
				t.Fatalf("untrusted origin was not rejected with HTTP 403: err=%v response=%v", err, response)
			}
		})
	}
}

func TestWebsocketRejectsInvalidHeaderBeforeMessageEnd(t *testing.T) {
	for _, length := range []uint32{0xffffffff, 1 << 20, 100<<20 + 1} {
		t.Run(strconv.FormatUint(uint64(length), 10), func(t *testing.T) {
			s := newWebsocketTestServer(t, nil)
			conn, peer := s.dial(t)
			header := websocketWire(nil)
			binary.LittleEndian.PutUint32(header[:4], length)
			if err := writeWebsocketFrame(conn, websocket.BinaryMessage, false, header); err != nil {
				t.Fatal(err)
			}
			s.awaitRemoval(t, peer)
			select {
			case <-s.messages:
				t.Fatal("invalid header reached a business callback")
			default:
			}
		})
	}
}

func newConfiguredWebsocketTestServer(t *testing.T, config WebSocketConfig, listeners ...net.Listener) *websocketTestServer {
	t.Helper()
	return newWebsocketTestServer(t, func(router *gin.Engine, proc *network.Processor) error {
		return SetupWebsocketWithConfig(router, proc, config)
	}, listeners...)
}

func (s *websocketTestServer) awaitMessage(t *testing.T, body []byte) {
	t.Helper()
	select {
	case message := <-s.messages:
		if message.Head.ID != 1 || int(message.Head.Length) != len(body) || !bytes.Equal(message.Body, body) {
			t.Fatalf("wrong protocol message: head=%+v body length=%d", message.Head, len(message.Body))
		}
	case <-time.After(time.Second):
		t.Fatal("complete protocol packet was not delivered")
	}
}

func TestWebsocketConfigRejectsInvalidValuesBeforeRegistration(t *testing.T) {
	for name, config := range map[string]WebSocketConfig{
		"negative limit":     {ReadLimit: -1},
		"short limit":        {ReadLimit: 7},
		"negative read":      {ReadTimeout: -time.Second},
		"negative handshake": {HandshakeTimeout: -time.Second},
		"negative write":     {WriteTimeout: -time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			if err := SetupWebsocketWithConfig(router, network.NewProcessor(), config); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
			if len(router.Routes()) != 0 {
				t.Fatal("invalid configuration registered a route")
			}
			// A failed setup must also leave the router usable for a corrected call.
			if err := SetupWebsocketWithConfig(router, nil, WebSocketConfig{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWebsocketCustomOriginPolicy(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(strconv.FormatBool(allowed), func(t *testing.T) {
			var calls atomic.Int32
			s := newConfiguredWebsocketTestServer(t, WebSocketConfig{
				CheckOrigin: func(r *http.Request) bool {
					calls.Add(1)
					return allowed && r.Header.Get("Origin") == "https://frontend.example"
				},
			})
			conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.server.URL, "http")+"/ws", http.Header{"Origin": {"https://frontend.example"}})
			if conn != nil {
				conn.Close()
			}
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			if calls.Load() != 1 {
				t.Fatalf("Origin policy was called %d times", calls.Load())
			}
			if allowed && err != nil {
				t.Fatalf("explicitly allowed cross-host origin was rejected: %v", err)
			}
			if !allowed && (err == nil || response == nil || response.StatusCode != http.StatusForbidden) {
				t.Fatalf("custom rejection did not produce HTTP 403: %v", err)
			}
		})
	}
}

func TestWebsocketReadLimitBoundaries(t *testing.T) {
	for _, size := range []int{0, 23, 24, 25} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			s := newConfiguredWebsocketTestServer(t, WebSocketConfig{ReadLimit: 32})
			conn, peer := s.dial(t)
			body := bytes.Repeat([]byte{42}, size)
			if err := conn.WriteMessage(websocket.BinaryMessage, websocketWire(body)); err != nil {
				t.Fatal(err)
			}
			if size <= 24 {
				s.awaitMessage(t, body)
			} else {
				conn.SetReadDeadline(time.Now().Add(time.Second))
				_, _, err := conn.ReadMessage()
				if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
					t.Fatalf("oversized WebSocket message did not get close 1009: %v", err)
				}
				s.awaitRemoval(t, peer)
			}
		})
	}
	// The minimum valid limit still admits an empty-body protocol packet.
	s := newConfiguredWebsocketTestServer(t, WebSocketConfig{ReadLimit: 8})
	conn, _ := s.dial(t)
	if err := conn.WriteMessage(websocket.BinaryMessage, websocketWire(nil)); err != nil {
		t.Fatal(err)
	}
	s.awaitMessage(t, nil)
}

func TestWebsocketReadLimitCanBeRaised(t *testing.T) {
	body := bytes.Repeat([]byte{42}, 1<<20)
	s := newConfiguredWebsocketTestServer(t, WebSocketConfig{ReadLimit: int64(len(body) + 8)})
	conn, _ := s.dial(t)
	if err := conn.WriteMessage(websocket.BinaryMessage, websocketWire(body)); err != nil {
		t.Fatal(err)
	}
	s.awaitMessage(t, body)
}

func TestWebsocketNetworkProtocolLimitStillApplies(t *testing.T) {
	s := newConfiguredWebsocketTestServer(t, WebSocketConfig{ReadLimit: 200 << 20})
	conn, peer := s.dial(t)
	header := websocketWire(nil)
	binary.LittleEndian.PutUint32(header[:4], 100<<20+1)
	if err := writeWebsocketFrame(conn, websocket.BinaryMessage, false, header); err != nil {
		t.Fatal(err)
	}
	s.awaitRemoval(t, peer)
}

func TestWebsocketStreamsSplitAndCombinedPackets(t *testing.T) {
	body := bytes.Repeat([]byte{42}, 2*websocketReadChunkSize+17)
	wire := websocketWire(body)
	for _, kind := range []string{"split header", "split body", "continuation frames", "combined packets", "ignored text"} {
		t.Run(kind, func(t *testing.T) {
			s := newWebsocketTestServer(t, nil)
			conn, _ := s.dial(t)
			write := func(data []byte) {
				t.Helper()
				if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "split header":
				write(wire[:3])
				write(wire[3:7])
				write(wire[7:])
			case "split body":
				write(wire[:100])
				write(wire[100:9000])
				write(wire[9000:])
			case "continuation frames":
				if err := writeWebsocketFrame(conn, websocket.BinaryMessage, false, wire[:3]); err != nil {
					t.Fatal(err)
				}
				if err := writeWebsocketFrame(conn, websocket.PingMessage, true, []byte("ping")); err != nil {
					t.Fatal(err)
				}
				if err := writeWebsocketFrame(conn, 0, true, wire[3:]); err != nil {
					t.Fatal(err)
				}
			case "combined packets":
				combined := append(websocketWire(nil), wire...)
				combined = append(combined, websocketWire([]byte("tail"))...)
				write(combined)
				s.awaitMessage(t, nil)
			case "ignored text":
				write(wire[:3])
				if err := conn.WriteMessage(websocket.TextMessage, []byte("ignored")); err != nil {
					t.Fatal(err)
				}
				write(wire[3:])
			}
			s.awaitMessage(t, body)
			if kind == "combined packets" {
				s.awaitMessage(t, []byte("tail"))
			}
		})
	}
}

func TestWebsocketProtocolLimitCannotBeBypassedWithSeparateMessages(t *testing.T) {
	s := newConfiguredWebsocketTestServer(t, WebSocketConfig{ReadLimit: 32})
	allocBefore, freeBefore, _, _ := network.GetBufferStats()
	conn, peer := s.dial(t)
	header := websocketWire(nil)
	binary.LittleEndian.PutUint32(header[:4], 25)
	for _, part := range [][]byte{header[:3], header[3:]} {
		if err := conn.WriteMessage(websocket.BinaryMessage, part); err != nil {
			t.Fatal(err)
		}
	}
	s.awaitRemoval(t, peer)
	allocAfter, freeAfter, _, _ := network.GetBufferStats()
	if allocAfter-allocBefore != freeAfter-freeBefore {
		t.Fatalf("rejected protocol header leaked buffers: alloc=%d free=%d", allocAfter-allocBefore, freeAfter-freeBefore)
	}
}

func TestWebsocketFrameLimitsCloseAndReleaseBuffers(t *testing.T) {
	for _, kind := range []string{"announced huge frame", "binary continuations", "oversized text", "text continuations"} {
		t.Run(kind, func(t *testing.T) {
			s := newConfiguredWebsocketTestServer(t, WebSocketConfig{ReadLimit: 32})
			allocBefore, freeBefore, _, _ := network.GetBufferStats()
			conn, peer := s.dial(t)
			var err error
			switch kind {
			case "announced huge frame":
				// Announce 512 MiB without allocating or sending the payload.
				frame := binary.BigEndian.AppendUint64([]byte{0x82, 0xff}, 512<<20)
				frame = append(frame, 1, 2, 3, 4)
				_, err = conn.UnderlyingConn().Write(frame)
			case "oversized text":
				err = conn.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte{'x'}, 33))
			default:
				opcode := byte(websocket.BinaryMessage)
				first := websocketWire(bytes.Repeat([]byte{42}, 24))[:20]
				if kind == "text continuations" {
					opcode = websocket.TextMessage
				}
				err = writeWebsocketFrame(conn, opcode, false, first)
				if err == nil {
					err = writeWebsocketFrame(conn, 0, true, bytes.Repeat([]byte{42}, 13))
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			conn.SetReadDeadline(time.Now().Add(time.Second))
			_, _, err = conn.ReadMessage()
			if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
				t.Fatalf("oversized frame did not get close 1009: %v", err)
			}
			s.awaitRemoval(t, peer)
			allocAfter, freeAfter, _, _ := network.GetBufferStats()
			if allocAfter-allocBefore != freeAfter-freeBefore {
				t.Fatalf("oversized frame leaked buffers: alloc=%d free=%d", allocAfter-allocBefore, freeAfter-freeBefore)
			}
		})
	}
}

func TestWebsocketReadTimeoutIsNotRenewedByPartialTraffic(t *testing.T) {
	const timeout = 200 * time.Millisecond
	for _, kind := range []string{"idle", "partial header", "partial body", "continuations", "text", "ping", "pong", "empty binary"} {
		t.Run(kind, func(t *testing.T) {
			s := newConfiguredWebsocketTestServer(t, WebSocketConfig{ReadTimeout: timeout})
			allocBefore, freeBefore, _, _ := network.GetBufferStats()
			conn, peer := s.dial(t)
			if kind == "partial body" {
				header := websocketWire(nil)
				binary.LittleEndian.PutUint32(header[:4], 100)
				if err := conn.WriteMessage(websocket.BinaryMessage, header); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "continuations" {
				if err := writeWebsocketFrame(conn, websocket.BinaryMessage, false, nil); err != nil {
					t.Fatal(err)
				}
			}
			ticker := time.NewTicker(timeout / 4)
			defer ticker.Stop()
			// Continued traffic must not keep the connection alive for twice
			// the configured absolute packet deadline.
			deadline := time.NewTimer(2 * timeout)
			defer deadline.Stop()
			for {
				select {
				case removed := <-s.removed:
					if removed != peer || peer.GetState() != network.PeerStateClosed {
						t.Fatal("timeout did not unregister the closed connection")
					}
					allocAfter, freeAfter, _, _ := network.GetBufferStats()
					if allocAfter-allocBefore != freeAfter-freeBefore {
						t.Fatal("timeout leaked the partial packet's buffer")
					}
					return
				case <-deadline.C:
					t.Fatal("partial traffic renewed the read deadline")
				case <-ticker.C:
					var opcode byte
					var payload []byte
					final := true
					switch kind {
					case "idle":
						continue
					case "partial header", "partial body":
						opcode, payload = websocket.BinaryMessage, []byte{0}
					case "continuations":
						final = false
					case "text":
						opcode, payload = websocket.TextMessage, []byte("ignored")
					case "ping":
						opcode = websocket.PingMessage
					case "pong":
						opcode = websocket.PongMessage
					case "empty binary":
						opcode = websocket.BinaryMessage
					}
					// A write racing with the expected close may fail. Removal is
					// the assertion; do not turn that normal race into a failure.
					_ = writeWebsocketFrame(conn, opcode, final, payload)
				}
			}
		})
	}
}

func TestWebsocketCompletePacketsRenewReadTimeout(t *testing.T) {
	const timeout = 400 * time.Millisecond
	s := newConfiguredWebsocketTestServer(t, WebSocketConfig{ReadTimeout: timeout})
	conn, peer := s.dial(t)
	for i := 0; i < 7; i++ {
		if i > 0 {
			time.Sleep(timeout / 4)
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, websocketWire([]byte("alive"))); err != nil {
			t.Fatal(err)
		}
		s.awaitMessage(t, []byte("alive"))
	}
	if peer.GetState() != network.PeerStateConnected {
		t.Fatal("complete packets did not renew the read deadline")
	}
	s.awaitRemoval(t, peer) // Idle waiting after the final packet still expires.
}

// pipeListener runs the real HTTP/Gin upgrade over an unbuffered connection so
// write-timeout tests do not depend on OS loopback buffering or TCP autotuning.
type pipeListener struct {
	connection net.Conn
	accepted   atomic.Bool
	closed     chan struct{}
	closeOnce  sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	if l.accepted.CompareAndSwap(false, true) {
		return l.connection, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}

func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
		l.connection.Close()
	})
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
}

func TestWebsocketHandshakeWriteTimeoutDoesNotRegisterPeer(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	t.Cleanup(func() { clientSide.Close() })
	listener := &pipeListener{connection: serverSide, closed: make(chan struct{})}
	handlerDone := make(chan struct{})
	s := newWebsocketTestServer(t, func(router *gin.Engine, proc *network.Processor) error {
		router.Use(func(c *gin.Context) {
			defer close(handlerDone)
			c.Next()
		})
		return SetupWebsocketWithConfig(router, proc, WebSocketConfig{HandshakeTimeout: 50 * time.Millisecond})
	}, listener)
	clientSide.SetWriteDeadline(time.Now().Add(time.Second))
	// Send a valid upgrade request, then refuse to read its response. With an
	// unbuffered connection, only HandshakeTimeout can release the handler.
	request := "GET /ws HTTP/1.1\r\nHost: 127.0.0.1:1\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"
	if _, err := clientSide.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("blocked upgrade response did not time out")
	}
	if s.addCount.Load() != 0 || s.rmCount.Load() != 0 {
		t.Fatal("failed handshake produced lifecycle events")
	}
}

func TestWebsocketWriteTimeoutClosesAndUnregisters(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	t.Cleanup(func() { clientSide.Close() })
	listener := &pipeListener{connection: serverSide, closed: make(chan struct{})}
	s := newConfiguredWebsocketTestServer(t, WebSocketConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 50 * time.Millisecond,
	}, listener)
	_, peer := s.dial(t, &websocket.Dialer{
		NetDial:          func(_, _ string) (net.Conn, error) { return clientSide, nil },
		HandshakeTimeout: time.Second,
	})
	done := make(chan error, 1)
	go func() {
		// The client stops reading after the handshake. net.Pipe cannot buffer
		// the response, so the configured deadline must unblock this write.
		_, err := peer.Connection.Write(websocketWire([]byte("reply")))
		done <- err
	}()
	select {
	case err := <-done:
		var timeoutError net.Error
		if !errors.As(err, &timeoutError) || !timeoutError.Timeout() {
			t.Fatalf("blocked write did not time out: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write did not respect the configured timeout")
	}
	s.awaitRemoval(t, peer)
}

func TestWebsocketConcurrentWritesAndDeadlineUpdates(t *testing.T) {
	s := newWebsocketTestServer(t, nil)
	conn, peer := s.dial(t)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	readDone := make(chan error, 1)
	go func() {
		for i := 0; i < 64; i++ {
			kind, _, err := conn.ReadMessage()
			if err != nil {
				readDone <- err
				return
			}
			if kind != websocket.BinaryMessage {
				readDone <- fmt.Errorf("wrong response type: %d", kind)
				return
			}
		}
		readDone <- nil
	}()
	var writers sync.WaitGroup
	writeErrors := make(chan error, 5)
	for i := 0; i < 4; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for j := 0; j < 16; j++ {
				if _, err := peer.Connection.Write(websocketWire([]byte("reply"))); err != nil {
					writeErrors <- err
					return
				}
			}
		}()
	}
	writers.Add(1)
	go func() {
		defer writers.Done()
		for i := 0; i < 64; i++ {
			if err := peer.Connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				writeErrors <- err
				return
			}
		}
	}()
	writers.Wait()
	close(writeErrors)
	for err := range writeErrors {
		t.Error(err)
	}
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
}
