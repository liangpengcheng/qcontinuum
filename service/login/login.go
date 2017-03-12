package main

import (
	"flag"

	"time"

	"github.com/liangpengcheng/Qcontinuum/base"
	"github.com/liangpengcheng/Qcontinuum/config"
	"github.com/liangpengcheng/Qcontinuum/network"
)

var cfg *config.Config

func main() {
	cfg = config.NewConfigFromFile("../runtime/config.json")
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
