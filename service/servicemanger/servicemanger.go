package main

import (
	"github.com/liangpengcheng/qcontinuum/network"
	"github.com/liangpengcheng/qcontinuum/base"
)

func main() {
	cfg := newConfigFromFile("../runtime/serviceManger.json")
	ser, err := network.NewTCP4Server(cfg.Port)
	if err == nil {
		manger := newConnectionManger()
		ser.BlockAccept(manger.processor)
	} else {
		base.LogError("manger server start error %s", err.Error())
	}
}
