module github.com/liangpengcheng/qcontinuum

go 1.12

replace (
	golang.org/x/crypto => github.com/golang/crypto v0.0.0-20190820162420-60c769a6c586
	golang.org/x/net => github.com/golang/net v0.0.0-20190813141303-74dc4d7220e7
	golang.org/x/sys => github.com/golang/sys v0.0.0-20190813064441-fde4db37ae7a
)

require (
	github.com/fasthttp/router v0.5.0
	github.com/fasthttp/websocket v1.4.0
	github.com/garyburd/redigo v1.6.0
	github.com/gin-gonic/gin v1.7.4
	github.com/golang/protobuf v1.3.3
	github.com/golang/snappy v0.0.1 // indirect
	github.com/google/uuid v1.1.1 // indirect
	github.com/gorilla/websocket v1.4.2
	github.com/klauspost/reedsolomon v1.9.2 // indirect
	github.com/kr/pretty v0.1.0 // indirect
	github.com/opentracing/opentracing-go v1.1.0 // indirect
	github.com/pkg/errors v0.8.1 // indirect
	github.com/templexxx/cpufeat v0.0.0-20180724012125-cef66df7f161 // indirect
	github.com/templexxx/xor v0.0.0-20181023030647-4e92f724b73b // indirect
	github.com/tjfoc/gmsm v1.0.1 // indirect
	github.com/valyala/fasthttp v1.34.0
	github.com/xtaci/kcp-go v5.4.4+incompatible
	github.com/xtaci/lossyconn v0.0.0-20190602105132-8df528c0c9ae // indirect
	golang.org/x/net v0.0.0-20220225172249-27dd8689420f
	gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127 // indirect
	gopkg.in/couchbase/gocb.v1 v1.6.2
	gopkg.in/couchbase/gocbcore.v7 v7.1.13 // indirect
	gopkg.in/couchbaselabs/gocbconnstr.v1 v1.0.3 // indirect
	gopkg.in/couchbaselabs/gojcbmock.v1 v1.0.3 // indirect
	gopkg.in/couchbaselabs/jsonx.v1 v1.0.0 // indirect
	gopkg.in/mgo.v2 v2.0.0-20190816093944-a6b53ec6cb22
)
