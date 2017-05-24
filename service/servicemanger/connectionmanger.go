package main

import (
	proto "github.com/golang/protobuf/proto"
	network "github.com/liangpengcheng/qcontinuum/network"
	"github.com/liangpengcheng/qcontinuum/service/protocol"
)

type connectionAgent struct {
	peer  *network.ClientPeer
	ip    string
	users map[int64]int64
}

// connectionManger connection manger
// Connection Server连接后，并没有认证策略，所以连接管理部署的时候注意网络安全
type connectionManger struct {
	connections map[*network.ClientPeer]*connectionAgent
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

// connectionagent连接上来了
func (manger *connectionManger) onConnectionServerConnected(event *network.Event) {

}

// connectionagent下线了
func (manger *connectionManger) onConnectionServerClose(event *network.Event) {
	delete(manger.connections, event.Peer)
}

// connectionagetn 要求注册
func (manger *connectionManger) onConnectionRegister(msg *network.Message) {
	con2sm := protocol.Con2SMRegister
	proto.Unmarshal(msg.Body, &con2sm)
	ca := &connectionAgent{
		peer:  msg.Peer,
		users: make(map[*network.ClientPeer]*connectionAgent),
		ip:    con2sm.PublicIP,
	}
	manger.connections[msg.Peer] = ca
}
