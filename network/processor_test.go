package network

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/liangpengcheng/qcontinuum/base"
	"golang.org/x/net/websocket"
)

func TestProcessor(t *testing.T) {
	prc := NewProcessor()
	go func() {
		time.Sleep(1)
		prc.EventChan <- &Event{
			ID:    ExitEvent,
			Param: "test",
		}
	}()
	prc.StartProcess()
}

func TestTcp4Server(t *testing.T) {
	serv, err := NewTCP4Server(":7878")
	if err != nil {
		base.LogError(err.Error())
	}
	proc := NewProcessor()
	proc.AddEventCallback(AddEvent, func(event *Event) {
		base.LogDebug("new user is connected")
	})
	proc.AddEventCallback(RemoveEvent, func(event *Event) {
		//一个连接断线后，退出测试
		base.LogDebug("user disconnected")
		proc.EventChan <- &Event{
			ID:    ExitEvent,
			Param: "exit loop",
		}
	})
	go func() {
		conn, err := net.Dial("tcp", "127.0.0.1:7878")
		if err != nil {
			base.LogError("dail error :%s", err.Error())
			proc.EventChan <- &Event{
				ID:    ExitEvent,
				Param: "dail failed",
			}
			return
		}
		//连接后等待一秒断开链接
		time.Sleep(1 * time.Second)
		conn.Close()
	}()
	go serv.BlockAccept(proc)
	proc.StartProcess()
}

func TestWebSocket(t *testing.T) {
	go http.ListenAndServe(":8888", nil)
	time.Sleep(1 * time.Second)
	proc := NewProcessor()
	proc.AddEventCallback(AddEvent, func(event *Event) {
		base.LogDebug("new ws user is connected")
	})
	proc.AddEventCallback(RemoveEvent, func(event *Event) {
		//一个连接断线后，退出测试
		base.LogDebug("ws user disconnected")
		proc.EventChan <- &Event{
			ID:    ExitEvent,
			Param: "exit loop",
		}
	})

	NewWebSocket(proc)
	url := "ws://127.0.0.1:8888/ws"
	org := "http://127.0.0.1:8888/"
	conn, err := websocket.Dial(url, "", org)
	if err == nil {
		time.Sleep(1 * time.Second)
		conn.Close()
	}
	proc.StartProcess()
}
