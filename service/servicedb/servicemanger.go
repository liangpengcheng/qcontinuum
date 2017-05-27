package servicedb

import "github.com/liangpengcheng/qcontinuum/db"

var (
	cfg   *config
	query db.IQuery
)

// RegConnectionAgent 注册一个连接代理，返回这个代理的id号
func RegConnectionAgent(publicip string) int {
	query.Set("0", publicip, 0)
	return 0
}

func init() {
	cfg = newConfigFromFile("../runtime/db.json")
	redis := db.NewRedisNode(cfg.Redis, "", 0)
	cb := db.NewCouchbaseConnection(cfg.Couchbase, cfg.CouchbaseBucket, "")
	query = db.NewRedisCouchbaseQuery(redis, cb)
	//RegConnectionAgent("192.168.1.1:80")
	//base.LogDebug(query.Get("0"))
}
