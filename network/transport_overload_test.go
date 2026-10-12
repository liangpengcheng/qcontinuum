package network_test

import (
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/liangpengcheng/qcontinuum/ginhttp"
	"github.com/liangpengcheng/qcontinuum/kcp"
	"github.com/liangpengcheng/qcontinuum/network"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

type overloadTransport struct {
	write func([]byte) error
	stop  func()
}

func startOverloadTransport(t *testing.T, kind string, proc *network.Processor) overloadTransport {
	t.Helper()
	switch kind {
	case "TCP":
		server, err := network.NewTCP4Server("127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		server.SetProcessor(proc)
		if err := server.StartAsync(); err != nil {
			server.Stop()
			t.Fatal(err)
		}
		conn, err := net.DialTimeout("tcp4", server.Listener.Addr().String(), time.Second)
		if err != nil {
			server.Stop()
			t.Fatal(err)
		}
		return overloadTransport{
			write: func(wire []byte) error { _, err := conn.Write(wire); return err },
			stop: func() {
				conn.Close()
				server.Stop()
				if _, active := server.GetStats(); active != 0 {
					t.Errorf("TCP transport leaked %d connections", active)
				}
			},
		}
	case "KCP":
		server, err := kcp.NewKCPServer("127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		server.SetProcessor(proc)
		if err := server.StartAsync(); err != nil {
			server.Stop()
			t.Fatal(err)
		}
		conn, err := kcpgo.DialWithOptions(server.Listener.Addr().String(), nil, 0, 0)
		if err != nil {
			server.Stop()
			t.Fatal(err)
		}
		conn.SetNoDelay(1, 10, 2, 1)
		conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		return overloadTransport{
			write: func(wire []byte) error { _, err := conn.Write(wire); return err },
			stop: func() {
				conn.Close()
				server.Stop()
				if _, active := server.GetStats(); active != 0 {
					t.Errorf("KCP transport leaked %d connections", active)
				}
			},
		}
	default:
		var httpServer *httptest.Server
		if kind == "WebSocket" {
			// NewWebSocket registers with DefaultServeMux; isolate its handler.
			original := http.DefaultServeMux
			mux := http.NewServeMux()
			http.DefaultServeMux = mux
			network.NewWebSocket("/ws", proc)
			http.DefaultServeMux = original
			httpServer = httptest.NewServer(mux)
		} else {
			router := gin.New()
			ginhttp.SetupWebsocket(router, proc)
			httpServer = httptest.NewServer(router)
		}
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+"/ws", http.Header{"Origin": {httpServer.URL}})
		if err != nil {
			httpServer.Close()
			t.Fatal(err)
		}
		conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		return overloadTransport{
			write: func(wire []byte) error { return conn.WriteMessage(websocket.BinaryMessage, wire) },
			stop:  func() { conn.Close(); httpServer.Close() },
		}
	}
}

func overloadWire() []byte {
	wire := make([]byte, 12)
	binary.LittleEndian.PutUint32(wire[:4], 4)
	binary.LittleEndian.PutUint32(wire[4:8], 1)
	copy(wire[8:], "test")
	return wire
}

func TestTransportOverloadClosesAndUnregisters(t *testing.T) {
	testTransportCleanup(t, true)
}

func TestTransportStopUnregisters(t *testing.T) {
	testTransportCleanup(t, false)
}

func testTransportCleanup(t *testing.T, overload bool) {
	for _, kind := range []string{"TCP", "KCP", "WebSocket", "GinWebSocket"} {
		t.Run(kind, func(t *testing.T) {
			proc := network.NewProcessor()
			added := make(chan *network.ClientPeer, 1)
			primed := make(chan struct{}, 1)
			var messageCalls atomic.Int32
			records := make(map[*network.ClientPeer]bool)
			var addCalls, removeCalls int
			proc.AddEventCallback(network.AddEvent, func(event *network.Event) {
				addCalls++
				records[event.Peer] = true
				added <- event.Peer
			})
			proc.AddEventCallback(network.RemoveEvent, func(event *network.Event) {
				removeCalls++
				if !records[event.Peer] {
					t.Error("transport removed an unregistered connection")
				}
				delete(records, event.Peer)
			})
			proc.AddCallback(1, func(*network.Message) {
				messageCalls.Add(1)
				primed <- struct{}{}
			})
			done := make(chan struct{})
			transport := startOverloadTransport(t, kind, proc)
			go func() { proc.StartProcess(); close(done) }()
			blocked, release := make(chan struct{}), make(chan struct{})
			var releaseOnce, finishOnce sync.Once
			finish := func() {
				finishOnce.Do(func() {
					transport.stop()
					releaseOnce.Do(func() { close(release) })
					go func() { proc.EventChan <- &network.Event{ID: network.ExitEvent} }()
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Error("transport cleanup did not drain")
					}
				})
			}
			t.Cleanup(finish)
			wire := overloadWire()
			// A first message creates the KCP session and proves real read delivery.
			if err := transport.write(wire); err != nil {
				t.Fatal(err)
			}
			var peer *network.ClientPeer
			select {
			case peer = <-added:
			case <-time.After(3 * time.Second):
				t.Fatal("real transport did not register")
			}
			select {
			case <-primed:
			case <-time.After(3 * time.Second):
				t.Fatal("real transport did not deliver the first message")
			}
			proc.FuncChan <- func() { close(blocked); <-release }
			select {
			case <-blocked:
			case <-time.After(3 * time.Second):
				t.Fatal("processor barrier did not run")
			}
			if overload {
				for len(proc.MessageChan) < cap(proc.MessageChan) {
					proc.MessageChan <- &network.Message{Peer: peer, Head: network.MessageHead{ID: 1}}
				}
			}
			for len(proc.EventChan) < cap(proc.EventChan) {
				proc.EventChan <- &network.Event{ID: 99}
			}
			if overload {
				if err := transport.write(append(wire, wire...)); err != nil {
					t.Fatal(err)
				}
			} else {
				transport.stop()
			}
			deadline := time.Now().Add(3 * time.Second)
			for peer.GetState() != network.PeerStateClosed {
				if time.Now().After(deadline) {
					t.Fatal("overload or stop did not close the real transport")
				}
				time.Sleep(time.Millisecond)
			}
			peer.Close() // A second close cannot produce a second business removal.
			finish()
			if addCalls != 1 || removeCalls != 1 || len(records) != 0 || messageCalls.Load() != 1 {
				t.Fatalf("overload cleanup failed: add=%d remove=%d records=%d messages=%d", addCalls, removeCalls, len(records), messageCalls.Load())
			}
		})
	}
}
