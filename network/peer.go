package network

import (
	"fmt"

	"io"

	"github.com/liangpengcheng/qcontinuum/base"
)

// Peer 链接节点
type Peer interface {
	io.ReadWriter
	GetRemoteAddr() string
	Close()
}

//ClientPeer client connection peer
type ClientPeer struct {
	Connection Peer
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
			//base.LogInfo("socket read error %s,%s", peer.Connection.RemoteAddr().String(), err.Error())
			//peer.Connection.Close()
			peer.Connection = nil
			break
		}
		msg := &Message{
			Peer: peer,
			Head: *h,
			Body: buffer,
		}
		//是否立即处理这个消息，如果立即处理的话，就在当前线程处理了，小心线程安全问题
		if proc.ImmediateMode {
			if cb, ok := proc.CallbackMap[msg.Head.ID]; ok {
				cb(msg)
			}
		} else {
			proc.MessageChan <- msg
		}
	}
	event := &Event{
		ID:   RemoveEvent,
		Peer: peer,
	}
	proc.EventChan <- event
	//base.LogInfo("disconnect %s", peer.Connection.RemoteAddr().String())
}
