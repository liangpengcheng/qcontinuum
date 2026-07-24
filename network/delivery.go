package network

func DeliverZeroCopyMessage(proc *Processor, peer *ClientPeer, message *ZeroCopyMessage) bool {
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
		proc.DispatchMessage(&Message{
			Peer: peer,
			Head: message.Head,
			Body: body,
		})
		return true
	}

	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)
	return proc.TryEnqueueMessage(&Message{
		Peer: peer,
		Head: message.Head,
		Body: bodyCopy,
	})
}
