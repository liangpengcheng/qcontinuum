package network

import (
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func finishTestProcessor(t *testing.T, proc *Processor) {
	t.Helper()
	done := make(chan struct{})
	go func() { proc.StartProcess(); close(done) }()
	proc.EventChan <- &Event{ID: ExitEvent}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("processor did not finish lifecycle cleanup")
	}
}

func TestRemoveEventSurvivesFullOrdinaryQueue(t *testing.T) {
	proc := NewProcessor()
	peer := NewStreamClientPeer(nil, proc)
	var added, removed int
	records := make(map[*ClientPeer]bool)
	proc.AddEventCallback(AddEvent, func(event *Event) {
		added++
		records[event.Peer] = true
	})
	proc.AddEventCallback(RemoveEvent, func(event *Event) {
		removed++
		if !records[event.Peer] {
			t.Error("remove ran before add, or ran twice")
		}
		delete(records, event.Peer)
	})
	if !proc.TryPostEvent(&Event{ID: AddEvent, Peer: peer}) {
		t.Fatal("registration failed")
	}
	for len(proc.EventChan) < cap(proc.EventChan) {
		proc.EventChan <- &Event{ID: 99}
	}
	peer.Close()
	finishTestProcessor(t, proc)
	if added != 1 || removed != 1 || len(records) != 0 {
		t.Fatalf("business record leaked: add=%d remove=%d records=%d", added, removed, len(records))
	}
}

func TestRegistrationLimitStillAllowsEveryRemoval(t *testing.T) {
	proc := NewProcessor()
	var added, removed int
	proc.AddEventCallback(AddEvent, func(*Event) { added++ })
	proc.AddEventCallback(RemoveEvent, func(*Event) { removed++ })
	peers := make([]*ClientPeer, 1024)
	for i := range peers {
		peers[i] = NewStreamClientPeer(nil, proc)
		peers[i].ID = 7 // Deduplication must use connection identity, not business ID.
		if !proc.TryPostEvent(&Event{ID: AddEvent, Peer: peers[i]}) {
			t.Fatalf("registration %d was rejected prematurely", i)
		}
	}
	rejected := NewStreamClientPeer(nil, proc)
	if proc.TryPostEvent(&Event{ID: AddEvent, Peer: rejected}) {
		t.Fatal("registration exceeded the pending lifecycle limit")
	}
	rejected.Close()
	for _, peer := range peers {
		peer.Close()
	}
	finishTestProcessor(t, proc)
	if added != len(peers) || removed != len(peers) {
		t.Fatalf("accepted/rejected registrations were not paired correctly: add=%d remove=%d", added, removed)
	}
}

func TestConcurrentClosePostsOneRemovalBeforeOnClosed(t *testing.T) {
	proc := NewProcessor()
	peer := NewStreamClientPeer(nil, proc)
	if !proc.TryPostEvent(&Event{ID: AddEvent, Peer: peer}) {
		t.Fatal("registration failed")
	}
	var removes atomic.Int32
	proc.AddEventCallback(RemoveEvent, func(*Event) { removes.Add(1) })
	// Start processing and request exit as soon as the transport reports closure.
	done := make(chan struct{})
	peer.onClosed = func() {
		go func() { proc.StartProcess(); close(done) }()
		proc.EventChan <- &Event{ID: ExitEvent}
	}
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			peer.Close()
			peer.OnClose(-1)
		}()
	}
	workers.Wait()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent close blocked cleanup")
	}
	if removes.Load() != 1 {
		t.Fatalf("expected one removal before exit, got %d", removes.Load())
	}
}

func TestClosedPeerQueuedMessagesAreSkipped(t *testing.T) {
	proc := NewProcessor()
	peer := NewStreamClientPeer(nil, proc)
	called := false
	proc.AddCallback(1, func(*Message) { called = true })
	peer.Close()
	proc.DispatchMessage(&Message{Peer: peer, Head: MessageHead{ID: 1}})
	if called {
		t.Fatal("a closed connection still executed a business message")
	}
}

func TestDeliveryUsesRedirectedProcessorAndReleasesBuffer(t *testing.T) {
	old := NewProcessor()
	current := NewProcessor()
	current.ImmediateMode = true
	peer := NewStreamClientPeer(nil, old)
	defer peer.Close()
	called := false
	current.AddCallback(1, func(*Message) { called = true })
	peer.Redirect(current)
	reader := NewAsyncMessageReader()
	defer reader.Release()
	wire := make([]byte, 8)
	binary.LittleEndian.PutUint32(wire[4:], 1)
	messages, err := reader.FeedData(wire)
	if err != nil {
		t.Fatal(err)
	}
	buffer := messages[0].Buffer
	if !DeliverZeroCopyMessage(old, peer, messages[0]) || !called {
		t.Fatal("delivery used the captured processor instead of the current owner")
	}
	if refs := atomic.LoadInt32(&buffer.refs); refs != 0 {
		t.Fatalf("delivery leaked its buffer: refs=%d", refs)
	}
}

