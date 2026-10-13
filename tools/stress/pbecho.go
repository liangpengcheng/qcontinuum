package main

import (
	"encoding/binary"
	"sync/atomic"

	"github.com/liangpengcheng/qcontinuum/generated/go"
	"github.com/liangpengcheng/qcontinuum/network"
	"google.golang.org/protobuf/proto"
)

// registerProtobufEcho 注册 protobuf 回显处理器：收到 MsgId ECHO 后校验
// protobuf 包体合法性，合法则原样回显（相同 ID）。非法包体计为错误，
// 用于压测中验证协议健壮性。TCP / WebSocket / KCP 共用同一回调。
func registerProtobufEcho(proc *network.Processor, stats *ServerStats) {
	proc.AddCallback(int32(testpb.MsgId_ECHO), func(msg *network.Message) {
		atomic.AddUint64(&stats.TotalReceived, 1)

		var echo testpb.Echo
		if err := proto.Unmarshal(msg.Body, &echo); err != nil {
			atomic.AddUint64(&stats.Errors, 1)
			return
		}

		responseData := make([]byte, 8+len(msg.Body))
		binary.LittleEndian.PutUint32(responseData[0:4], uint32(len(msg.Body)))
		binary.LittleEndian.PutUint32(responseData[4:8], uint32(testpb.MsgId_ECHO))
		copy(responseData[8:], msg.Body)

		if err := msg.Peer.SendMessageBuffer(responseData); err != nil {
			atomic.AddUint64(&stats.Errors, 1)
		} else {
			atomic.AddUint64(&stats.TotalSent, 1)
		}
	})
}
