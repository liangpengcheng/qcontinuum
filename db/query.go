package db

import "github.com/golang/protobuf/proto"

// IQuery 接口
type IQuery interface {
	Get(key string, valuePtr interface{})
	Set(key string, v interface{}, expiry uint32)
	GetHash(hashkey string, key string, valuePtr interface{})
	SetHash(hashkey string, key string, value interface{})
	GetObj(key string, obj proto.Message)
	SetObj(key string, obj proto.Message, expiry uint32)
	GenID(key string, start int64) int64
	Del(key string)
}
