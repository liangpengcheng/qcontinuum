package db

import (
	"testing"

	"github.com/garyburd/redigo/redis"
	"github.com/liangpengcheng/qcontinuum/base"
)

func TestRedis(t *testing.T) {
	rn := NewRedisNode("127.0.0.1:6379", "", 0, true)
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
	rn := NewRedisNode("localhost:6379", "", 0, true)
	cb := NewCouchbaseConnection("localhost:8091", "default", "")
	q := NewRedisCouchbaseQuery(rn, cb)
	r := rn.GetRedis()
	base.LogDebug("new id %d", q.GenID("idtest", 0))
	id := q.GenID("idtest", 0)
	r.Do("del", "idtest")
	for i := 1; i < 100; i++ {
		if id2 := q.GenID("idtest", 0); id+uint64(i) != id2 {
			t.Errorf("gen id error,%d ~ %d", id+uint64(i), id2)
		}
		if i%3 == 0 {
			r.Do("del", "idtest")
		}
	}
	q.Set("keytest", "keytest", 0)
	var getv string
	q.Get("keytest", &getv)
	if getv != "keytest" {
		t.Errorf("test failed,v(%s)", getv)
	}
	r.Do("del", "keytest")
	//测试缓存失效的情况
	q.Get("keytest", &getv)
	if getv != "keytest" {
		t.Errorf("test failed,v(%s)", getv)
	}
	q.Del("keytest")

	q.SetHash("hashTest", "v1", int32(1024))
	var hval int32
	q.GetHash("hashTest", "v1", &hval)
	if hval != 1024 {
		t.Error("hash test failed")
	}
	if !q.HExists("hashTest", "v1") {
		t.Error("hash exists failed")
	}
	r.Do("hdel", "hashTest", "v1")

	q.GetHash("hashTest", "v1", &hval)
	if hval != 1024 {
		t.Error("hash test failed")
	}
	if !q.HExists("hashTest", "v1") {
		t.Error("hash exists failed")
	}
	q.SetHash("hashTest", "v1", "notval")
	q.HashDel("hashTest", "v1")
	if q.HExists("hashTest", "v1") {
		t.Error("hash exists failed")
	}
	q.SetHash("hashTest", "v2", "wahaha")
	q.SetHash("hashTest", "v1", "reset")
	rn.Put(r)
}
