package main

import (
	"github.com/liangpengcheng/qcontinuum/base"
	"github.com/liangpengcheng/qcontinuum/network"
)

func main() {
	cfg := newConfigFromFile("../runtime/serviceManger.json")
	ser, err := network.NewTCP4Server(cfg.Port)
	if err == nil {
		manger := newConnectionManger()
		go ser.BlockAccept(manger.processor)
		manger.processor.StartProcess()
	} else {
		base.LogError("manger server start error %s", err.Error())
	}
}
