package db

import (
	"fmt"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/golang/protobuf/proto"
	"github.com/liangpengcheng/qcontinuum/base"
)

// RedisNode 一个redis节点
type RedisNode struct {
	address  string
	password string
	pool     *redis.Pool
}

// RedisConn 重新定义
type RedisConn redis.Conn

// NewRedisNode 创建一个节点
func NewRedisNode(addr string, pwd string, dbindex int32, redisorssdb bool) *RedisNode {
	if redisorssdb {
		USECMD = REDISCMD
	} else {
		USECMD = SSDBCMD
	}
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
					if _, err := r.Do(redisCmd[cAUTH][USECMD], pwd); err != nil {
						base.LogError("redis auth error :%s", err.Error())
					}
				}
				if dbindex > 0 {
					if _, err := r.Do(redisCmd[cSELECT][USECMD], dbindex); err != nil {
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
func (node *RedisNode) GetRedis() RedisConn {
	return node.pool.Get()
}

// Put 放回去
func (node *RedisNode) Put(conn RedisConn) {
	conn.Close()
}

// GetHashRedis get hash value
func GetHashRedis(conn RedisConn, hkey string, key string, valuePtr interface{}) error {
	ret, err := conn.Do(getRCmd(cHGET), hkey, key)
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
		case *int32:
			var ret32 int
			ret32, formaterr = redis.Int(ret, err)
			*out = int32(ret32)
			break
		case *int64:
			*out, formaterr = redis.Int64(ret, err)
			break
		case *uint32:
			var retu32 int64
			retu32, formaterr = redis.Int64(ret, err)
			*out = uint32(retu32)
		case *uint64:
			*out, formaterr = redis.Uint64(ret, err)
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
		case proto.Message:
			var bytes []byte
			bytes, formaterr = redis.Bytes(ret, err)
			proto.Unmarshal(bytes, out)
			break
		default:
			base.LogPanic("can't support type")
			break
		}
		if formaterr != nil {
			return fmt.Errorf("type error when db.get")
		}
		return nil
	}
	return fmt.Errorf("value not found")

}

// SetExpiryRedis 设置一个数值，并且设置过期时间,如果只设置一个过期时间value设置为nil
func SetExpiryRedis(conn RedisConn, key string, value interface{}, expiry uint32) {
	if value != nil {
		conn.Send(getRCmd(cSET), key, value)
	}
	if expiry > 0 {
		conn.Send(getRCmd(cEXPIRE), key, expiry)
	}
	conn.Flush()
}

// IncrExpiryRedis 设置一个数值，并且设置过期时间,如果只设置一个过期时间value设置为nil
func IncrExpiryRedis(conn RedisConn, key string, value interface{}, expiry uint32) int64 {
	if value != nil {
		conn.Send(getRCmd(cINCRBY), key, value)
	}
	if expiry > 0 {
		conn.Send(getRCmd(cEXPIRE), key, expiry)
	}
	ret, err := redis.Int64(conn.Do(getRCmd(cEXEC)))
	base.CheckError(err, "redis incr ")
	return ret
}

// GetRedis 泛型获得,没有这个key的时候也返回error
func GetRedis(conn RedisConn, key string, valuePtr interface{}) error {
	ret, err := conn.Do(getRCmd(cGET), key)
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
		case *int32:
			var ret32 int
			ret32, formaterr = redis.Int(ret, err)
			*out = int32(ret32)
			break
		case *int64:
			*out, formaterr = redis.Int64(ret, err)
			break
		case *uint64:
			*out, formaterr = redis.Uint64(ret, err)
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
		case proto.Message:
			var bytes []byte
			bytes, formaterr = redis.Bytes(ret, err)
			proto.Unmarshal(bytes, out)
			break
		default:
			base.LogPanic("can't support type")
			break
		}
		if formaterr != nil {
			return fmt.Errorf("type error when db.get")
		}
		return nil
	}
	return fmt.Errorf("value not found")
}

// IncrRedis incr value
func IncrRedis(conn RedisConn, key string, step interface{}) (int64, error) {
	switch step.(type) {
	case int:
		return redis.Int64(conn.Do(getRCmd(cINCRBY), key, step))
	case int64:
		return redis.Int64(conn.Do(getRCmd(cINCRBY), key, step))
	case int32:
		return redis.Int64(conn.Do(getRCmd(cINCRBY), key, step))
	case uint:
		return redis.Int64(conn.Do(getRCmd(cINCRBY), key, step))
	case uint32:
		return redis.Int64(conn.Do(getRCmd(cINCRBY), key, step))
	case uint64:
		return redis.Int64(conn.Do(getRCmd(cINCRBY), key, step))
	default:
		base.LogPanic("can't support type")
		return 0, fmt.Errorf("can't support type")
	}
}

// IncrHashRedis incr hash value
func IncrHashRedis(conn RedisConn, hkey string, key string, step interface{}) (int64, error) {
	switch step.(type) {
	case int:
		return redis.Int64(conn.Do(getRCmd(cHINCRBY), hkey, key, step))
	case int64:
		return redis.Int64(conn.Do(getRCmd(cHINCRBY), hkey, key, step))
	case int32:
		return redis.Int64(conn.Do(getRCmd(cHINCRBY), hkey, key, step))
	case uint:
		return redis.Int64(conn.Do(getRCmd(cHINCRBY), hkey, key, step))
	case uint32:
		return redis.Int64(conn.Do(getRCmd(cHINCRBY), hkey, key, step))
	case uint64:
		return redis.Int64(conn.Do(getRCmd(cHINCRBY), hkey, key, step))
	default:
		base.LogPanic("can't support type")
		return 0, fmt.Errorf("can't support type")
	}
}

