package network

import (
	"bytes"
	"encoding/binary"

	"net"

	"github.com/golang/protobuf/proto"
	"github.com/liangpengcheng/qcontinuum/base"
)

//ClientPeer client connection peer
type ClientPeer struct {
	Connection   net.Conn
	Flag         int32
	redirectProc chan *Processor
	Proc         *Processor
	ID           int64
}

func NewTcpConnection(address string, proc *Processor) (client *ClientPeer, err error) {

	socket, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}
	client.Connection = socket
	client.Proc = proc
	return client, nil

}

// Redirect 重新设置处理器
func (peer *ClientPeer) Redirect(proc *Processor) {
	for len(peer.redirectProc) > 0 {
		<-peer.redirectProc
		base.LogWarn("重设处理器队列不为0，请检查逻辑")
	}
	peer.redirectProc <- proc
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
		/*
			peer.Proc.SendChan <- &Message{
				Peer: peer,
				Body: allbuf,
			}
		*/
		//peer.Connection.Send()
		n, err := peer.Connection.Write(allbuf)
		if err != nil {
			base.LogWarn("send error:%s,%d", err.Error(), n)
		} else {
			//base.LogDebug("send success id:%d,len:%d", msgid, n)
		}
	} else {
		base.LogWarn("msg:%d unmarshal error :%s", msgid, err.Error())
	}
	return err
}

// SendMessageBuffer 发送缓冲区
func (peer *ClientPeer) SendMessageBuffer(msg []byte) {
	n, err := peer.Connection.Write(msg)
	if err != nil {
		base.LogWarn("send error:%s,%d", err.Error(), n)
	} else {
		//base.LogDebug("send success len:%d", n)
	}
}

// TransmitMsg 转发消息
func (peer *ClientPeer) TransmitMsg(msg *Message) {

	bufhead := bytes.NewBuffer([]byte{})
	binary.Write(bufhead, binary.LittleEndian, msg.Head.Length)
	binary.Write(bufhead, binary.LittleEndian, msg.Head.ID)
	allbuf := base.BytesCombine(bufhead.Bytes(), msg.Body)
	/*
		peer.Proc.SendChan <- &Message{
			Peer: peer,
			Body: allbuf,
		}
	*/
	n, err := peer.Connection.Write(allbuf)
	if err != nil {
		base.LogWarn("send error:%s,%d", err.Error(), n)
	} else {
		//base.LogDebug("send success len:%d", n)
	}

}

// ConnectionHandler read messages here
func (peer *ClientPeer) ConnectionHandler() {
	defer func() {
		if err := recover(); err != nil {
			base.LogError("%v", err)
		}
	}()
	if peer.Connection == nil {
		base.LogError("connection is nil")
	}

	for {
		h, buffer, err := ReadMessage(peer.Connection)
		if len(peer.redirectProc) > 0 {
			peer.Proc = <-peer.redirectProc

		}
		if err != nil {
			//base.LogInfo("socket read error %s,%s", peer.Connection.RemoteAddr().String(), err.Error())
			//peer.Connection.Close()
			//peer.Connection = nil
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
			} else if peer.Proc.UnHandledHandler != nil {
				peer.Proc.UnHandledHandler(msg)
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
	base.LogInfo("lost connection %s", peer.Connection.RemoteAddr().String())
}
