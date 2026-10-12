package network

import (
	"runtime/debug"

	"github.com/liangpengcheng/qcontinuum/base"
)

func (p *Processor) DispatchMessage(msg *Message) {
	p.dispatchMessage(msg)
}

func (p *Processor) dispatchMessage(msg *Message) (accepted bool) {
	if p == nil || msg == nil {
		return false
	}
	accepted = true // A recovered callback panic does not mean queue overload.
	// Immediate delivery runs outside StartProcess's recovery boundary.
	defer func() {
		if recovered := recover(); recovered != nil {
			base.Zap().Sugar().Errorf("processor recovered from panic: %v\n%s", recovered, debug.Stack())
		}
	}()
	return p.handleMessage(msg)
}

// TryEnqueueMessage rejects overload, shutdown, and messages for closing peers.
func (p *Processor) TryEnqueueMessage(msg *Message) bool {
	if p == nil || msg == nil {
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
	select {
	case p.MessageChan <- msg:
		return true
	default:
		return false
	}
}

// TryPostEvent admits peer Add/Remove events to an ordered lifecycle FIFO.
// Adds have a 1024-event admission limit; accepted peers' removals are deduplicated
// and never rejected for queue capacity. Other events use EventChan as before.
// Explicit removal closes the connection and uses its current processor.
func (p *Processor) TryPostEvent(event *Event) bool {
	if p == nil || event == nil {
		return false
	}
	if event.Peer != nil && event.Peer.AsyncClientPeer != nil && (event.ID == AddEvent || event.ID == RemoveEvent) {
		return p.postLifecycleEvent(event)
	}
	p.queueMu.Lock()
	defer p.queueMu.Unlock()
	if p.stopping {
		return false
	}
	select {
	case p.EventChan <- event:
		return true
	default:
		return false
	}
}
