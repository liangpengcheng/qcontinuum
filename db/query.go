package db

import "github.com/golang/protobuf/proto"

// IQuery 接口
type IQuery interface {
	Get(key string) string
	Set(key string, v string, expiry uint32)
	GetObj(key string, obj proto.Message)
	SetObj(key string, obj proto.Message, expiry uint32)
}