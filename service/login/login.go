package main

import (
	"flag"

	"time"

	"github.com/liangpengcheng/qcontinuum/base"
	"github.com/liangpengcheng/qcontinuum/network"
)

var cfg *loginConfig

func main() {
	cfg = NewConfigFromFile("../runtime/login.json")
	port := flag.String("port", ":80", "login port")
	if port != nil {
		ser, err := network.NewTCP4Server(*port)
		if err == nil {
			proc := network.NewProcessor()
			// 注意线程安全
			proc.ImmediateMode = true
			proc.AddEventCallback(network.AddEvent, func(event *network.Event) {
				go func() {
					//force disconnect after 15 seconds
					time.Sleep(time.Duration(cfg.AuthTimeout) * time.Second)
					event.Peer.Connection.Close()
				}()
			})

			ser.BlockAccept(proc)
		} else {
			base.LogError("create server failed :%s", err.Error())
		}
	}
}
