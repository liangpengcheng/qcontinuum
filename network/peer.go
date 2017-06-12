package network

import (
	"bytes"
	"encoding/binary"
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
func (peer *ClientPeer) SendMessage(msg proto.Message, msgid int32) error {

	length := int32(proto.Size(msg))
	bufhead := bytes.NewBuffer([]byte{})
	//buf := Base.BufferPoolGet()
	//defer Base.BufferPoolPut(buf)
	binary.Write(bufhead, binary.LittleEndian, length)
	binary.Write(bufhead, binary.LittleEndian, msgid)
	buf, err := proto.Marshal(msg)
	if err == nil {
		allbuf := base.BytesCombine(bufhead.Bytes(), buf)
		peer.Proc.SendChan <- &Message{
			Peer: peer,
			Body: allbuf,
		}
	}
	return err
}

// TransmitMsg 转发消息
func (peer *ClientPeer) TransmitMsg(msg *Message) {

	bufhead := bytes.NewBuffer([]byte{})
	binary.Write(bufhead, binary.LittleEndian, msg.Head.Length)
	binary.Write(bufhead, binary.LittleEndian, msg.Head.ID)
	allbuf := base.BytesCombine(bufhead.Bytes(), msg.Body)
	peer.Proc.SendChan <- &Message{
		Peer: peer,
		Body: allbuf,
	}
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
