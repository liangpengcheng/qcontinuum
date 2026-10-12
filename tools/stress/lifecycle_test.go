package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/liangpengcheng/qcontinuum/network"
)

func TestStopClosesPendingWebSocketRegistrationBeforeProcessorExit(t *testing.T) {
	server := NewStressServer(false)
	proc := server.setupProcessor()
	peer := network.NewStreamClientPeer(nil, proc)
	if !proc.TryPostEvent(&network.Event{ID: network.AddEvent, Peer: peer}) {
		t.Fatal("registration failed")
	}
	// Simulate a hijacked WebSocket whose AddEvent is pending at HTTP shutdown.
	server.Stop()
	server.Stop()
	done := make(chan struct{})
	go func() { proc.StartProcess(); close(done) }()
	proc.EventChan <- &network.Event{ID: network.ExitEvent}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish pending business cleanup")
	}
	if peer.GetState() != network.PeerStateClosed || server.peerManager.Count() != 0 || atomic.LoadInt64(&server.stats.ActiveConnections) != 0 {
		t.Fatal("a late registration survived server shutdown")
	}
	if atomic.LoadUint64(&server.stats.TotalConnections) != 1 {
		t.Fatal("registration was not processed exactly once")
	}
}
