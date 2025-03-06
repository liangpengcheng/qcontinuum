package network

import (
	"bytes"
	"encoding/binary"
	"time"

	"net"

	"github.com/golang/protobuf/proto"
	"github.com/liangpengcheng/qcontinuum/base"
)

// ClientPeer client connection peer
type ClientPeer struct {
	Connection   net.Conn
	Flag         int32
	redirectProc chan *Processor
	Proc         *Processor
	ID           int64
}

func (c *ClientPeer) CheckAfter(t time.Duration) {
	go func() {
		time.AfterFunc(t, func() {
			if c.Proc != nil {
				if c.ID == 0 {
					c.Proc.FuncChan <- func() {
						base.Zap().Sugar().Warnf("auth timeout %v", c.Connection)
						c.Connection.Close()
					}
				}
			}
		})
	}()
}
func (c *ClientPeer) Close() {
	if c.Connection != nil {
		c.Connection.Close()
	}
}

func NewTcpConnection(address string, proc *Processor) (client *ClientPeer, err error) {

	socket, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}
	client = &ClientPeer{
		Connection: socket,
		Proc:       proc,
	}
	return client, nil

}

// Redirect 重新设置处理器
func (peer *ClientPeer) Redirect(proc *Processor) {
	for len(peer.redirectProc) > 0 {
		<-peer.redirectProc
		base.Zap().Sugar().Warnf("重设处理器队列不为0，请检查逻辑")
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
		peer.Connection.SetWriteDeadline(time.Now().Add(time.Second * 10))
		n, writeErr := peer.Connection.Write(allbuf)
		if writeErr != nil {
			base.Zap().Sugar().Warnf("send error:%s,%d", writeErr.Error(), n)
			return writeErr
		}
	} else {
		base.Zap().Sugar().Warnf("msg:%d unmarshal error :%s", msgid, err.Error())
	}
	return err
}

// SendMessageBuffer 发送缓冲区
func (peer *ClientPeer) SendMessageBuffer(msg []byte) error {
	peer.Connection.SetWriteDeadline(time.Now().Add(time.Second * 10))
	n, err := peer.Connection.Write(msg)
	if err != nil {
		base.Zap().Sugar().Warnf("send error:%s,%d", err.Error(), n)
	} else {
		//base.Zap().Sugar().Debugf("send success len:%d", n)
	}
	return err
}

// TransmitMsg 转发消息
func (peer *ClientPeer) TransmitMsg(msg *Message) error {

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
	peer.Connection.SetWriteDeadline(time.Now().Add(time.Second * 10))
	n, err := peer.Connection.Write(allbuf)
	if err != nil {
		base.Zap().Sugar().Warnf("send error:%s,%d", err.Error(), n)
	}
	return err
}

// ConnectionHandler read messages here
func (peer *ClientPeer) ConnectionHandler() {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(net.Error); ok {
				base.Zap().Sugar().Infof("Connection closed")
			} else {
				base.Zap().Sugar().Errorf("Unexpected error in connection handler: %v", r)
			}
		}
	}()
	if peer.Connection == nil {
		base.Zap().Sugar().Error("connection is nil")
		return
	}

	for {
		h, buffer, err := ReadMessage(peer.Connection)
		if len(buffer) == 0 {
			continue
		}
		if len(peer.redirectProc) > 0 {
			peer.Proc = <-peer.redirectProc

		}
		if err != nil {
			//base.Zap().Sugar().Infof("socket read error %s,%s", peer.Connection.RemoteAddr().String(), err.Error())
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
	base.Zap().Sugar().Infof("lost connection %s", peer.Connection.RemoteAddr().String())
}

// ConnectionHandler read messages here
func (peer *ClientPeer) ConnectionHandlerWithPreFunc(f func()) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(net.Error); ok {
				base.Zap().Sugar().Infof("Connection closed")
			} else {
				base.Zap().Sugar().Errorf("Unexpected error in connection handler: %v", r)
			}
		}
	}()
	if peer.Connection == nil {
		base.Zap().Sugar().Error("connection is nil")
		return
	}

	for {
		f()
		h, buffer, err := ReadMessage(peer.Connection)
		if len(buffer) == 0 {
			continue
		}
		if len(peer.redirectProc) > 0 {
			peer.Proc = <-peer.redirectProc

		}
		if err != nil {
			//base.Zap().Sugar().Infof("socket read error %s,%s", peer.Connection.RemoteAddr().String(), err.Error())
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
	base.Zap().Sugar().Infof("lost connection %s", peer.Connection.RemoteAddr().String())
}
