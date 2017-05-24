package main

import (
	"encoding/json"
	"io/ioutil"

	"github.com/liangpengcheng/QContinuum/base"
)

type config struct {
	Port string
}

func newConfig(jstring []byte) *config {
	cfg := &config{}
	err := json.Unmarshal(jstring, cfg)
	if err != nil {
		base.LogError("load config error %s", err.Error())
		return nil
	}
	return cfg
}
func newConfigFromFile(filename string) *config {
	bytes, err := ioutil.ReadFile(filename)
	if err == nil {
		return newConfig(bytes)
	}
	base.LogError("load config failed %s", err.Error())
	return nil
}
