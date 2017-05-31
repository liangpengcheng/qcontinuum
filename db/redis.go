package db

import (
	"fmt"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/liangpengcheng/qcontinuum/base"
)

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

// GetHashInterfacePtr get hash value
func GetHashInterfacePtr(conn redis.Conn, hkey string, key string, valuePtr interface{}) error {
	ret, err := conn.Do("hget", hkey, key)
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
			return fmt.Errorf("type error when db.get")
		}
		return nil
	}
	return fmt.Errorf("value not found")

}

// GetInterfacePtr 泛型获得,没有这个key的时候也返回error
func GetInterfacePtr(conn redis.Conn, key string, valuePtr interface{}) error {
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
			return fmt.Errorf("type error when db.get")
		}
		return nil
	}
	return fmt.Errorf("value not found")
}

// SetInterfacePtr set a value by interface
func SetInterfacePtr(conn redis.Conn, key string, valuePtr interface{}) {
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

}

// SetHashInterfacePtr set hash value
func SetHashInterfacePtr(conn redis.Conn, hashKey string, key string, valuePtr interface{}) {
	switch out := valuePtr.(type) {
	case *int:
		conn.Do("hset", hashKey, key, *out)
	case *int64:
		conn.Do("hset", hashKey, key, *out)
	case *float32:
		conn.Do("hset", hashKey, key, *out)
	case *string:
		conn.Do("hset", hashKey, key, *out)
	case *float64:
		conn.Do("hset", hashKey, key, *out)
	case *[]byte:
		conn.Do("hset", hashKey, key, *out)
	case *bool:
		conn.Do("hset", hashKey, key, *out)
	default:
		base.LogError("can't support type")
		break
	}
}
