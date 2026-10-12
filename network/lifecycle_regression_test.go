package network

import (
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMessageReaderRejectsDataAfterRelease(t *testing.T) {
	reader := NewAsyncMessageReader()
	reader.Release()
	reader.Release()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("feeding a released reader panicked: %v", recovered)
		}
	}()
	if messages, err := reader.FeedData([]byte{0}); err == nil || len(messages) != 0 {
		t.Fatalf("released reader accepted input: messages=%d err=%v", len(messages), err)
	}
}

func TestMessageReaderConcurrentRelease(t *testing.T) {
	reader := NewAsyncMessageReader()
	wire := make([]byte, 12)
	binary.LittleEndian.PutUint32(wire[:4], 4)
	binary.LittleEndian.PutUint32(wire[4:8], 1)
	copy(wire[8:], "test")
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for i := 0; i < 1000; i++ {
			messages, err := reader.FeedData(wire)
			for _, message := range messages {
				message.Release()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		reader.Release()
	}()
	close(start)
	workers.Wait()
	reader.Release()
}

func TestPeerCanCloseFromImmediateCallback(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	proc := NewProcessor()
	proc.ImmediateMode = true
	peer := NewWebSocketClientPeer(server, proc)
	defer peer.Close()
	called := false
	proc.AddCallback(1, func(message *Message) {
		called = true
		message.Peer.Close()
	})
	wire := make([]byte, 8)
	binary.LittleEndian.PutUint32(wire[4:], 1)
	done := make(chan error, 1)
	go func() { done <- peer.OnRead(-1, wire) }()
	select {
	case err := <-done:
		if err != nil || !called || peer.GetState() != PeerStateClosed {
			t.Fatalf("callback close failed: called=%v state=%v err=%v", called, peer.GetState(), err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("closing from an immediate callback deadlocked")
	}
}

func TestTCPServerCannotStartAfterStop(t *testing.T) {
	server, err := NewAsyncTCP4Server("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.SetProcessor(NewProcessor())
	server.Stop()
	if err := server.StartAsync(); err == nil {
		t.Fatal("stopped server restarted with a closed listener and reactors")
	}
}

func TestImmediateDispatchRecoversAndContinues(t *testing.T) {
	proc := NewProcessor()
	proc.AddCallback(1, func(*Message) { panic("immediate callback failure") })
	called := false
	proc.AddCallback(2, func(*Message) { called = true })
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("immediate callback panic escaped the processor: %v", recovered)
		}
	}()
	proc.DispatchMessage(&Message{Head: MessageHead{ID: 1}})
	proc.DispatchMessage(&Message{Head: MessageHead{ID: 2}})
	if !called {
		t.Fatal("processor did not continue after a callback panic")
	}
}

func TestTCPStopDuringIngress(t *testing.T) {
	server, err := NewAsyncTCP4Server("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	proc := NewProcessor()
	proc.ImmediateMode = true
	var received atomic.Int64
	proc.AddCallback(1, func(*Message) { received.Add(1) })
	server.SetProcessor(proc)
	if err := server.StartAsync(); err != nil {
		t.Fatal(err)
	}
	wire := make([]byte, 12)
	binary.LittleEndian.PutUint32(wire[:4], 4)
	binary.LittleEndian.PutUint32(wire[4:8], 1)
	copy(wire[8:], "test")
	var senders sync.WaitGroup
	for i := 0; i < 8; i++ {
		conn, err := net.DialTimeout("tcp", server.listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		senders.Add(1)
		go func() {
			defer senders.Done()
			_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
			for {
				if _, err := conn.Write(wire); err != nil {
					return
				}
			}
		}()
	}
	deadline := time.Now().Add(3 * time.Second)
	for received.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no messages reached the real TCP read path")
		}
		time.Sleep(time.Millisecond)
	}
	// Keep admitting new connections while shutdown begins.
	senders.Add(1)
	go func() {
		defer senders.Done()
		for {
			conn, err := net.DialTimeout("tcp", server.listener.Addr().String(), time.Second)
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	done := make(chan struct{})
	go func() {
		server.Stop()
		senders.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not terminate ingress and connection handlers")
	}
	if _, connections := server.GetStats(); connections != 0 {
		t.Fatalf("connections leaked after shutdown: %d", connections)
	}
	server.peers.Range(func(_, _ interface{}) bool {
		t.Error("closed connection remains in the server registry")
		return true
	})
}

func TestPeerRegistrationConcurrentClose(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	reactor, err := NewEpollReactor()
	if err != nil {
		t.Fatal(err)
	}
	defer reactor.Close()
	go reactor.Run()
	for i := 0; i < 50; i++ {
		client, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		server, err := listener.AcceptTCP()
		if err != nil {
			client.Close()
			t.Fatal(err)
		}
		peer, err := NewAsyncClientPeer(server, NewProcessor(), reactor)
		if err != nil {
			client.Close()
			server.Close()
			t.Fatal(err)
		}
		start := make(chan struct{})
		var workers sync.WaitGroup
		workers.Add(2)
		go func() { defer workers.Done(); <-start; _ = peer.StartAsyncIO() }()
		go func() { defer workers.Done(); <-start; peer.Close() }()
		close(start)
		workers.Wait()
		client.Close()
		if peer.GetState() != PeerStateClosed || getConnFromFd(peer.FD) != nil {
			t.Fatal("registration/close race leaked a connection")
		}
		if err := peer.StartAsyncIO(); err == nil {
			t.Fatal("closed peer was registered again")
		}
		reactor.mu.RLock()
		registered := len(reactor.handlers)
		reactor.mu.RUnlock()
		if registered != 0 {
			t.Fatalf("registration/close race left %d reactor handlers", registered)
		}
	}
}
