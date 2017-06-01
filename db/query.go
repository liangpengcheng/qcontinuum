package db

import "github.com/golang/protobuf/proto"

// IQuery 接口
type IQuery interface {
	Get(key string, valuePtr interface{})
	Set(key string, v interface{}, expiry uint32)
	Exists(key string) bool
	HExists(hkey, key string) bool
	GetHash(hashkey, key string, valuePtr interface{})
	HLen(hashKey string) uint
	SetHash(hashkey, key string, value interface{})
	HashDel(hashKey, delKey string)
	GetObj(key string, obj proto.Message)
	SetObj(key string, obj proto.Message, expiry uint32)
	GenID(key string, start int64) int64
	Del(key string)
}
