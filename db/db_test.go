package db

import (
	"testing"

	"github.com/garyburd/redigo/redis"
	"github.com/liangpengcheng/qcontinuum/base"
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

func TestQuery(t *testing.T) {
	rn := NewRedisNode("localhost:6379", "", 0)
	cb := NewCouchbaseConnection("localhost:8091", "default", "")
	q := NewRedisCouchbaseQuery(rn, cb)
	base.LogDebug("new id %d", q.GenID("idtest", 10))
	base.LogDebug("new id %d", q.GenID("idtest", 10))
	q.Set("keytest", "keytest", 0)
	var getv string
	q.Get("keytest", &getv)
	if getv != "keytest" {
		t.Errorf("test failed,v(%s)", getv)
	}
	r := rn.GetRedis()
	r.Do("del", "keytest")
	rn.Put(r)
	//测试缓存失效的情况
	q.Get("keytest", &getv)
	if getv != "keytest" {
		t.Errorf("test failed,v(%s)", getv)
	}
}
