package network

import "testing"
import "time"

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
