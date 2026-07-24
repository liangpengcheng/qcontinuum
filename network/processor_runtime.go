package network

func (p *Processor) DispatchMessage(msg *Message) {
	if p == nil || msg == nil {
		return
	}
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
