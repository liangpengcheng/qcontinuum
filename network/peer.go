package network

import (
	"fmt"

	"net"

	"github.com/golang/protobuf/proto"
	"github.com/liangpengcheng/qcontinuum/base"
)

//ClientPeer client connection peer
type ClientPeer struct {
	Connection   net.Conn
	Serv         *Server
	Flag         int32
	RedirectProc chan *Processor
	Proc         *Processor
}

// SendMessage send a message to peer
func (peer *ClientPeer) SendMessage(msg proto.Message) error {
	buf, err := proto.Marshal(msg)
	if err == nil {
		peer.Connection.Write(buf)
	}
	return err
}

// ConnectionHandler read messages here
func (peer *ClientPeer) ConnectionHandler() {
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
		if len(peer.RedirectProc) > 0 {
			peer.Proc = <-peer.RedirectProc

		}
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
		if peer.Proc.ImmediateMode {
			if cb, ok := peer.Proc.CallbackMap[msg.Head.ID]; ok {
				cb(msg)
			}
		} else {
			peer.Proc.MessageChan <- msg
		}
	}
	event := &Event{
		ID:   RemoveEvent,
		Peer: peer,
	}
	peer.Proc.EventChan <- event
	//base.LogInfo("disconnect %s", peer.Connection.RemoteAddr().String())
}
