package main

import (
	"container/list"

	"github.com/liangpengcheng/qcontinuum/network"
	"github.com/liangpengcheng/qcontinuum/base"
)

type connectionAgent struct {
	peer  *network.ClientPeer
	users map[int64]int64
}

// connectionManger connection manger
// Connection Server连接后，并没有认证策略，所以连接管理部署的时候注意网络安全
type connectionManger struct {
	connections list.List
	processor   *network.Processor
}

// newConnectionManger 创建一个连接管理器
func newConnectionManger() *connectionManger {
	manger := &connectionManger{
		processor: network.NewProcessor(),
	}
	manger.processor.AddEventCallback(network.AddEvent, manger.onConnectionServerConnected)
	manger.processor.AddEventCallback(network.RemoveEvent, manger.onConnectionServerClose)
	return manger
}

func (manger *connectionManger) onConnectionServerConnected(event *network.Event) {
	ca := &connectionAgent{
		peer:  event.Peer,
		users: make(map[int64]int64),
	}
	manger.connections.PushBack(ca)
}

func (manger *connectionManger) onConnectionServerClose(event *network.Event) {
	for c := manger.connections.Front(); c != nil; c = c.Next() {
		if c.Value.(connectionAgent).peer == event.Peer {
			manger.connections.Remove(c)
			base.LogDebug("connection agent disconnected")
			return
		}
	}
}