func awaitTestCondition(t *testing.T, description string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal(description)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestExitWaitsForImmediateSelfClose(t *testing.T) {
	proc := NewProcessor()
	peer := NewStreamClientPeer(nil, proc)
	added := make(chan struct{})
	proc.AddEventCallback(AddEvent, func(*Event) { close(added) })
	if !proc.TryPostEvent(&Event{ID: AddEvent, Peer: peer}) {
		t.Fatal("registration failed")
	}
	callbackClosed := make(chan struct{})
	callbackDone := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	proc.AddCallback(1, func(*Message) {
		peer.Close()
		close(callbackClosed)
		<-release
		close(callbackDone)
	})
	var removes atomic.Int32
	proc.AddEventCallback(RemoveEvent, func(*Event) {
		select {
		case <-callbackDone:
		default:
			t.Error("remove ran before the in-flight callback completed")
		}
		removes.Add(1)
	})
	done := make(chan struct{})
	go func() { proc.StartProcess(); close(done) }()
	select {
	case <-added:
	case <-time.After(3 * time.Second):
		t.Fatal("registration was not dispatched")
	}
	dispatched := make(chan struct{})
	go func() {
		proc.DispatchMessage(&Message{Peer: peer, Head: MessageHead{ID: 1}})
		close(dispatched)
	}()
	select {
	case <-callbackClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("closing inside an immediate callback deadlocked")
	}
	proc.EventChan <- &Event{ID: ExitEvent}
	awaitTestCondition(t, "processor did not begin draining", proc.isStopping)
	select {
	case <-done:
		t.Fatal("processor exited with an in-flight callback")
	default:
	}
	if proc.TryEnqueueMessage(&Message{Head: MessageHead{ID: 1}}) || proc.TryPostEvent(&Event{ID: AddEvent, Peer: NewStreamClientPeer(nil, proc)}) {
		t.Error("draining processor accepted new work")
	}
	unblock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("callback completion did not wake the draining processor")
	}
	<-dispatched
	if removes.Load() != 1 {
		t.Fatalf("expected one removal, got %d", removes.Load())
	}
}

func TestRedirectedRemovalWaitsForOldProcessorCallback(t *testing.T) {
	old, current := NewProcessor(), NewProcessor()
	peer := NewStreamClientPeer(nil, old)
	if !old.TryPostEvent(&Event{ID: AddEvent, Peer: peer}) {
		t.Fatal("registration failed")
	}
	// Dispatch the original registration before transferring business ownership.
	old.safeProcessOnce(nil)
	entered, release, callbackDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	old.AddCallback(1, func(*Message) {
		close(entered)
		<-release
		close(callbackDone)
	})
	dispatched := make(chan struct{})
	go func() { old.DispatchMessage(&Message{Peer: peer, Head: MessageHead{ID: 1}}); close(dispatched) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("original callback did not start")
	}
	peer.Redirect(current)
	peer.Close()
	peer.OnError(-1, errors.New("duplicate error after close"))
	// Repeated explicit notifications must also use the connection's dedup state.
	if !current.TryPostEvent(&Event{ID: RemoveEvent, Peer: peer}) {
		t.Fatal("duplicate removal should be acknowledged")
	}
	var removes atomic.Int32
	current.AddEventCallback(RemoveEvent, func(*Event) {
		select {
		case <-callbackDone:
		default:
			t.Error("redirected removal overtook the original callback")
		}
		removes.Add(1)
	})
	done := make(chan struct{})
	go func() { current.StartProcess(); close(done) }()
	current.EventChan <- &Event{ID: ExitEvent}
	awaitTestCondition(t, "redirected processor did not begin draining", current.isStopping)
	select {
	case <-done:
		t.Fatal("redirected processor exited before the old callback finished")
	default:
	}
	unblock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("old callback did not wake the removal owner")
	}
	<-dispatched
	if removes.Load() != 1 {
		t.Fatalf("current owner received %d removals", removes.Load())
	}
	finishTestProcessor(t, old)
}

