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
	ret, err := conn.Do("get", key)
	//ret, err := redis.String(conn.Do("get", key))
	if err == nil && ret != nil {
		var formaterr error
		switch out := valuePtr.(type) {
		case *string:
			*out, formaterr = redis.String(ret, err)
			break
		case *int:
			*out, formaterr = redis.Int(ret, err)
			break
		case *int64:
			*out, formaterr = redis.Int64(ret, err)
			break
		case *float64:
			*out, formaterr = redis.Float64(ret, err)
			break
		case *float32:
			var ret64 float64
			ret64, formaterr = redis.Float64(ret, err)
			*out = float32(ret64)
			break
		case *[]byte:
			*out, formaterr = redis.Bytes(ret, err)
			break
		case *bool:
			*out, formaterr = redis.Bool(ret, err)
		default:
			base.LogError("can't support type")
			break
		}
		if formaterr != nil {
			base.LogError("type error when db.get")
		}
		return
	}
	//从couchbase读取
	//var retstr string
	rc.couchNode.bucket.Get(key, valuePtr)
	switch out := valuePtr.(type) {
	case *int:
		conn.Do("set", key, *out)
	case *int64:
		conn.Do("set", key, *out)
	case *float32:
		conn.Do("set", key, *out)
	case *string:
		conn.Do("set", key, *out)
	case *float64:
		conn.Do("set", key, *out)
	case *[]byte:
		conn.Do("set", key, *out)
	case *bool:
		conn.Do("set", key, *out)
	default:
		base.LogError("can't support type")
		break
	}
	//return retstr
}

func (rc *rediscouchbaseQuery) Set(key string, v interface{}, expiry uint32) {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	conn.Do("set", key, v)
	if expiry > 0 {
		conn.Do("expire", key, expiry)
	}
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
			go rc.couchNode.bucket.Upsert(key, retv, 0)
		} else {
			base.LogError("genid (%s)Error", key)
			return -1
		}
		return retv + start
	}
	/*
		cas, err := rc.couchNode.bucket.GetAndLock(key, 1024, nil)
		for err != nil {
			cas, err = rc.couchNode.bucket.GetAndLock(key, 1024, nil)
		}
	*/
	counter, _, err := rc.couchNode.bucket.Counter(key, 1, 0, 0)
	//rc.couchNode.bucket.Unlock(key, cas)
	if err != nil {
		base.LogError("GenID (%s) Error ", key)
		return -1
	}
	conn.Do("set", key, counter)
	return int64(counter) + start
}
func (rc *rediscouchbaseQuery) GetObj(key string, obj proto.Message) {
	var b []byte
	rc.Get(key, &b)
	err := proto.Unmarshal(b, obj)
	if err != nil {
		base.LogError("proto.Ummarshal error key %s,e %s", key, err.Error())
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

// NewRedisCouchbaseQuery 新建一个组合
func NewRedisCouchbaseQuery(rnode *RedisNode, cbnode *CouchbaseCluster) IQuery {
	return &rediscouchbaseQuery{
		node:      rnode,
		couchNode: cbnode,
	}
}
