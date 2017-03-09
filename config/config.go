package config

import (
	"encoding/json"

	"io/ioutil"

	"github.com/liangpengcheng/Qcontinuum/base"
)

// Config 配置信息
type Config struct {
	// AuthTimeout 认证超时，如果一个链接在连接后 AuthTimeout 秒钟之后还没有发送认证消息，就强制断开链接
	AuthTimeout int32
}

// NewConfigFromJSON 加载一个配置文件
func NewConfigFromJSON(jsonstring []byte) *Config {
	cfg := &Config{}
	err := json.Unmarshal(jsonstring, cfg)
	if err != nil {
		base.LogError("load config failed %s", err.Error())
		return nil
	}
	return cfg
}

// NewConfigFromFile 加载一个配置文件
func NewConfigFromFile(filename string) *Config {
	bytes, err := ioutil.ReadFile(filename)
	if err == nil {
		return NewConfigFromJSON(bytes)
	}
	base.LogError("load config failed %s", err.Error())
	return nil
}
