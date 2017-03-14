package db

import (
	"github.com/liangpengcheng/Qcontinuum/base"
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
			return couchbase
		}
		base.LogError("open bucket error :%s", error.Error())
	} else {
		base.LogError("connect couchbase error :%s", error.Error())
	}
	return nil
}
