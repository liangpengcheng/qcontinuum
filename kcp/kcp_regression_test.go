package kcp

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liangpengcheng/qcontinuum/network"
	_kcp "github.com/xtaci/kcp-go/v5"
)

func newTestPeer(t *testing.T) *KCPPeer {
	t.Helper()
	conn, err := _kcp.DialWithOptions("127.0.0.1:9", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	peer := NewKCPPeer(conn, network.NewProcessor())
	t.Cleanup(peer.Close)
	return peer
}

func TestClientPeerSharesIdentity(t *testing.T) {
	peer := newTestPeer(t)
	client := peer.ToClientPeer()
	client.ID = 12345
	client.Flag = 7
	if again := peer.ToClientPeer(); again != client || again.ID != 12345 || again.Flag != 7 {
		t.Fatalf("conversion overwrote business identity: id=%d flag=%d", again.ID, again.Flag)
	}
	if peer.ID != client.ID || peer.Flag != client.Flag {
		t.Fatal("KCP and client views must share the same identity")
	}
	peer.ID = 67890
	peer.Flag = 9
	if client.ID != 67890 || client.Flag != 9 {
		t.Fatal("direct KCP identity updates must reach the client view")
	}
}

func TestClientPeerSharesCloseState(t *testing.T) {
	for _, closeClient := range []bool{false, true} {
		t.Run(map[bool]string{false: "KCP", true: "Client"}[closeClient], func(t *testing.T) {
			peer := newTestPeer(t)
			client := peer.ToClientPeer()
			if closeClient {
				client.Close()
			} else {
				peer.Close()
			}
			if peer.GetState() != KCPPeerStateClosed || client.GetState() != network.PeerStateClosed {
				t.Fatalf("close state diverged: KCP=%v client=%v", peer.GetState(), client.GetState())
			}
			if err := client.SendMessageBuffer([]byte("closed")); err == nil {
				t.Fatal("closed client view still accepts sends")
			}
		})
	}
}

func TestKCPServerCannotStartAfterStop(t *testing.T) {
	server, err := NewAsyncKCPServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.SetProcessor(network.NewProcessor())
	server.Stop()
	if err := server.StartAsync(); err == nil {
		t.Fatal("stopped KCP server restarted with a closed listener")
	}
}

func TestKCPIdentitySurvivesMessageDelivery(t *testing.T) {
	for _, immediate := range []bool{false, true} {
		t.Run(fmt.Sprintf("immediate=%v", immediate), func(t *testing.T) {
			proc := network.NewProcessor()
			proc.ImmediateMode = immediate
			added := make(chan *network.ClientPeer, 1)
			proc.AddEventCallback(network.AddEvent, func(event *network.Event) { added <- event.Peer })
			proc.AddCallback(1, func(message *network.Message) {
				message.Peer.ID = 12345
				message.Peer.Flag = 7
				_ = message.Peer.TransmitMsg(message)
			})
			proc.AddCallback(2, func(message *network.Message) {
				if message.Peer.ID != 12345 || message.Peer.Flag != 7 {
					t.Errorf("message delivery lost identity: id=%d flag=%d", message.Peer.ID, message.Peer.Flag)
				}
				_ = message.Peer.TransmitMsg(message)
			})
			processed := make(chan struct{})
			go func() { proc.StartProcess(); close(processed) }()
			server, err := NewAsyncKCPServer("127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			server.SetProcessor(proc)
			t.Cleanup(func() {
				server.Stop()
				proc.EventChan <- &network.Event{ID: network.ExitEvent}
				<-processed
			})
			if err := server.StartAsync(); err != nil {
				t.Fatal(err)
			}
			conn, err := _kcp.DialWithOptions(server.listener.Addr().String(), nil, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetNoDelay(1, 10, 2, 1)
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			// Mutate an unrelated registration while real messages are being
			// dispatched, so immediate delivery must use the Processor map lock.
			stopRegistration := make(chan struct{})
			registrationDone := make(chan struct{})
			go func() {
				defer close(registrationDone)
				for {
					select {
					case <-stopRegistration:
						return
					default:
						proc.AddCallback(99, func(*network.Message) {})
						proc.RemoveCallback(99)
					}
				}
			}()
			t.Cleanup(func() { close(stopRegistration); <-registrationDone })
			// First write establishes the KCP session; its reply also orders the
			// identity update before the second message in both processing modes.
			for _, id := range []uint32{1, 2} {
				wire := make([]byte, 12)
				binary.LittleEndian.PutUint32(wire[:4], 4)
				binary.LittleEndian.PutUint32(wire[4:8], id)
				copy(wire[8:], "echo")
				if _, err := conn.Write(wire); err != nil {
					t.Fatal(err)
				}
				head, body, err := network.ReadMessage(conn)
				if err != nil || head.ID != int32(id) || string(body) != "echo" {
					t.Fatalf("echo failed: head=%+v body=%q err=%v", head, body, err)
				}
			}
			select {
			case peer := <-added:
				peer.Close()
			case <-time.After(time.Second):
				t.Fatal("connection event was not delivered")
			}
			server.Stop()
			if _, active := server.GetStats(); active != 0 {
				t.Fatalf("KCP connections leaked: %d", active)
			}
		})
	}
}

func TestKCPStopDuringIngress(t *testing.T) {
	server, err := NewAsyncKCPServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	proc := network.NewProcessor()
	proc.ImmediateMode = true
	var received atomic.Int64
	proc.AddCallback(1, func(*network.Message) { received.Add(1) })
	server.SetProcessor(proc)
	if err := server.StartAsync(); err != nil {
		t.Fatal(err)
	}
	wire := make([]byte, 12)
	binary.LittleEndian.PutUint32(wire[:4], 4)
	binary.LittleEndian.PutUint32(wire[4:8], 1)
	copy(wire[8:], "test")
	stopIngress := make(chan struct{})
	var senders sync.WaitGroup
	t.Cleanup(func() { close(stopIngress); senders.Wait() })
	for i := 0; i < 8; i++ {
		conn, err := _kcp.DialWithOptions(server.listener.Addr().String(), nil, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetNoDelay(1, 10, 2, 1)
		_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		senders.Add(1)
		go func() {
			defer senders.Done()
			defer conn.Close()
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stopIngress:
					return
				case <-ticker.C:
					if _, err := conn.Write(wire); err != nil {
						return
					}
				}
			}
		}()
	}
	deadline := time.Now().Add(3 * time.Second)
	for received.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no messages reached the real KCP read path")
		}
		time.Sleep(time.Millisecond)
	}
	// Admit new sessions while existing connections are still sending data.
	senders.Add(1)
	go func() {
		defer senders.Done()
		for {
			select {
			case <-stopIngress:
				return
			default:
			}
			conn, err := _kcp.DialWithOptions(server.listener.Addr().String(), nil, 0, 0)
			if err != nil {
				return
			}
			_, _ = conn.Write(wire)
			conn.Close()
		}
	}()
	done := make(chan struct{})
	go func() { server.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("KCP shutdown did not finish during ingress and admission")
	}
	if _, active := server.GetStats(); active != 0 || server.IsRunning() {
		t.Fatalf("KCP shutdown leaked connections: %d", active)
	}
	server.peers.Range(func(_, _ interface{}) bool {
		t.Error("closed KCP connection remains registered")
		return true
	})
}
