package network

import "github.com/liangpengcheng/qcontinuum/base"

// DeliverZeroCopyMessage always releases message. A peer determines the current
// destination; proc is the fallback for messages without an attached peer.
func DeliverZeroCopyMessage(proc *Processor, peer *ClientPeer, message *ZeroCopyMessage) bool {
	if peer != nil && peer.AsyncClientPeer != nil {
		proc = peer.getProcessor()
	}
	if proc == nil || message == nil {
		if message != nil {
			message.Release()
		}
		return false
	}
	defer message.Release()

	body := message.GetBody()
	if body == nil && message.Head.Length != 0 {
		return false
	}

	if proc.ImmediateMode {
		return proc.dispatchMessage(&Message{
			Peer: peer,
			Head: message.Head,
			Body: body,
		})
	}

	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)
	accepted := proc.TryEnqueueMessage(&Message{
		Peer: peer,
		Head: message.Head,
		Body: bodyCopy,
	})
	if !accepted {
		base.Zap().Sugar().Warn("message queue full or delivery stopped; closing connection")
	}
	return accepted
}
