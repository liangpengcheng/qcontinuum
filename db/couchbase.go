package db

import (
	"github.com/liangpengcheng/qcontinuum/base"
	"gopkg.in/couchbase/gocb.v1"
)

// CouchbaseCluster 封装
type CouchbaseCluster struct {
	bucket *gocb.Bucket
}

// NewCouchbaseConnection 创建链接
func NewCouchbaseConnection(server string, bucketName string, pwd string) *CouchbaseCluster {
	clu, error := gocb.Connect(server)
	if error == nil {
		bk, err := clu.OpenBucket(bucketName, pwd)
		if err == nil {
			couchbase := &CouchbaseCluster{
				bucket: bk,
			}
			base.LogInfo("coubase started")
			return couchbase
		}
		base.Zap().Sugar().Errorf("open bucket error :%s", err.Error())
	} else {
		base.Zap().Sugar().Errorf("connect couchbase error :%s", error.Error())
	}
	return nil
}

// MapUpser inserts an item to a map document.
func MapUpser(b *gocb.Bucket, key, path string, value interface{}, createMap bool) (gocb.Cas, error) {
	for {
		frag, err := b.MutateIn(key, 0, 0).Upsert(path, value, false).Execute()
		if err != nil {
			if err == gocb.ErrKeyNotFound && createMap {
				data := make(map[string]interface{})
				data[path] = value
				cas, err := b.Insert(key, data, 0)
				if err != nil {
					if err == gocb.ErrKeyExists {
						continue
					}

					return 0, err
				}
				return cas, nil
			}
			return 0, err
		}
		return frag.Cas(), nil
	}
}
