package db

import "github.com/garyburd/redigo/redis"

type rediscouchbaseQuery struct {
	node      *RedisNode
	couchNode *CouchbaseCluster
}

func (rc *rediscouchbaseQuery) Get(key string) string {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	ret, err := redis.String(conn.Do("get", key))
	if err == nil {
		return ret
	}
	//从couchbase读取
	var retstr string
	rc.couchNode.bucket.Get(key, &retstr)
	conn.Do("set", key, retstr)
	return retstr
}

func (rc *rediscouchbaseQuery) Set(key string, v string, expiry uint32) {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	conn.Do("set", key, v)
	if expiry > 0 {
		conn.Do("expire", key, expiry)
	}
	//设置couchbase
	rc.couchNode.bucket.Upsert(key, v, expiry)
}
