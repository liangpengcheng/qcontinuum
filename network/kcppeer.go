package network

import (
	"fmt"

	"github.com/liangpengcheng/qcontinuum/base"
	kcp "github.com/xtaci/kcp-go"
)

// KcpPeer 一个kcp连接
type KcpPeer struct {
	Connection *kcp.UDPSession
}

// ConnectionHandle 处理这个连接
func (peer *KcpPeer) ConnectionHandle(proc *Processor) {
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
			peer.Connection.Close()
			break
		}
		msg := &Message{
			//需要重新设计
			//Peer: peer,
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
		ID: RemoveEvent,
		// 需要重新设计
		//	Peer: peer,
	}
	proc.EventChan <- event
	base.LogInfo("disconnect %s", peer.Connection.RemoteAddr().String())
}
