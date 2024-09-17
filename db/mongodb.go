package db

import (
	"github.com/liangpengcheng/qcontinuum/base"
	"gopkg.in/mgo.v2"
)

// MongoDB mongo
type MongoDB struct {
	Session *mgo.Session
	DB      *mgo.Database
}

// MongoDBCollection 集合
type MongoDBCollection struct {
	Collection *mgo.Collection
}

// NewMongoDB 创建一个MongoDB
func NewMongoDB(addr string, db string, user string, password string) *MongoDB {
	session, err := mgo.Dial(addr)
	if err == nil {
		ndb := session.DB(db)
		if len(user) > 0 && len(password) > 0 {
			ndb.Login(user, password)
		}
		return &MongoDB{
			Session: session,
			DB:      ndb,
		}
	}
	base.Zap().Sugar().Errorf("connect mongo error : %s", err.Error())
	return nil
}

// NewMongoCollection collection
func NewMongoCollection(mdb *MongoDB, collection string) *MongoDBCollection {
	if mdb.DB != nil {
		return &MongoDBCollection{
			Collection: mdb.DB.C(collection),
		}
	}
	base.Zap().Sugar().Errorf("connect db first")
	return nil
}

// Close 关闭Mongodb
func (m *MongoDB) Close() {
	if m.Session != nil {
		m.Session.Close()
	}
}
