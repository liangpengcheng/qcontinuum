package db

import "github.com/garyburd/redigo/redis"
import "time"
import "github.com/liangpengcheng/Qcontinuum/base"

// RedisNode 一个redis节点
type RedisNode struct {
	address  string
	password string
	pool     *redis.Pool
}

// NewRedisNode 创建一个节点
func NewRedisNode(addr string, pwd string, dbindex int32) *RedisNode {
	return &RedisNode{
		pool: &redis.Pool{
			MaxIdle:     3,
			IdleTimeout: 240 * time.Second,
			Dial: func() (redis.Conn, error) {
				r, err := redis.Dial("tcp", addr)
				if err != nil {
					base.LogError("redis connect error:%s", err.Error())
					return nil, err
				}
				if len(pwd) > 0 {
					if _, err := r.Do("AUTH", pwd); err != nil {
						base.LogError("redis auth error :%s", err.Error())
					}
				}
				if dbindex > 0 {
					if _, err := r.Do("SELECT", dbindex); err != nil {
						base.LogError("redis select error :%s ", err.Error())
						return nil, err
					}
				}
				return r, nil

			},
		},
		address:  addr,
		password: pwd,
	}
}

// GetRedis  get a connection
func (node *RedisNode) GetRedis() redis.Conn {
	return node.pool.Get()
}

// Put 放回去
func (node *RedisNode) Put(conn redis.Conn) {
	conn.Close()
}
