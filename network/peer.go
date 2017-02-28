package network

import (
	"net"

	"fmt"

	"github.com/liangpengcheng/Qcontinuum/base"
)

//ClientPeer client connection peer
type ClientPeer struct {
	Connection net.Conn
	Serv       *Server
	Flag       int32
}

// ConnectionHandler read messages here
func (peer *ClientPeer) ConnectionHandler(proc *Processor) {
	defer func() {
		if err := recover(); err != nil {
			base.LogError(fmt.Sprint(err))
		}
	}()
	if peer.Connection == nil {
		base.LogError("connection is nil")
	}

	for {
		h, buffer, err := ReadMessage(peer.Connection)
		if err != nil {
			base.LogInfo("socket read error %s,%s", peer.Connection.RemoteAddr().String(), err.Error())

			break
		}
		msg := &Message{
			Peer: peer,
			Head: *h,
			Body: buffer,
		}
		proc.MessageChan <- msg
	}
	event := &Event{
		ID:   RemoveEvent,
		Peer: peer,
	}
	proc.EventChan <- event
	base.LogInfo("disconnect %s", peer.Connection.RemoteAddr().String())
}
