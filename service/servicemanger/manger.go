package main

import (
	"github.com/liangpengcheng/QContinuum/network"
)

func main() {
	cfg := newConfigFromFile("../runtime/serviceManger.json")
	ser, err := network.NewTCP4Server(cfg.Port)
	if err == nil {
		ser.BlockAccept()
	}
}
