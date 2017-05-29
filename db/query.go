package db

import "github.com/golang/protobuf/proto"

// IQuery 接口
type IQuery interface {
	Get(key string, valuePtr interface{})
	Set(key string, v interface{}, expiry uint32)
	GetObj(key string, obj proto.Message)
	SetObj(key string, obj proto.Message, expiry uint32)
	GenID(key string, start int64) int64
	Del(key string)
}
