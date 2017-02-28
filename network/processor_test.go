package network

import "testing"
import "time"
import "github.com/liangpengcheng/Qcontinuum/base"

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
	go serv.BlockAccept(proc)
	proc.StartProcess()
}