// SetRedis set a value by interface
func SetRedis(conn RedisConn, key string, valuePtr interface{}) {
	switch out := valuePtr.(type) {
	case *int:
		conn.Do(getRCmd(cSET), key, *out)
	case *int32:
		conn.Do(getRCmd(cSET), key, *out)
	case *int64:
		conn.Do(getRCmd(cSET), key, *out)
	case *float32:
		conn.Do(getRCmd(cSET), key, *out)
	case *string:
		conn.Do(getRCmd(cSET), key, *out)
	case *float64:
		conn.Do(getRCmd(cSET), key, *out)
	case *[]byte:
		conn.Do(getRCmd(cSET), key, *out)
	case *bool:
		conn.Do(getRCmd(cSET), key, *out)
		break
	case int:
		conn.Do(getRCmd(cSET), key, out)
	case int32:
		conn.Do(getRCmd(cSET), key, out)
	case uint64:
		conn.Do(getRCmd(cSET), key, out)
	case int64:
		conn.Do(getRCmd(cSET), key, out)
	case float32:
		conn.Do(getRCmd(cSET), key, out)
	case string:
		conn.Do(getRCmd(cSET), key, out)
	case float64:
		conn.Do(getRCmd(cSET), key, out)
	case []byte:
		conn.Do(getRCmd(cSET), key, out)
	case bool:
		conn.Do(getRCmd(cSET), key, out)
		break
	case proto.Message:
		buf, err := proto.Marshal(out)
		if err == nil {
			conn.Do(getRCmd(cSET), key, buf)
		}
		break
	default:
		base.LogPanic("can't support type")
		break
	}

}

// SetHashRedis set hash value
func SetHashRedis(conn RedisConn, hashKey string, key string, valuePtr interface{}) {
	switch out := valuePtr.(type) {
	case *int:
		conn.Do(getRCmd(cHSET), hashKey, key, *out)
	case *int32:
		conn.Do(getRCmd(cHSET), hashKey, key, *out)
	case *int64:
		conn.Do(getRCmd(cHSET), hashKey, key, *out)
	case *uint64:
		conn.Do(getRCmd(cHSET), hashKey, key, *out)
	case *float32:
		conn.Do(getRCmd(cHSET), hashKey, key, *out)
	case *string:
		conn.Do(getRCmd(cHSET), hashKey, key, *out)
	case *float64:
		conn.Do(getRCmd(cHSET), hashKey, key, *out)
	case *[]byte:
		conn.Do(getRCmd(cHSET), hashKey, key, *out)
	case *bool:
		conn.Do(getRCmd(cHSET), hashKey, key, *out)
		break
	case int:
		conn.Do(getRCmd(cHSET), hashKey, key, out)
	case int32:
		conn.Do(getRCmd(cHSET), hashKey, key, out)
	case int64:
		conn.Do(getRCmd(cHSET), hashKey, key, out)
	case uint64:
		conn.Do(getRCmd(cHSET), hashKey, key, out)
	case float32:
		conn.Do(getRCmd(cHSET), hashKey, key, out)
	case string:
		conn.Do(getRCmd(cHSET), hashKey, key, out)
	case float64:
		conn.Do(getRCmd(cHSET), hashKey, key, out)
	case []byte:
		conn.Do(getRCmd(cHSET), hashKey, key, out)
	case bool:
		conn.Do(getRCmd(cHSET), hashKey, key, out)
		break
	case proto.Message:
		buf, err := proto.Marshal(out)
		if err == nil {
			conn.Do(getRCmd(cHSET), key, buf)
		}
		break
	default:
		break
	}
}

// KeyExistsRedis 是否存在这个键值
func KeyExistsRedis(conn RedisConn, key string) bool {
	exist, error := redis.Bool(conn.Do(getRCmd(cEXISTS), key))
	if exist && error == nil {
		return true
	}
	return false
}

// GenKeyIDRedis 获得一个自增长id
func GenKeyIDRedis(conn RedisConn, key string, start uint64) uint64 {
	exist, error := redis.Bool(conn.Do(getRCmd(cEXISTS), key))
	if exist && error == nil {
		retv, error := redis.Uint64(conn.Do(getRCmd(cINCRBY), key, 1))
		if error == nil {
			return retv
		}
		base.LogPanic("genid (%s)Error(%s)", key, error.Error())
		return 0
	}
	_, err := conn.Do(getRCmd(cSET), key, start)
	if err == nil {
		return start
	}
	base.LogPanic("genid (%s)Error(%s)", key, err.Error())
	return 0
}

// DelRedis 删除某个键值
func DelRedis(conn RedisConn, key string) {
	conn.Do(getRCmd(cDEL), key)
}

// HExistsRedis 是否存在某个hash值
func HExistsRedis(conn RedisConn, hashKey, key string) bool {

	exist, error := redis.Bool(conn.Do(getRCmd(cHEXISTS), hashKey, key))
	if exist && error == nil {
		return true
	}

	return false
}

// HashDelRedis 删除某个hash键值
func HashDelRedis(conn RedisConn, hashKey, key string) {
	conn.Do(getRCmd(cHDEL), hashKey, key)
}

// HLenRedis hash长度
func HLenRedis(conn RedisConn, hashKey string) uint {

	res, err := conn.Do(getRCmd(cHLEN), hashKey)
	if err == nil {
		len, err := redis.Int(res, err)
		if len != 0 && err == nil {
			return uint(len)
		}
	}
	return 0
}
