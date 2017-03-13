package db

import "github.com/garyburd/redigo/redis"

type rediscouchbaseQuery struct {
	node *RedisNode
}

func (rc *rediscouchbaseQuery) Get(key string) string {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	ret, err := redis.String(conn.Do("get", key))
	if err == nil {
		return ret
	}
	//从couchbase读取
	return ""
}

func (rc *rediscouchbaseQuery) Set(key string, v string) {
	conn := rc.node.GetRedis()
	defer rc.node.Put(conn)
	conn.Do("set", key, v)
	//设置couchbase
}
