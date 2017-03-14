package db

import (
	"testing"

	"github.com/garyburd/redigo/redis"
	"github.com/liangpengcheng/Qcontinuum/base"
)

func TestRedis(t *testing.T) {
	rn := NewRedisNode("192.168.1.8:6379", "111111", 0)
	con := rn.GetRedis()
	defer rn.Put(con)
	con.Do("set", "dbtest", "oh..")
	base.LogDebug(redis.String(con.Do("get", "dbtest")))
}