func TestLifecycleTurnDoesNotStarveOrdinaryWork(t *testing.T) {
	proc := NewProcessor()
	peer := NewStreamClientPeer(nil, proc)
	proc.TryPostEvent(&Event{ID: AddEvent, Peer: peer})
	if !proc.TryPostEvent(&Event{ID: AddEvent, Peer: peer}) {
		t.Fatal("duplicate registration should be acknowledged")
	}
	peer.Close()
	ordinaryCalled := false
	proc.FuncChan <- func() { ordinaryCalled = true }
	proc.safeProcessOnce(nil)
	if !ordinaryCalled {
		t.Fatal("lifecycle dispatch did not give ordinary work a turn")
	}
	if len(proc.lifecycleQueue) != 1 {
		t.Fatalf("duplicate registration added an event, or too many events ran: pending=%d", len(proc.lifecycleQueue))
	}
	finishTestProcessor(t, proc)
}

func TestOutboundTCPKeepsReliableCloseNotification(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	proc := NewProcessor()
	client, err := NewTcpConnection(listener.Addr().String(), proc)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for len(proc.EventChan) < cap(proc.EventChan) {
		proc.EventChan <- &Event{ID: 99}
	}
	var added, removed int
	proc.AddEventCallback(AddEvent, func(*Event) { added++ })
	proc.AddEventCallback(RemoveEvent, func(*Event) { removed++ })
	client.Close()
	finishTestProcessor(t, proc)
	if added != 0 || removed != 1 {
		t.Fatalf("outbound TCP notification changed: add=%d remove=%d", added, removed)
	}
}

func TestOverloadedReadReleasesEveryMessageBuffer(t *testing.T) {
	proc := NewProcessor()
	proc.MessageChan = make(chan *Message)
	peer := NewStreamClientPeer(nil, proc)
	peer.reader = NewAsyncMessageReader()
	buffer := peer.reader.buffer
	wire := make([]byte, 24)
	for i := 0; i < len(wire); i += 12 {
		binary.LittleEndian.PutUint32(wire[i:], 4)
		binary.LittleEndian.PutUint32(wire[i+4:], 1)
		copy(wire[i+8:], "test")
	}
	if err := peer.OnRead(-1, wire); err == nil || peer.GetState() != PeerStateClosed {
		t.Fatal("full message queue did not close the reader")
	}
	if refs := atomic.LoadInt32(&buffer.refs); refs != 0 {
		t.Fatalf("rejected batch retained the reader buffer: refs=%d", refs)
	}
}

func TestImmediatePanicStillReleasesCallbackAndRemoval(t *testing.T) {
	proc := NewProcessor()
	proc.ImmediateMode = true
	peer := NewStreamClientPeer(nil, proc)
	proc.TryPostEvent(&Event{ID: AddEvent, Peer: peer})
	var removed int
	proc.AddEventCallback(RemoveEvent, func(*Event) { removed++ })
	proc.AddCallback(1, func(message *Message) {
		peer.Close()
		message.Peer = nil // Callback accounting must retain the admitted peer.
		panic("business callback failure")
	})
	reader := NewAsyncMessageReader()
	defer reader.Release()
	wire := make([]byte, 8)
	binary.LittleEndian.PutUint32(wire[4:], 1)
	messages, err := reader.FeedData(wire)
	if err != nil {
		t.Fatal(err)
	}
	buffer := messages[0].Buffer
	if !DeliverZeroCopyMessage(proc, peer, messages[0]) {
		t.Fatal("a recovered callback panic was reported as overload")
	}
	finishTestProcessor(t, proc)
	if removed != 1 || atomic.LoadInt32(&peer.messageCallbacks) != 0 || atomic.LoadInt32(&buffer.refs) != 0 {
		t.Fatal("panic or callback mutation leaked callback accounting, removal, or buffer")
	}
}

func TestRegistrationUsesStablePeerIdentityAndEstablishesOwner(t *testing.T) {
	proc := NewProcessor()
	async := &AsyncClientPeer{FD: -1}
	alias := &ClientPeer{AsyncClientPeer: async}
	canonical := async.ToClientPeer()
	records := make(map[*ClientPeer]bool)
	var added, removed int
	proc.AddEventCallback(AddEvent, func(event *Event) {
		added++
		if event.Peer != canonical {
			t.Error("registration did not use the stable connection view")
		}
		records[event.Peer] = true
	})
	proc.AddEventCallback(RemoveEvent, func(event *Event) {
		removed++
		delete(records, event.Peer)
	})
	if !proc.TryPostEvent(&Event{ID: AddEvent, Peer: alias}) || !proc.TryPostEvent(&Event{ID: AddEvent, Peer: canonical}) {
		t.Fatal("registration failed")
	}
	alias.Close()
	finishTestProcessor(t, proc)
	if added != 1 || removed != 1 || len(records) != 0 {
		t.Fatalf("alias or absent initial owner leaked a record: add=%d remove=%d records=%d", added, removed, len(records))
	}
}
