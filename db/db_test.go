package db

import (
	"testing"

	"github.com/garyburd/redigo/redis"
	"github.com/liangpengcheng/Qcontinuum/base"
)

func TestRedis(t *testing.T) {
	rn := NewRedisNode("127.0.0.1:6379", "", 0)
	con := rn.GetRedis()
	defer rn.Put(con)
	con.Do("set", "dbtest", "oh..")
	base.LogDebug(redis.String(con.Do("get", "dbtest")))
}

func TestMongo(t *testing.T) {
	m := NewMongoDB("127.0.0.1:27017", "test", "", "")
	defer m.Close()
}
