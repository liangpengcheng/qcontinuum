package db

import (
	"github.com/garyburd/redigo/redis"
	"github.com/golang/protobuf/proto"
	"github.com/liangpengcheng/qcontinuum/base"
)

type rediscouchbaseQuery struct {
	node      *RedisNode
	couchNode *CouchbaseCluster
}

func (rc *rediscouchbaseQuery) Get(key string, valuePtr interface{}) {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	err := GetInterfacePtr(conn, key, valuePtr)
	if err == nil {
		return
	}
	//从couchbase读取
	//var retstr string
	rc.couchNode.bucket.Get(key, valuePtr)
	SetInterfacePtr(conn, key, valuePtr)
	//return retstr
}

// 设置couchbase的时候使用异步的方式，这个地方要注意安全，可能不是及时生效
func (rc *rediscouchbaseQuery) Set(key string, v interface{}, expiry uint32) {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	SetExpiry(conn, key, v, expiry)

	//设置couchbase
	go rc.couchNode.bucket.Upsert(key, v, expiry)
}
func (rc *rediscouchbaseQuery) GenID(key string, start int64) int64 {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	exist, error := redis.Bool(conn.Do("exists", key))
	if exist && error == nil {
		retv, error := redis.Int64(conn.Do("incr", key))
		if error == nil {
			rc.couchNode.bucket.Upsert(key, retv, 0)
		} else {
			base.LogError("genid (%s)Error", key)
			return -1
		}
		return retv
	}
	counter, _, err := rc.couchNode.bucket.Counter(key, 1, start, 0)
	if err != nil {
		base.LogError("GenID (%s) Error (%s) ", key, err.Error())
		return -1
	}
	conn.Do("set", key, counter)
	return int64(counter)
}
func (rc *rediscouchbaseQuery) GetObj(key string, obj proto.Message) {
	var b []byte
	rc.Get(key, &b)
	if b != nil {
		err := proto.Unmarshal(b, obj)
		if err != nil {
			base.LogError("proto.Ummarshal error key %s,e %s", key, err.Error())
		}
	} else {
		rc.couchNode.bucket.Get(key, obj)
	}

}
func (rc *rediscouchbaseQuery) SetObj(key string, obj proto.Message, expiry uint32) {
	b, err := proto.Marshal(obj)
	if err != nil {
		base.LogError("proto.Mashal error key %s,e %s", err.Error())
		return
	}
	rc.Set(key, b, expiry)
}
func (rc *rediscouchbaseQuery) Del(key string) {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	conn.Do("del", key)
	//var str string
	//cas, error := rc.couchNode.bucket.GetAndLock(key, 1024, &str)
	//for error != nil {
	//cas, error = rc.couchNode.bucket.GetAndLock(key, 1024, &str)
	//}
	// pass zero no cas check
	rc.couchNode.bucket.Remove(key, 0)
	//rc.couchNode.bucket.Unlock(key, cas)
}
func (rc *rediscouchbaseQuery) GetHash(hashkey string, key string, valuePtr interface{}) {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	err := GetHashInterfacePtr(conn, hashkey, key, valuePtr)
	if err == nil {
		return
	}
	//从couchbase读取
	//var retstr string
	rc.couchNode.bucket.MapGet(key, hashkey, valuePtr)
	SetHashInterfacePtr(conn, hashkey, key, valuePtr)
}
func (rc *rediscouchbaseQuery) SetHash(hashkey string, key string, value interface{}) {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	SetHashInterfacePtr(conn, hashkey, key, &value)
	//设置couchbase
	go func() {
		_, err := MapUpser(rc.couchNode.bucket, hashkey, key, value, true)
		if err != nil {
			base.LogPanic("set hash couchbase error %s", err.Error())
		}
	}()

}
func (rc *rediscouchbaseQuery) Exists(key string) bool {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	exist := KeyExistsRedis(conn, key)
	if exist {
		return true
	}
	var ret interface{}
	_, err := rc.couchNode.bucket.Get(key, &ret)
	if ret != nil && err == nil {
		return true
	}
	return false
}
func (rc *rediscouchbaseQuery) HExists(hashKey, key string) bool {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	exist := HExistsRedis(conn, hashKey, key)
	if exist {
		return true
	}
	var ret interface{}
	_, err := rc.couchNode.bucket.MapGet(hashKey, key, &ret)
	if ret != nil && err == nil {
		return true
	}
	return false
}
func (rc *rediscouchbaseQuery) HashDel(hashKey, key string) {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	HashDelRedis(conn, hashKey, key)
	rc.couchNode.bucket.MapRemove(hashKey, key)
}
func (rc *rediscouchbaseQuery) HLen(hashKey string) uint {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	if l := HLenRedis(conn, hashKey); l > 0 {
		return l
	}
	len, _, _ := rc.couchNode.bucket.MapSize(hashKey)
	return len
}

// NewRedisCouchbaseQuery 新建一个组合
func NewRedisCouchbaseQuery(rnode *RedisNode, cbnode *CouchbaseCluster) IQuery {
	return &rediscouchbaseQuery{
		node:      rnode,
		couchNode: cbnode,
	}
}
