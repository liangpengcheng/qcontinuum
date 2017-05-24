package main

import (
	"encoding/json"
	"io/ioutil"

	"github.com/liangpengcheng/qcontinuum/base"
)

type serviceMangerConfig struct {
	Port string
}

func newConfig(jstring []byte) *serviceMangerConfig {
	cfg := &serviceMangerConfig{}
	err := json.Unmarshal(jstring, cfg)
	if err != nil {
		base.LogError("load config error %s", err.Error())
		return nil
	}
	return cfg
}
func newConfigFromFile(filename string) *serviceMangerConfig {
	bytes, err := ioutil.ReadFile(filename)
	if err == nil {
		return newConfig(bytes)
	}
	base.LogError("load config failed %s", err.Error())
	return nil
}
