package network

import (
	"runtime/debug"

	"github.com/liangpengcheng/qcontinuum/base"
)

func (p *Processor) DispatchMessage(msg *Message) {
	if p == nil || msg == nil {
		return
	}
	// Immediate delivery runs outside StartProcess's recovery boundary.
	defer func() {
		if recovered := recover(); recovered != nil {
			base.Zap().Sugar().Errorf("processor recovered from panic: %v\n%s", recovered, debug.Stack())
		}
	}()
	p.handleMessage(msg)
}

func (p *Processor) TryEnqueueMessage(msg *Message) bool {
	if p == nil || msg == nil {
		return false
	}
	select {
	case p.MessageChan <- msg:
		return true
	default:
		return false
	}
}

func (p *Processor) TryPostEvent(event *Event) bool {
	if p == nil || event == nil {
		return false
	}
	select {
	case p.EventChan <- event:
		return true
	default:
		return false
	}
}
