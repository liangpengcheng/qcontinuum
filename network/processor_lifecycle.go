package network

import (
	"sync/atomic"
	"time"

	"github.com/liangpengcheng/qcontinuum/base"
)

const maxPendingLifecycleEvents = 1024

// postLifecycleEvent serializes admission with Close. Lock order is always peer
// then processor; consumers only inspect atomic callback readiness under queueMu.
func (p *Processor) postLifecycleEvent(event *Event) bool {
	peer := event.Peer.AsyncClientPeer
	if event.ID == RemoveEvent {
		return peer.closeWithEvent(event)
	}
	peer.lifecycleMu.Lock()
	defer peer.lifecycleMu.Unlock()
	return p.postLifecycleEventLocked(event)
}

// postLifecycleEventLocked requires the event peer's lifecycleMu.
func (p *Processor) postLifecycleEventLocked(event *Event) bool {
	peer := event.Peer.AsyncClientPeer
	p.queueMu.Lock()
	defer p.queueMu.Unlock()

	if event.ID == AddEvent {
		if p.stopping || peer.GetState() != PeerStateConnected || peer.removeQueued {
			return false
		}
		if peer.registered {
			return true
		}
		if len(p.lifecycleQueue) >= maxPendingLifecycleEvents {
			return false
		}
		if peer.getProcessorLocked() == nil {
			peer.Proc = p // Also support explicitly registered, literal-created peers.
		}
		peer.registered = true
	} else {
		if peer.removeQueued || peer.removeDelivered {
			return true
		}
		if !peer.registered && !peer.notifyOnClose {
			return false
		}
		if p.stopped {
			base.Zap().Sugar().Warn("remove event posted after processor exit; stop connections before ExitEvent")
			return false
		}
		peer.removeQueued = true
		peer.removeProc = p
	}
	// Own the event value so a caller cannot change an admitted event's identity.
	queued := *event
	queued.Peer = peer.ToClientPeer()
	p.lifecycleQueue = append(p.lifecycleQueue, &queued)
	p.signalLifecycle()
	return true
}

func (p *Processor) signalLifecycle() {
	select {
	case p.lifecycleWake <- struct{}{}:
	default:
	}
}

func (p *Processor) popLifecycleEvent() *Event {
	p.queueMu.Lock()
	defer p.queueMu.Unlock()
	if len(p.lifecycleQueue) == 0 {
		return nil
	}
	event := p.lifecycleQueue[0]
	if event.ID == RemoveEvent {
		if event.Peer.GetState() != PeerStateClosed || atomic.LoadInt32(&event.Peer.messageCallbacks) != 0 {
			return nil
		}
	}
	p.lifecycleQueue[0] = nil
	p.lifecycleQueue = p.lifecycleQueue[1:]
	if len(p.lifecycleQueue) == 0 {
		p.lifecycleQueue = nil
	}
	return event
}

func (p *Processor) beginMessage(msg *Message) bool {
	if msg == nil {
		return false
	}
	if msg.Peer != nil && msg.Peer.AsyncClientPeer != nil {
		peer := msg.Peer.AsyncClientPeer
		peer.lifecycleMu.Lock()
		defer peer.lifecycleMu.Unlock()
	}
	p.queueMu.Lock()
	defer p.queueMu.Unlock()
	if !p.acceptsMessageLocked(msg) {
		return false
	}
	p.inFlight++
	if msg.Peer != nil && msg.Peer.AsyncClientPeer != nil {
		atomic.AddInt32(&msg.Peer.messageCallbacks, 1)
	}
	return true
}

// acceptsMessageLocked requires queueMu and, when present, the peer's lifecycleMu.
// Both queue admission and callback entry apply this same rule.
func (p *Processor) acceptsMessageLocked(msg *Message) bool {
	if p.stopping {
		return false
	}
	if msg.Peer != nil && msg.Peer.AsyncClientPeer != nil {
		peer := msg.Peer.AsyncClientPeer
		return peer.GetState() == PeerStateConnected && !peer.removeQueued && !peer.removeDelivered
	}
	return true
}

func (p *Processor) endMessage(client *ClientPeer) {
	var removalOwner *Processor
	if client != nil && client.AsyncClientPeer != nil {
		peer := client.AsyncClientPeer
		peer.lifecycleMu.Lock()
		atomic.AddInt32(&peer.messageCallbacks, -1)
		removalOwner = peer.removeProc
		peer.lifecycleMu.Unlock()
	}
	p.queueMu.Lock()
	p.inFlight--
	stopping := p.stopping
	p.queueMu.Unlock()
	if stopping || removalOwner == p {
		p.signalLifecycle()
	}
	if removalOwner != nil && removalOwner != p {
		removalOwner.signalLifecycle()
	}
}

func (p *Processor) isStopping() bool {
	p.queueMu.Lock()
	defer p.queueMu.Unlock()
	return p.stopping
}

func (p *Processor) finishIfDrained() bool {
	p.queueMu.Lock()
	defer p.queueMu.Unlock()
	if p.stopping && len(p.lifecycleQueue) == 0 && p.inFlight == 0 {
		p.stopped = true
		return true
	}
	return false
}

func (p *Processor) processOrdinaryEvent(event *Event) {
	if event == nil {
		return
	}
	if event.ID == ExitEvent {
		p.queueMu.Lock()
		p.stopping = true
		p.queueMu.Unlock()
		base.Zap().Sugar().Infof("Processor draining before exit: %s", event.Param)
		return
	}
	// Preserve direct channel users while routing their lifecycle events through
	// the same ordering and readiness checks as TryPostEvent.
	if event.Peer != nil && event.Peer.AsyncClientPeer != nil && (event.ID == AddEvent || event.ID == RemoveEvent) {
		p.TryPostEvent(event)
		return
	}
	p.handleEvent(event)
}

// One nonblocking ordinary turn after each lifecycle event prevents starvation.
func (p *Processor) tryProcessOrdinary(tick <-chan time.Time) {
	select {
	case msg := <-p.MessageChan:
		p.handleMessage(msg)
	case event := <-p.EventChan:
		p.processOrdinaryEvent(event)
	case f := <-p.FuncChan:
		p.handleFunc(f)
	case <-tick:
		if p.updateCallback != nil {
			p.updateCallback()
		}
	default:
	}
}
